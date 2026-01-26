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

import argparse
import os
import random
import requests
import string
import json
import yaml
import logging
import time
import sys
import utils_oc
import utils_pg
import re
from utils_common import RecoveryException
from utils_dcs import PatroniDCS, PatroniDCSEtcd, PatroniDCSKubernetes

import dateutil.parser
import dateutil.zoneinfo
import dateutil.tz

from multiprocessing.pool import ThreadPool
import concurrent.futures
import asyncio


retry_count = 60
pg_dir = "/var/lib/pgsql/data"
pg_data_dir = "{}/postgresql_${{POD_IDENTITY}}".format(pg_dir)

log = logging.getLogger()
log.setLevel(logging.DEBUG)

ch = logging.StreamHandler(sys.stdout)
ch.setLevel(logging.INFO)
formatter = logging\
    .Formatter('%(asctime)s - %(thread)d - %(name)s:%(funcName)s#%(lineno)d - %(levelname)s - %(message)s')
ch.setFormatter(formatter)
log.addHandler(ch)

ch = logging.FileHandler("recovery.debug.{}.log".format(int(time.time())), mode='w', encoding=None, delay=False)
ch.setLevel(logging.DEBUG)
formatter = logging \
    .Formatter('%(asctime)s - %(thread)d - %(name)s:%(funcName)s#%(lineno)d - %(levelname)s - %(message)s')
ch.setFormatter(formatter)
log.addHandler(ch)

RECOVERY_EXCEPTION_NO_RESTORE_COMMAND = """FAILURE: cannot find restore_command in dcs and in patroni config template.
Put valid value into dcs (etcd or '{}-config' configmap) on path 'postgresql.recovery_conf.restore_command'.
If dcs uninitialized put it into configmap 'patroni-{}.config.yaml' on path 'bootstrap.dcs.postgresql.recovery_conf.restore_command'
Value can be '' if wal_archive is disabled and 'curl -v -S -f --connect-timeout 3 postgres-backup-daemon:8082/archive/get?filename=%f -o %p' if enabled. 
"""


class PoolLogger(object):

    def __init__(self, callable):
        self.__callable = callable

    def __call__(self, *args, **kwargs):
        try:
            result = self.__callable(*args, **kwargs)
        except Exception as e:
            log.exception("Task failed with error")
            raise e
        return result


class LoggingPool(ThreadPool):

    def apply_async(self, func, args=(), kwds={}, callback=None):
        return ThreadPool.apply_async(self, PoolLogger(func), args, kwds, callback)

# todo[anin] OpenshiftPyClient cannot work in parallel!




def cleanup_pg_pod_data_directory(oc_client, pod_id, preserve_old_files):
    log.info("Try to cleanup data directory for pod {}".format(pod_id))
    if preserve_old_files == "yes":
        oc_client.oc_exec(pod_id, "sh -c 'mv {} {}_backup_$(date +%s); ls -ll {}'".format(pg_data_dir, pg_data_dir, pg_dir))
        log.info("Old files were preserved on volume. Cleanup if needed.")
    oc_client.oc_exec(pod_id, " sh -c 'rm -rf {}; mkdir {}; chmod 700 {}' ".format(pg_data_dir, pg_data_dir, pg_data_dir))


def update_configmap(oc_client, dcs_storage, recovery_pod_id, pg_cluster_name, restore_version,
                     recovery_target_timeline, recovery_target_inclusive,
                     recovery_target_name, recovery_target_time, recovery_target_xid, recovery_target,):
    log.info("Update configmap with actual bootstrap method")
    patroni_config_cm = oc_client.get_configmap("patroni-{}.config.yaml".format(pg_cluster_name))
    if not patroni_config_cm:
        log.info("Can't find patroni-{}.config.yaml, trying to find {}-patroni.config.yaml".format(pg_cluster_name, pg_cluster_name))
        patroni_config_cm = oc_client.get_configmap("{}-patroni.config.yaml".format(pg_cluster_name))
    patroni_config_data = patroni_config_cm['data']['patroni-config-template.yaml']
    log.debug("Patroni config template from configmap: \n {}".format(patroni_config_data))
    patroni_config = yaml.safe_load(patroni_config_data)
    log.debug("Patroni config template from configmap (parsed): \n {}".format(patroni_config))

    patroni_dsc, patroni_dsc_data = dcs_storage.get_dcs_config(oc_client, recovery_pod_id, pg_cluster_name)

    targets = {
        "recovery_target_name": recovery_target_name,
        "recovery_target_time": recovery_target_time,
        "recovery_target_xid": recovery_target_xid,
        "recovery_target": recovery_target
    }

    recovery_target_key = ""
    recovery_target_value = ""

    for (key, value) in list(targets.items()):
        if value:
            recovery_target_key = key
            recovery_target_value = value

    patroni_config["bootstrap"]["daemon_recovery"] = {
        "command": "/daemon-recovery.sh  --restore-version={}".format(restore_version),
        "recovery_conf": {
            "recovery_target_action": "promote",
            "recovery_target_timeline": recovery_target_timeline,
            "recovery_target_inclusive": recovery_target_inclusive
        }
    }

    restore_command = get_restore_command(oc_client, dcs_storage, pg_cluster_name, recovery_pod_id)
    # This is a WA for PG12. In PG12 next IF condition were introduced:
    # https://github.com/postgres/postgres/blob/REL_12_STABLE/src/backend/access/transam/xlog.c#L5397
    if not restore_command:
        restore_command = "echo"
    patroni_config["bootstrap"]["daemon_recovery"]["recovery_conf"]["restore_command"] = restore_command

    if recovery_target_key:
        patroni_config["bootstrap"]["daemon_recovery"]["recovery_conf"][recovery_target_key] = recovery_target_value

    patroni_config["bootstrap"]["method"] = "daemon_recovery"
    log.debug("Patroni config template after update: {}".format(patroni_config))
    updated_patroni_config = yaml.safe_dump(patroni_config, default_flow_style=False, encoding="utf-8", allow_unicode=True)
    log.debug("Patroni config template after update: {}".format(updated_patroni_config))
    # encoded_patroni_config = json.dumps(updated_patroni_config)
    # log.debug("Encoded patroni config template after update: {}".format(encoded_patroni_config))
    patroni_config_cm['data']['patroni-config-template.yaml'] = updated_patroni_config.decode()
    oc_client.apply_object(patroni_config_cm)


