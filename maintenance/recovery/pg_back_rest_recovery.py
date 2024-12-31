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

from utils_oc import OpenshiftOrchestrator, OpenshiftPyClient
from recovery import cleanup_pg_pod_data_directory
from kubernetes import client
from kubernetes.client import configuration
from kubernetes.client import rest
from kubernetes.stream import stream
from kubernetes.stream.ws_client import ERROR_CHANNEL, STDOUT_CHANNEL, STDERR_CHANNEL
import kubernetes
from utils_common import retry

import time
import requests
import logging
import os
import yaml


log = logging.getLogger()
log.setLevel(logging.DEBUG)
retry_count = 60

skip_tls_verify = os.getenv("OC_SKIP_TLS_VERIFY", "true")
oc_openshift_url = os.getenv("OC_OPENSHIFT_URL", None)
oc_project = os.getenv("POD_NAMESPACE", None)
pg_cluster_name = os.getenv("PG_CLUSTER_NAME", None)

pg_dir = "/var/lib/pgsql/data"
pg_data_dir = "{}/postgresql_${{POD_IDENTITY}}".format(pg_dir)


class PgBackRestRecovery():
    def __init__(self):
        try:
            from kubernetes import config as k8s_config
            k8s_config.load_incluster_config()
            log.info("Using pyclient")
            self.oc_client = OpenshiftPyClient()
            self.oc_client.use_token(oc_url=oc_openshift_url, oc_token="", project=oc_project, skip_tls_verify=skip_tls_verify)
            self._api_client = None
            self.project = os.getenv("POD_NAMESPACE")
            self.oc_orch = OpenshiftOrchestrator(self.oc_client, retry_count)
            try:
                self.apps_api = client.AppsV1Api(self._api_client)
            except:
                self.apps_api = client.AppsV1beta1Api(self._api_client)

        except Exception as e:
            log.exception("Failed to create OpenshiftPyClient")


    def get_patroni_replicas_ip(self, statefulsets):
        r = requests.get("pg-patroni:8008")
        return r.json()['replication']


    def get_patroni_statefulsets(self):
        stateful_sets = self.apps_api.list_namespaced_stateful_set(self.project).items
        return stateful_sets

    def patch_statefulset_cmd(self, stateful_set, stateful_set_name, cmd):

        log.info(f'Going to set {stateful_set_name} with {cmd} command')

        stateful_set.spec.template.spec.containers[0].command = cmd

        self.apps_api.patch_namespaced_stateful_set(stateful_set_name, self.project, stateful_set)


    def scale_statefulset(self, stateful_set_name, replicas):

        log.info(f'Going to set {stateful_set_name} with {replicas} replicas')

        self.apps_api.patch_namespaced_stateful_set_scale(stateful_set_name, self.project, {"spec": {"replicas": replicas}})

    def patch_configmap(self, config_map_name, config_map):

        log.info(f'Going to replace {config_map_name}')
        core_api = client.CoreV1Api(self._api_client)
        core_api.replace_namespaced_config_map(config_map_name, self.project, config_map)


    def delete_master_cm(self):
        try:
            log.info("Delete leader cm")
            body = client.V1DeleteOptions()
            core_api = client.CoreV1Api(self._api_client)
            core_api.delete_namespaced_config_map("patroni-leader", self.project, body=body,)
        except kubernetes.client.rest.ApiException as e:
            if e.reason == "Not Found":
                return
            else:
                return e

    def clean_patroni_cm(self):
        log.info("Delete initialize key")
        cmaps = client.CoreV1Api(self._api_client).list_namespaced_config_map(self.project).items
        for cm in cmaps:
            if cm.metadata.name == 'patroni-config':
                if "initialize" in cm.metadata.annotations:
                    del cm.metadata.annotations["initialize"]
                    self.patch_configmap(cm.metadata.name, cm)
                return cm

    def get_config_map(self, name):
        cmaps = client.CoreV1Api(self._api_client).list_namespaced_config_map(self.project).items
        for cm in cmaps:
            if cm.metadata.name == name:
                return cm

    def get_template_cm(self):
        template_cm = self.get_config_map(f"patroni-{pg_cluster_name}.config.yaml")
        if not template_cm:
            log.info(f"Can't find patroni-{pg_cluster_name}.config.yaml, trying to find {pg_cluster_name}-patroni.config.yaml")
            template_cm = self.get_config_map(f"{pg_cluster_name}-patroni.config.yaml")
        return template_cm

    def create_custom_bootstrap_method(self, target, restore_type):
        template_cm = self.get_template_cm()
        patroni_config_data = template_cm.data['patroni-config-template.yaml']
        log.info(f"Data {patroni_config_data}")
        dict_data = yaml.load(patroni_config_data,Loader=yaml.FullLoader)
        dict_data["bootstrap"]["pgbackrest"] = {
            "command": f"pgbackrest --stanza=patroni --delta --type={restore_type} --target='{target}' --target-action=promote restore",
            "keep_existing_recovery_conf": "True",
            "no_params": "True"
        }
        dict_data["bootstrap"]["method"] = "pgbackrest"
        template_cm.data['patroni-config-template.yaml'] = yaml.dump(dict_data)
        self.patch_configmap(template_cm.metadata.name, template_cm)

    def clean_custom_bootstrap_method(self):
        log.info(f"Pop custom bootstrap method from patroni-template config map")
        template_cm = self.get_template_cm()
        patroni_config_data = template_cm.data['patroni-config-template.yaml']
        dict_data = yaml.load(patroni_config_data,Loader=yaml.FullLoader)
        dict_data["bootstrap"].pop("method", None)
        template_cm.data['patroni-config-template.yaml'] = yaml.dump(dict_data)
        self.patch_configmap(template_cm.metadata.name, template_cm)

    def upgrade_stanza(self):
        # wait for leader to upgrade stanza
        from requests.adapters import HTTPAdapter, Retry
        logging.basicConfig(level=logging.DEBUG)
        s = requests.Session()
        retries = Retry(total=3600, backoff_factor=1, status_forcelist=[ 502, 503, 504 ])
        s.mount('http://', HTTPAdapter(max_retries=retries))
        r = s.post("http://pgbackrest:3000/upgrade")
        log.info(f'{r.status_code}, {r.text}')

    def restore_pod(self, pod_name, backup_id):
        # wait for pod to restore
        log.info(f'Will invoke restore command for pod {pod_name}')
        from requests.adapters import HTTPAdapter, Retry
        logging.basicConfig(level=logging.DEBUG)
        s = requests.Session()
        retries = Retry(total=3600, backoff_factor=1, status_forcelist=[ 502, 503, 504 ])
        s.mount('http://', HTTPAdapter(max_retries=retries))
        r = s.post(f"http://{pod_name}.backrest-headless:3000/restore", data={'backupId':backup_id})
        log.info(f'{r.status_code}, {r.text}')
        return r.status_code

    def perform_restore(self):
        backup_id = '' if not os.getenv("SET") else os.getenv("SET")
        restore_type = '' if not os.getenv("TYPE") else os.getenv("TYPE")
        target = '' if not os.getenv("TARGET") else os.getenv("TARGET")


        http_codes = {}
        stateful_sets =  self.get_patroni_statefulsets()
        for stateful_set in stateful_sets:

            stateful_set_name = stateful_set.metadata.name
            pod_name = stateful_set.metadata.name + "-0"

            cmd = ["sh", "-c", "while true ; do sleep 3600; done"]

            self.patch_statefulset_cmd(stateful_set, stateful_set_name, cmd)
            time.sleep(5)
            #Just in case when pods could be scaled 0
            self.scale_statefulset(stateful_set_name,1)
            if not self.wait_for_pod(pod_name, attempts=5):
                raise Exception("Pod {} is not ready".format(pod_name))
            self.cleanup_patroni_data(pod_name, stateful_set_name, False)
            time.sleep(15)


        stateful_sets =  self.get_patroni_statefulsets()
        for stateful_set in stateful_sets:
            stateful_set_name = stateful_set.metadata.name
            pod_name = stateful_set.metadata.name + "-0"
            if target:
                log.info(f"Target has been provided, so starting PITR for pod {pod_name}")
                self.create_custom_bootstrap_method(target, restore_type)
                self.clean_patroni_cm()
                self.delete_master_cm()
            else:
                log.info(f"Starting full restore procedure for pod {pod_name}")
                http_codes[stateful_set_name] = self.restore_pod(pod_name, backup_id)

            if target or http_codes[stateful_set_name] == 200:
                log.info(f"Restore return 200 http state, so remove sleep cmd")
                self.patch_statefulset_cmd(stateful_set, stateful_set_name, [])
                time.sleep(5)
                self.scale_statefulset(stateful_set_name,0)
                time.sleep(15)
                self.scale_statefulset(stateful_set_name,1)
            else:
                log.error(f'Restore procedure for {stateful_set_name} ends with error. It was {http_codes[stateful_set_name]}')
                return
            if not self.wait_for_pod(pod_name, attempts=5):
                raise Exception("Pod {} is not ready".format(pod_name))
        self.clean_custom_bootstrap_method()
        self.upgrade_stanza()
        print("Done")




    def cleanup_patroni_data(self, pod_name, container_name, preserve_old_files):
        log.info("Try to cleanup data directory for pod {}".format(pod_name))
        if preserve_old_files == "yes":
            self.oc_client.oc_exec(pod_name, container_name, "sh -c 'mv {} {}_backup_$(date +%s); ls -ll {}'".format(pg_data_dir, pg_data_dir, pg_dir))
            log.info("Old files were preserved on volume. Cleanup if needed.")
        self.oc_exec(pod_name, container_name, " sh -c 'rm -rf {}; mkdir {}; chmod 700 {}' ".format(pg_data_dir, pg_data_dir, pg_data_dir))


    @retry(tries=30, delay=5)
    def oc_exec(self, pod_id, container_name, command):
        log.debug(f"Try to execute '{command}' on pod {pod_id}")
        core_api = client.CoreV1Api(self._api_client)

        exec_command = [
            '/bin/sh', '-c', command
        ]

        try:
            resp = stream(core_api.connect_get_namespaced_pod_exec,
                          pod_id,
                          self.project,
                          container=container_name,
                          command=exec_command,
                          stderr=True, stdin=False,
                          stdout=True, tty=False, _preload_content=True, _request_timeout=60)

            log.info(f"Command executed. Result: {resp}")

            if resp:
                log.debug(f"Command output: {resp}")
                if "No such file or directory" in resp or "cannot remove" in resp:
                    log.info("Directory already cleaned up or removal issue detected.")
                    return resp  # Exit early if the directory is already cleaned up or a removal issue was detected

            return resp

        except Exception as e:
            log.error(f"Exception occurred while executing command: {e}")
            raise


    def wait_for_pod(self, pod_name, attempts=5):
        for i in range(1, attempts):
            time.sleep(15)
            status = self.get_pod_status(pod_name)
            log.info("Pod state is {}".format(status))
            if status and status.lower() == "running":
                return True
            else:
                log.info("Retrying...")
        log.info("Can't get pod {} status".format(pod_name))
        return False


    def get_pod_status(self, pod_name):
        core_api = client.CoreV1Api(self._api_client)
        pods = core_api.list_namespaced_pod(self.project).items
        for x in pods:
            if x.metadata.name == pod_name:
                return x.status.phase
            else:
                log.info("Pod {} not found".format(pod_name))

if __name__ == "__main__":
    recovery = PgBackRestRecovery()
    recovery.perform_restore()






