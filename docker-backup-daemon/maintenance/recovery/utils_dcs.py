# Copyright 2024-2025 NetCracker Technology Corporation
#
# Licensed under the Apache License, Version 2.0 (the "License");
# you may not use this file except in compliance with the License.
# You may obtain a copy of the License at
#
#     http://www.apache.org/licenses/LICENSE-2.0
#
# Unless required by applicable law or agreed to in writing, software
# distributed under the License is distributed on an "AS IS" BASIS,
# WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
# See the License for the specific language governing permissions and
# limitations under the License.

import json
import logging
from abc import ABCMeta, abstractmethod

log = logging.getLogger()


class PatroniDCS(metaclass=ABCMeta):
    @abstractmethod
    def get_dcs_config(self, client, recovery_pod_id, pg_cluster_name):
        """
        returns 2 dict with data from dcs - config from dcs and dcs data itself.
        :type client: OpenshiftClient
        :type recovery_pod_id: str
        :type pg_cluster_name: str
        :rtype: tuple(dict, str)
        """
        pass

    @abstractmethod
    def update_dcs_config(self, oc_client, recovery_pod_id, patroni_dsc):
        pass

    @abstractmethod
    def cleanup_initialization_key(self, oc_client, pg_cluster_name, pod_id):
        """
        :type oc_client: OpenshiftClient
        :param pg_cluster_name:
        :param pod_id:
        :return:
        """
        pass


class PatroniDCSEtcd(PatroniDCS):

    def get_dcs_config(self, oc_client, recovery_pod_id, pg_cluster_name):
        patroni_dsc_data = json.loads(oc_client.oc_exec(recovery_pod_id, "curl etcd:2379/v2/keys/patroni/{}/config"
                                                        .format(pg_cluster_name)))["node"]["value"]
        log.debug("Patroni dcd: {}".format(patroni_dsc_data))
        patroni_dsc = json.loads(patroni_dsc_data)
        log.debug("Patroni dcd (parsed): {}".format(patroni_dsc))
        return patroni_dsc, patroni_dsc_data

    def update_dcs_config(self, oc_client, recovery_pod_id, patroni_dsc):
        log.info("Start dsc configuration update.")
        recovery_conf = patroni_dsc["postgresql"]["recovery_conf"]
        recovery_conf["recovery_target_action"] = "promote"
        with open("dsc.config.tmp", mode="w") as fd:
            json.dump(patroni_dsc, fd)
        oc_client.rsync("./", "{}:/tmp".format(recovery_pod_id))
        log.debug(oc_client.oc_exec(recovery_pod_id,
                                    'python -c \'import etcd, os; '
                                    'fd=open("/tmp/dsc.config.tmp"); data=fd.read(); fd.close(); '
                                    'client = etcd.Client(host="etcd", protocol="http", port=2379); '
                                    'client.write("patroni/{}/config".format(os.getenv("PG_CLUST_NAME")), data)\''))

    def cleanup_initialization_key(self, oc_client, pg_cluster_name, pod_id):
        oc_client.oc_exec(pod_id, "sh -c '"
                                  "curl -XDELETE -s etcd:2379/v2/keys/patroni/${PG_CLUST_NAME}/initialize; "
                                  "curl -XDELETE etcd:2379/v2/keys/patroni/${PG_CLUST_NAME}/optime?recursive=true'")


class PatroniDCSKubernetes(PatroniDCS):

    def get_dcs_config(self, oc_client, recovery_pod_id, pg_cluster_name):
        """
        :type oc_client: OpenshiftClient
        :param recovery_pod_id:
        :param pg_cluster_name:
        :return:
        """
        patroni_dcs_cm = oc_client.get_configmap("{}-config".format(pg_cluster_name))
        log.debug("DCS config map: {}".format(patroni_dcs_cm))
        patroni_dsc_data = patroni_dcs_cm["metadata"]["annotations"]["config"]
        log.debug("Patroni dcs: {}".format(patroni_dsc_data))
        patroni_dsc = json.loads(patroni_dsc_data)
        log.debug("Patroni dcd (parsed): {}".format(patroni_dsc))
        return patroni_dsc, patroni_dsc_data

    def update_dcs_config(self, oc_client, recovery_pod_id, patroni_dsc):
        raise NotImplementedError()

    def cleanup_initialization_key(self, oc_client, pg_cluster_name, pod_id):
        """
        :type oc_client: OpenshiftClient
        :param pg_cluster_name:
        :param pod_id:
        :return:
        """
        log.debug("Try to delete configmap {}-leader".format(pg_cluster_name))
        oc_client.delete_entity("configmap", "{}-leader".format(pg_cluster_name))

        # in case of sync replication we should also remove leader key from cm
        if oc_client.get_entity_safe("configmap", "{}-sync".format(pg_cluster_name)):
            log.debug("Try to delete configmap {}-sync".format(pg_cluster_name))
            patroni_dcs_sync_cm = oc_client.get_configmap("{}-sync".format(pg_cluster_name))
            patroni_dcs_sync_cm["metadata"]["annotations"].pop('leader', None)
            oc_client.replace_object(patroni_dcs_sync_cm)

        log.debug("Try to remove initialize key from configmap {}-config".format(pg_cluster_name))
        for _ in range(1, 5):
            # this is a dirty WA because sometimes initialize did not removed
            patroni_dcs_cm = oc_client.get_configmap("{}-config".format(pg_cluster_name))
            log.debug("DCS config map before : {}".format(patroni_dcs_cm))
            if "initialize" in patroni_dcs_cm["metadata"]["annotations"]:
                del patroni_dcs_cm["metadata"]["annotations"]["initialize"]
            oc_client.replace_object(patroni_dcs_cm)
            patroni_dcs_cm = oc_client.get_configmap("{}-config".format(pg_cluster_name))
            log.debug("DCS config map after applying: {}".format(patroni_dcs_cm))