def remove_bootstrap_method(oc_client, pg_cluster_name):
    log.info("Remove bootstrap method from configmap")
    patroni_config_cm = oc_client.get_configmap("patroni-{}.config.yaml".format(pg_cluster_name))
    if not patroni_config_cm:
        log.info("Can't find patroni-{}.config.yaml, trying to find {}-patroni.config.yaml".format(pg_cluster_name, pg_cluster_name))
        patroni_config_cm = oc_client.get_configmap("{}-patroni.config.yaml".format(pg_cluster_name))
    patroni_config_data = patroni_config_cm['data']['patroni-config-template.yaml']
    log.debug("Patroni config template from configmap: \n {}".format(patroni_config_data))
    patroni_config = yaml.safe_load(patroni_config_data)
    log.debug("Patroni config template from configmap (parsed): \n {}".format(patroni_config))

    patroni_config["bootstrap"].pop("method", None)
    log.debug("Patroni config template after update: {}".format(patroni_config))
    updated_patroni_config = yaml.safe_dump(patroni_config, default_flow_style=False, encoding="utf-8", allow_unicode=True)
    log.debug("Patroni config template after update: {}".format(updated_patroni_config))

    patroni_config_cm['data']['patroni-config-template.yaml'] = updated_patroni_config.decode()
    oc_client.apply_object(patroni_config_cm)



def perform_bootstrap_recovery(oc_client, oc_orch, pg, dcs_storage,
                               pg_depl_name, pg_cluster_name,
                               preserve_old_files, restore_version,
                               recovery_target_timeline, recovery_target_inclusive,
                               recovery_target_name, recovery_target_time, recovery_target_xid, recovery_target,
                               deployment_type):
    '''
    :type oc_client: OpenshiftClient
    :type oc_orch: OpenshiftOrchestrator
    :type pg: PostgresqlClient
    :type dcs_storage: PatroniDCS
    :param pg_depl_name:
    :param pg_cluster_name:
    :param preserve_old_files:
    :param restore_version:
    :param recovery_target_timeline:
    :param recovery_target_inclusive:
    :param recovery_target_name:
    :param recovery_target_time:
    :param recovery_target_xid:
    :param recovery_target:
    :param deployment_type:
    :return:
    '''
    log.info("Start recovery procedure using bootstrap config")

    log.info("Replace current postgresql deployments with test versions")

    pool = concurrent.futures.ThreadPoolExecutor(max_workers=4)
    loop = asyncio.get_event_loop()

    statefulsets = None
    initial_sts_replicas = None

    if deployment_type == "statefulset":
        initial_replicas_desc = oc_client.get_cluster_pods_desc(pg_cluster_name)
        initial_replicas = [pod["metadata"]["name"] for pod in initial_replicas_desc]

        statefulsets = sorted({statefulset_from_pod(p) for p in initial_replicas})
        initial_sts_replicas = {sts: oc_client.get_stateful_set_replicas_count(sts) for sts in statefulsets}

    if deployment_type == "dc":
        deployments = oc_client.get_deployment_names(pg_depl_name)
        tasks = [loop.run_in_executor(
            pool,
            oc_orch.replace_command_on_dc,
            p, ["sh", "-c", "while true ; do sleep 3600; done"]) for p in deployments]
        loop.run_until_complete(asyncio.gather(*tasks))
    elif deployment_type == "deployment":
        log.info("Will set test version on postgresql deployments: {}".format(pg_depl_name))
        deployments = oc_client.get_deployment_names(pg_depl_name, deployment_type)
        tasks = [loop.run_in_executor(
            pool,
            oc_orch.replace_command_on_deployment,
            p, ["sh", "-c", "while true ; do sleep 3600; done"]) for p in deployments]

        loop.run_until_complete(asyncio.gather(*tasks))

    elif deployment_type == "statefulset":
        for sts in statefulsets:
            oc_orch.replace_command_on_statefulset(sts, ["sh", "-c", "while true ; do sleep 3600; done"])



    if deployment_type != "statefulset":
        initial_replicas_desc = oc_client.get_cluster_pods_desc(pg_cluster_name)
        initial_replicas = [pod["metadata"]["name"] for pod in initial_replicas_desc]

    # in case of statefulset need to get number of replicas for scaling back
    # if deployment_type == "statefulset":
    #     initial_replicas_num = oc_client.get_stateful_set_replicas_count("patroni")

    recovery_pod_id = list([pod for pod in initial_replicas_desc if not list([env for env in pod["spec"]["containers"][0]["env"] if env["name"] == "DR_MODE" and "value" in env and env["value"].lower() == "true"])])[0]["metadata"]["name"]
    log.info("Will use for procedure pod {} from {}".format(recovery_pod_id, initial_replicas))

    log.info("Update configmap")

    update_configmap(
         oc_client, dcs_storage, recovery_pod_id, pg_cluster_name, restore_version,
         recovery_target_timeline, recovery_target_inclusive,
         recovery_target_name, recovery_target_time, recovery_target_xid, recovery_target)

    log.info("Cleanup initialization key")
    dcs_storage.cleanup_initialization_key(
    oc_client, pg_cluster_name, recovery_pod_id)

    log.info("Cleanup data directories")
    for p in initial_replicas:
        cleanup_pg_pod_data_directory(
        oc_client, p, preserve_old_files)

    # restore command on deployments
    if deployment_type == "dc":
        recovery_dc = list([p for p in deployments if p in recovery_pod_id])[0]
        log.info("Restore command for replica deployment and leave them shut down")
        tasks = [loop.run_in_executor(
            pool,
            oc_orch.replace_command_on_dc,
            p, None, False) for p in [d for d in deployments if d != recovery_dc]]
        log.info("Restore command for deployment {}".format(recovery_dc))
        tasks.append(loop.run_in_executor(
            pool,
            oc_orch.replace_command_on_dc,
            recovery_dc, None))

        loop.run_until_complete(asyncio.gather(*tasks))

        # determine new pod for recovery deployment
        recovery_replicas = oc_client.get_replicas_names(recovery_dc)
        recovery_pod_id = recovery_replicas[0]
    elif deployment_type == "deployment":
        deployments = oc_client.get_deployment_names(pg_depl_name, deployment_type)
        log.info("Deployments: {}".format(deployments))
        recovery_dc = list([p for p in deployments if p in recovery_pod_id])[0]
        log.info("Restore command for replica deployment and leave them shut down")
        tasks = [loop.run_in_executor(
            pool,
            oc_orch.replace_command_on_deployment,
            p, None, True) for p in [d for d in deployments if d != recovery_dc]]
        log.info("Restore command for deployment {}".format(recovery_dc))
        loop.run_until_complete(asyncio.gather(*tasks))
        oc_orch.replace_command_on_deployment(recovery_dc, None)
        time.sleep(60)
        recovery_pod_id = oc_client.get_pods_by_label("app={}".format(pg_cluster_name))[0]
    elif deployment_type == "statefulset":
        # set command as None for statefulset
        for sts in statefulsets:
            oc_orch.replace_command_on_statefulset(sts, None, False)
        recovery_sts = statefulset_from_pod(recovery_pod_id)
        oc_orch.scale_stateful_set(recovery_sts, 1)
        recovery_pod_id = oc_client.get_cluster_pods(pg_cluster_name)[0]
    log.info(recovery_pod_id)
    # wait while pod is up
    if not oc_client.is_pod_ready(recovery_pod_id, attempts=5):
        raise Exception("Pod {} is not ready".format(recovery_pod_id))

    log.info("Cleanup initialization key")
    dcs_storage.cleanup_initialization_key(
    oc_client, pg_cluster_name, recovery_pod_id)

    cleanup_pg_pod_data_directory(oc_client, recovery_pod_id, preserve_old_files)

    # wait while database will complete bootstrap and exit from recovery mode
    log.info("Wait while patroni pod: {} will complete bootstrap from backup.".format(recovery_pod_id))
    oc_orch.wait_for_one_of_records_in_logs_since(
        recovery_pod_id,
        ["no action. I am ({}), the leader with the lock".format(recovery_pod_id)],  # prefix can be "INFO:" or  "[INFO][source=patroni]"
        time.time(),
        "Bootstrap from backup daemon is in progress.",
        [" bootstrap in progress", " waiting for end of recovery after bootstrap"]
    )
    log.info("Wait while postgres will exit from recovery mode.")
    pg.wait_pg_recovery_complete(recovery_pod_id)

    log.info("Try to set password for postgres and replicator users")

    def split(line):
        pos = line.find("=")
        if pos > 0:
            return line[0:pos], line[pos+1:]
        else:
            return line, ""

    pg_user, pg_password = oc_client.get_secret_data("postgres-credentials")
    log.info(
        pg.execute_local_query(recovery_pod_id, "ALTER USER {} WITH PASSWORD '{}'"
                               .format(pg_user, pg_password)))
    replicator_user, replicator_password = oc_client.get_secret_data("replicator-credentials")
    log.info(
        pg.execute_local_query(recovery_pod_id, "ALTER USER {} WITH PASSWORD '{}'"
                               .format(replicator_user, replicator_password)))

    bootstrap_method_cleanup = loop.run_in_executor(
        pool,
        remove_bootstrap_method,
        oc_client,
        pg_cluster_name)
    loop.run_until_complete(asyncio.gather(bootstrap_method_cleanup))
    # restore replicas
    if deployment_type == "dc":
        for dc in deployments:
            if dc != recovery_dc:
                log.info("Scale up dc {}".format(dc))
                oc_orch.ensure_scale(dc, 1)
                oc_orch.wait_replicas(dc, 1, running=True)
    elif deployment_type == "deployment":
        for dc in deployments:
            if dc != recovery_dc:
                log.info("Scale up deployment {}".format(dc))
                oc_orch.ensure_scale(dc, 1, "deployment")
    elif deployment_type == "statefulset":
        for sts, cnt in initial_sts_replicas.items():
            oc_orch.scale_stateful_set(sts, cnt)

    # check if database working on all nodes
    replicas = oc_client.get_cluster_pods(pg_cluster_name)
    for pod_id in replicas:
        if pod_id != recovery_pod_id:
            cleanup_pg_pod_data_directory(oc_client, pod_id, preserve_old_files)
            log.info("Wait while patroni on replica {} will complete bootstrap from master.".format(pod_id))
            if not oc_client.is_pod_ready(pod_id, attempts=5):
                raise Exception("Pod {} is not ready".format(pod_id))
            oc_orch.wait_for_record_in_logs_since(
                pod_id,
                "no action. I am ({}), a secondary, and following a leader ({})".format(pod_id, recovery_pod_id),
                time.time(),
                "Bootstrap from master is in progress.",
                " bootstrap from leader"
            )
            log.info("Wait while replica database will start on pod {}.".format(pod_id))
            pg.wait_database(pod_id)


def statefulset_from_pod(pod_name: str) -> str:
    return re.sub(r"-\d+$", "", pod_name)


def download_archive(oc_client, recovery_pod_id, restore_version):
    if restore_version:
        oc_client.oc_exec(recovery_pod_id, "sh -c 'cd {} ; curl -u postgres:\"$PG_ROOT_PASSWORD\" postgres-backup-daemon:8081/get?id={} | tar -xzf - '"
                .format(pg_data_dir, restore_version))
    else:
        oc_client.oc_exec(recovery_pod_id, "sh -c 'cd {} ; curl -u postgres:\"$PG_ROOT_PASSWORD\" postgres-backup-daemon:8081/get | tar -xzf - '"
                .format(pg_data_dir))


def update_string_value_in_map(map, key, value):
    if value:
        map[key] = value
    else:
        map.pop(key, None)


def prepare_recovery_conf(recovery_pod_id, patroni_dsc,
                          recovery_target_timeline, recovery_target_inclusive,
                          recovery_target_name, recovery_target_time, recovery_target_xid, recovery_target):
    if "recovery_conf" not in patroni_dsc["postgresql"]:
        patroni_dsc["postgresql"]["recovery_conf"] = {}

    recovery_conf = patroni_dsc["postgresql"]["recovery_conf"]
    update_string_value_in_map(recovery_conf, "recovery_target_inclusive", recovery_target_inclusive)
    update_string_value_in_map(recovery_conf, "recovery_target_timeline", recovery_target_timeline)

    update_string_value_in_map(recovery_conf, "recovery_target_name", recovery_target_name)
    update_string_value_in_map(recovery_conf, "recovery_target_time", recovery_target_time)
    update_string_value_in_map(recovery_conf, "recovery_target_xid", recovery_target_xid)
    update_string_value_in_map(recovery_conf, "recovery_target", recovery_target)

def is_recovery_target_specified(recovery_target_name, recovery_target_time, recovery_target_xid, recovery_target):
    return recovery_target_name or recovery_target_time or recovery_target_xid or recovery_target


def validate_restore_version(backups, restore_version):
    if not backups:
        raise RecoveryException("FAILURE: cannot find any available backups.")

    if restore_version:
        if restore_version not in backups:
            raise RecoveryException("FAILURE: RESTORE_VERSION={} does not match any existing backup. "
                                    "List of available backups: {}.".format(restore_version, backups))
    else:
        log.info("Proceed with empty RESTORE_VERSION.")


def validate_recovery_target_parameters(
        oc_client, dcs_storage, pg_cluster_name, backup_daemon_pod_id,  restore_version,
        recovery_target_name, recovery_target_time, recovery_target_xid, recovery_target):
    '''

    :type oc_client: OpenshiftClient
    :type dcs_storage: PatroniDCS
    :param pg_cluster_name:
    :param backup_daemon_pod_id:
    :param restore_version:
    :param recovery_target_name:
    :param recovery_target_time:
    :param recovery_target_xid:
    :param recovery_target:
    :return:
    '''
    if is_recovery_target_specified(recovery_target_name, recovery_target_time, recovery_target_xid, recovery_target) \
            and (not restore_version and not recovery_target_time):
        raise RecoveryException("FAILURE: cannot perform PITR "
                                "without specified backup or recovery_target_time")

    if is_recovery_target_specified(recovery_target_name, recovery_target_time, recovery_target_xid, recovery_target):
        archive_mode = get_dcs_config_params(oc_client, dcs_storage, pg_cluster_name, backup_daemon_pod_id,
                                             ["postgresql", "parameters", "archive_mode"], use_template=True)
        if archive_mode != "on":
            raise RecoveryException("FAILURE: Cannot perform PITR recovery without WAL archive.")

        if not get_restore_command(oc_client, dcs_storage, pg_cluster_name, backup_daemon_pod_id):
            raise RecoveryException("FAILURE: Cannot perform PITR recovery without restore_command.")


def check_if_replication_works(oc_client, pg, pg_cluster_name):
    log.info("Start replication check")
    replicas = oc_client.get_cluster_pods_desc(pg_cluster_name)
    master_replicas = list([p for p in replicas if "pgtype" in p["metadata"]["labels"] and
                                            p["metadata"]["labels"]["pgtype"] == "master"])

    if len(master_replicas) > 1:
        raise RecoveryException("FAILURE: Several masters detected. Healthy PostgreSQL cluster should have one master.")

    if len(master_replicas) == 0:
        raise RecoveryException("FAILURE: Cannot find master. Healthy PostgreSQL cluster should have one master.")

    master_pod_id = master_replicas[0]["metadata"]["name"]

    id = int(time.time()) * 100000 + random.randint(1, 99999)
    uuid = ''.join(random.choice(string.ascii_letters + string.digits) for _ in range(32))
    pg.execute_local_query(master_pod_id, "create table IF NOT EXISTS test_recovery (id bigint primary key not null, value text not null)")
    pg.execute_local_query(master_pod_id, "insert into test_recovery values ({}, '{}')".format(id, uuid))
    resulted_uuid = pg.execute_local_query(master_pod_id, "select value from test_recovery where id={}".format(id))
    log.debug("Select result from master {}: {}".format(master_pod_id, resulted_uuid))

    if resulted_uuid != uuid:
        raise RecoveryException("FAILURE: Unexpected result '{}' on master while expected '{}'. "
                                "Master does not work or does not perform write operations.".format(resulted_uuid, uuid))

    log.info("Record was inserted in master")

    replica_names = list([p["metadata"]["name"] for p in replicas])

    for replica in replica_names:
        if replica != master_pod_id:
            query_result = None
            for i in range(1, retry_count):
                log.info("Try to check if database on pod {} receives data from master".format(replica))
                query_result = pg.execute_local_query(replica, "select value from test_recovery where id={}".format(id))
                log.debug("Query result from replica: {}".format(query_result))
                if query_result == uuid:
                    break
                time.sleep(1)

            if query_result != uuid:
                raise RecoveryException("FAILURE: Unexpected result '{}' while expected '{}'. "
                                        "Replica was not able to receive changes from master.".format(query_result, uuid))


def get_tzinfos():
    # todo[anin] implement for different versions of dateutils
    if "get_zonefile_instance" in dir(dateutil.zoneinfo):
        return dateutil.zoneinfo.get_zonefile_instance().zones
    if "getzoneinfofile_stream" in dir(dateutil.zoneinfo):
        return dateutil.zoneinfo.ZoneInfoFile(dateutil.zoneinfo.getzoneinfofile_stream()).zones
    return {}


def enrich_backup(backup_info, tzinfos, cluster_tz_name):
    backup_info["parsed"] = dateutil.parser.parse(backup_info["id"], tzinfos=tzinfos)
    if not backup_info["parsed"].tzinfo:
        backup_info["parsed"] = backup_info["parsed"].replace(tzinfo=tzinfos[cluster_tz_name])
    return backup_info


def safe_dict_get(data, path, default=None):
    value = data
    for x in path:
        value = value.get(x)
        if not value:
            break
    return value if value is not None else default


def get_dcs_config_params(oc_client, dcs_storage, pg_cluster_name, pod_id, path, use_template=True):
    patroni_dsc, _ = dcs_storage.get_dcs_config(oc_client, pod_id, pg_cluster_name)
    value = safe_dict_get(patroni_dsc, path)
    if value is None and use_template:
        log.warning("Cannot get '{}' from dcs. It is possible that dcs state was removed or lost.".format(path))
        patroni_template_cm = oc_client.get_configmap("patroni-{}.config.yaml".format(pg_cluster_name))
        if not patroni_template_cm:
            log.info("Can't find patroni-{}.config.yaml, trying to find {}-patroni.config.yaml".format(pg_cluster_name, pg_cluster_name))
            patroni_template_cm = oc_client.get_configmap("{}-patroni.config.yaml".format(pg_cluster_name))
        patroni_template_data = patroni_template_cm.get("data", {}).get("patroni-config-template.yaml")
        patroni_template = yaml.load(patroni_template_data)
        log.debug("Patroni config template from configmap (parsed): \n {}".format(patroni_template))
        patroni_template_dcs = safe_dict_get(patroni_template, ["bootstrap", "dcs"], {})
        log.debug("Patroni dcs config template from configmap (parsed): \n {}".format(patroni_template_dcs))
        value = safe_dict_get(patroni_template_dcs, path)
    return value


def get_restore_command(oc_client, dcs_storage, pg_cluster_name, backup_daemon_pod_id):
    return get_dcs_config_params(oc_client, dcs_storage, pg_cluster_name, backup_daemon_pod_id,
                                 ["postgresql", "recovery_conf", "restore_command"], use_template=True)


def perform_recovery(oc_openshift_url, oc_username, oc_password, oc_project,
                     pg_cluster_name, pg_depl_name, preserve_old_files, restore_version,
                     recovery_target_timeline, recovery_target_inclusive,
                     recovery_target_name, recovery_target_time, recovery_target_xid, recovery_target,
                     oc_path, oc_config_file, skip_tls_verify=False):

    # todo[anin] OpenshiftPyClient requires more testing (statefulsets for example)
    # so leave OpenshiftShellClient as default for backward compatibility
    try:
        from kubernetes import config as k8s_config
        k8s_config.load_incluster_config()
        log.info("Using pyclient")
        oc_client = utils_oc.OpenshiftPyClient()
        oc_client.use_token(oc_url=oc_openshift_url, oc_token="", project=oc_project, skip_tls_verify=skip_tls_verify)
    except Exception as e:
        log.exception("Failed to create OpenshiftPyClient, proceeding with OpenshiftShellClient")
        oc_client = utils_oc.OpenshiftShellClient(oc_path, oc_config_file)
        oc_client.login(oc_url=oc_openshift_url, username=oc_username, password=oc_password, project=oc_project,
                       skip_tls_verify=skip_tls_verify)

    oc_orch = utils_oc.OpenshiftOrchestrator(oc_client, retry_count)
    pg = utils_pg.PostgresqlClient(oc_client, retry_count)

    if oc_client.get_entity_safe("configmap", "{}-config".format(pg_cluster_name)):
        log.info("Use kubernetes as DCS storage")
        dcs_storage = PatroniDCSKubernetes()
    elif oc_client.get_entity_safe("svc", "etcd"):
        log.info("Use etcd as DCS storage")
        dcs_storage = PatroniDCSEtcd()
    else:
        raise RecoveryException("Cannot find configmap {}-config or service etcd and guess dcs type."
                                .format(pg_cluster_name))
    deployment_type = "dc"

    # here we are trying to check if this is a deployment via operator
    if oc_client.get_entity_safe("deployment", "postgres-backup-daemon"):
        log.info("Deployments are used for Pods management")
        deployment_type = "deployment"

    #Changed Order here of deployment check
    if oc_client.get_entity_safe("statefulset", "patroni"):
        log.info("Statefulset is used for Pod management")
        deployment_type = "statefulset"

    deployment_type = "statefulset"

    log.info("Try to validate if backup daemon running")
    backup_daemon_replicas = oc_client.get_postgres_backup_daemon_pod()
    if not backup_daemon_replicas:
        raise RecoveryException("FAILURE: cannot find backup daemon pod."
                                "Recovery procedure requires running backup daemon pod.")
    backup_daemon_pod_id = backup_daemon_replicas[0]
    log.info("Found backup daemon pod {}".format(backup_daemon_pod_id))

    log.info("Try to validate recovery target parameters")
    validate_recovery_target_parameters(
        oc_client, dcs_storage, pg_cluster_name, backup_daemon_pod_id, restore_version,
        recovery_target_name, recovery_target_time, recovery_target_xid, recovery_target)

    log.info("Try to validate restore_command setting")
    restore_command = get_restore_command(oc_client, dcs_storage, pg_cluster_name, backup_daemon_pod_id)
    log.info("Recovery command printed in start ={}".format(restore_command))
    if restore_command is None:
        raise RecoveryException(RECOVERY_EXCEPTION_NO_RESTORE_COMMAND.format(pg_cluster_name, pg_cluster_name))

    ##Added backup status check here
    log.info("Try to validate if the backup status is successful")
    backup_status = check_backup_status(restore_version)
    if not backup_status:
        raise RecoveryException("FAILURE: Backup with id {} has an unsuccessful status. "
                                     "Recovery cannot proceed.".format(restore_version))


    if restore_version:
        log.info("Try to validate backup {} against list of backups from {}".format(restore_version,
                                                                                    backup_daemon_pod_id))

        backup_list = requests.get("http://localhost:8081/list", auth=('postgres', os.getenv('POSTGRES_PASSWORD')))
        validate_restore_version(backup_list.json(), restore_version)
    elif recovery_target_time:
        log.info("Try to find backup id from specified recovery_target_time={}".format(recovery_target_time))
        backup_list = requests.get("http://localhost:8081/list", auth=('postgres', os.getenv('POSTGRES_PASSWORD')))
        cluster_tz_name = oc_client.oc_exec(backup_daemon_pod_id, 'date "+%Z"').strip()
        log.debug("Cluster time zone: {}" + cluster_tz_name)

        tzinfos = get_tzinfos()
        rt_time = dateutil.parser.parse(recovery_target_time, tzinfos=tzinfos)
        if not rt_time.tzinfo:
            rt_time = rt_time.replace(tzinfo=tzinfos[cluster_tz_name])

        log.debug("Parsed time: {}".format(rt_time))
        possible_backup_list = list([p for p in [enrich_backup(p, tzinfos, cluster_tz_name) for p in [p for p in list(backup_list.json().values()) if not p["failed"]]] if p["parsed"] and p["parsed"] < rt_time])

        if not possible_backup_list:
            raise RecoveryException("FAILURE: Cannot find backup for specified recovery_target_time='{}'. "
                                    "Try to specify restore_version manually. "
                                    "Available backups: {}".format(recovery_target_time, backup_list.json()))

        possible_backup = max(possible_backup_list, key=lambda p: p["parsed"])
        restore_version = possible_backup["id"]

        log.info("Selected restore_version is {}".format(restore_version))
    else:
        raise RecoveryException("FAILURE: Cannot perform recovery without restore_version and recovery_target_time. "
                                "Please specify at least one of them.")

    patroni_cm = oc_client.get_entity_safe("configmap", "patroni-{}.config.yaml".format(pg_cluster_name))
    if not patroni_cm:
        log.info("Can't find patroni-{}.config.yaml, trying to find {}-patroni.config.yaml".format(pg_cluster_name, pg_cluster_name))
        patroni_cm = oc_client.get_entity_safe("configmap", "{}-patroni.config.yaml".format(pg_cluster_name))
    if not patroni_cm:
        raise RecoveryException("FAILURE: Cannot find configmap patroni-{}.config.yaml. "
                                "Check if recovery scripts version complies with cluster version".format(pg_cluster_name))
    perform_bootstrap_recovery(oc_client, oc_orch, pg, dcs_storage,
                               pg_depl_name, pg_cluster_name,
                               preserve_old_files, restore_version,
                               recovery_target_timeline, recovery_target_inclusive,
                               recovery_target_name, recovery_target_time, recovery_target_xid, recovery_target,
                               deployment_type)

    if not os.getenv("SKIP_REPLICATION_CHECK", False):
        check_if_replication_works(oc_client, pg, pg_cluster_name)
    log.info("Recovery is completed successfully")


def check_backup_status(backup_id):
    try:
        url = f"http://postgres-backup-daemon:8080/backup/status/{backup_id}"
        headers = {"Accept": "application/json"}
        response = requests.get(url, headers=headers)
        response_text = response.text

        log.info("Backup Status Check Result: %s", response_text)

        if "Backup Done" in response_text:
            log.info("Backup Status: Backup done")
            return True
        else:
            log.info("Backup Status: Backup not done")
            raise Exception(f"Backup not done. Status: {response_text}")

    except requests.RequestException as e:
        log.error("Error occurred while checking backup status: %s", e)
        return False



def prepare_parameters_and_perform_recovery():
    log.info("Start parameters preparation")

    # import env variables
    oc_openshift_url = os.getenv("OC_OPENSHIFT_URL", None)
    oc_username = os.getenv("OC_USERNAME", None)
    oc_password = os.getenv("OC_PASSWORD", None)
    oc_project = os.getenv("OC_PROJECT", None)

    pg_cluster_name = os.getenv("PG_CLUSTER_NAME", None)
    pg_depl_name = os.getenv("PG_DEPL_NAME", None)
    preserve_old_files = os.getenv("PRESERVE_OLD_FILES", "no")

    restore_version = os.getenv("RESTORE_VERSION", "")

    recovery_target_timeline = os.getenv("RECOVERY_TARGET_TIMELINE", "latest")
    recovery_target_inclusive = os.getenv("RECOVERY_TARGET_INCLUSIVE", "true")

    recovery_target_name = os.getenv("RECOVERY_TARGET_NAME", None)
    recovery_target_time = os.getenv("RECOVERY_TARGET_TIME", None)
    recovery_target_xid = os.getenv("RECOVERY_TARGET_XID", None)
    recovery_target = os.getenv("RECOVERY_TARGET", None)

    oc_path = os.getenv("OC_PATH", "oc")
    skip_tls_verify = os.getenv("OC_SKIP_TLS_VERIFY", "true")
    oc_config_file = os.getenv("OC_CONFIG_FILE", "./oc_config_file.yaml")

    # parse args
    parser = argparse.ArgumentParser(description='Recovery procedure for Postgresql cluster')
    parser.add_argument('--oc-openshift-url', dest='oc_openshift_url', default=None, help='address of openshift console')
    parser.add_argument('--oc-username', dest='oc_username', default=None, help='user of openshift console')
    parser.add_argument('--oc-password', dest='oc_password', default=None, help='password of openshift console')
    parser.add_argument('--oc-project', dest='oc_project', default=None, help='address of openshift console')

    parser.add_argument('--pg-cluster-name', dest='pg_cluster_name', default=None, help='Postgresql cluster name')
    parser.add_argument('--pg-depl-name', dest='pg_depl_name', default=None,
                        help='Template of postgresql deployment name like "pg-common-node"')

    parser.add_argument('--preserve-old-files', dest='preserve_old_files', default=None, choices=["yes", "no"],
                        help='If "yes" then store old files on volume otherwise remove old files.')

    parser.add_argument('--restore-version', dest='restore_version', default=None,
                        help='ID of backup to recovery procedure')

    parser.add_argument('--timeline', dest='recovery_target_timeline', default=None,
                        help='Only for point in time recovery. Desired timeline for recovery procedure')
    parser.add_argument('--inclusive', dest='recovery_target_inclusive', default=None, choices=['true', 'false'],
                        help='Only for point in time recovery. Specifies if recovery procedure should inslude specified recovery target')

    parser.add_argument('--recovery-target-time', dest='recovery_target_time', default=None,
                        help='Only for point in time recovery. Specifies recovery_target_time.')
    parser.add_argument('--recovery-target-name', dest='recovery_target_name', default=None,
                        help='Only for point in time recovery. Specifies recovery_target_name.')
    parser.add_argument('--recovery-target-xid', dest='recovery_target_xid', default=None,
                        help='Only for point in time recovery. Specifies recovery_target_xid.')
    parser.add_argument('--recovery-target', dest='recovery_target', default=None, choices=['immediate'],
                        help='Only for point in time recovery. Specifies recovery_target.')

    parser.add_argument('--oc-path', dest='oc_path', default=None, help='path to oc client (with oc)')
    parser.add_argument('--oc-config', dest='oc_config_file', default=None, help='path to oc client (with oc)')
    parser.add_argument('--oc-skip-tls-verify', dest='skip_tls_verify', default=None, choices=['true', 'false'],
                        help='Set value to "true" to turn off ssl validation')

    args = parser.parse_args()

    for (key, value) in list(vars(args).items()):
        if value and key in locals():
            locals()[key] = value

    if not pg_depl_name:
        pg_depl_name = "pg-{}-node".format(pg_cluster_name)

    log.info("Parameters were parsed")
    log.debug("Local vars : {}".format(locals()))
    log.debug("Global vars : {}".format(globals()))

    try:
        perform_recovery(oc_openshift_url, oc_username, oc_password, oc_project,
                         pg_cluster_name, pg_depl_name,
                         preserve_old_files, restore_version,
                         recovery_target_timeline, recovery_target_inclusive,
                         recovery_target_name, recovery_target_time, recovery_target_xid, recovery_target,
                         oc_path, oc_config_file,
                         # force_manual_recovery,
                         skip_tls_verify=(True if skip_tls_verify == 'true' else False))
    except RecoveryException as ex:
        log.exception("Recovery procedure failed.")
        log.error(str(ex))
        sys.exit(1)


if __name__ == "__main__":
    prepare_parameters_and_perform_recovery()
