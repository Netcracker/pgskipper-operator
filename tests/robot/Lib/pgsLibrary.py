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

import os
import sys
import urllib3
import time
import logging
import requests
import json
import yaml
import psycopg2
import socket
import base64
from PlatformLibrary import PlatformLibrary
from robot.api.deco import keyword
from robot.libraries.BuiltIn import BuiltIn
from contextlib import closing

urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)
log = logging.getLogger()
log.setLevel(logging.DEBUG)

class pgsLibrary(object):
    def __init__(self, namespace, ssl_mode, internal_tls):
        self._namespace = namespace
        self._ssl_mode = ssl_mode
        self._internal_tls = internal_tls
        self._scheme = 'http'
        self._dbaas_scheme = 'http'
        if self._internal_tls == 'true':
            self._dbaas_scheme = 'https'
        if self._ssl_mode == 'require':
            self._scheme = 'https'
        self.pl_lib = PlatformLibrary(managed_by_operator="true")

    def setup_console_logging(self):
        log = logging.getLogger()
        log.setLevel(logging.INFO)

        ch = logging.StreamHandler(sys.stdout)
        ch.setLevel(logging.INFO)
        formatter = logging \
            .Formatter('%(asctime)s - %(thread)d - %(name)s:%(funcName)s#%(lineno)d - %(levelname)s - %(message)s')
        ch.setFormatter(formatter)
        for handler in log.handlers:
            log.removeHandler(handler)
        log.addHandler(ch)

    def setup_robot_logging(self):
        try:
            from robot.api import logger
        except ImportError:
            pass
        log = logging.getLogger()
        log.setLevel(logging.INFO)
        ch = logging.StreamHandler(sys.stdout)
        ch.setLevel(logging.INFO)
        formatter = logging \
            .Formatter('%(asctime)s - %(thread)d - %(name)s:%(funcName)s#%(lineno)d - %(levelname)s - %(message)s')
        ch.setFormatter(formatter)
        for handler in log.handlers:
            log.removeHandler(handler)

        class RobotRedirectHandler(logging.StreamHandler):
            def emit(self, record):
                try:
                    msg = self.format(record)
                    logger.info(msg)
                except (KeyboardInterrupt, SystemExit):
                    raise
                except Exception:
                    self.handleError(record)
        log.addHandler(RobotRedirectHandler())

    def setup_logging(self, log_to_robot=False):
        urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)
        if log_to_robot:
            self.setup_robot_logging()
        else:
            self.setup_console_logging()

    def get_master_pod(self):
        for pod in self.pl_lib.get_pods(self._namespace):
            if "pgtype" in pod.metadata.labels and pod.metadata.labels[
                'pgtype'] == 'master' and pod.status.phase == 'Running':
                return pod
        BuiltIn().run_keyword('Fail', "Master pod not found")
        return None

    def get_master_pod_id(self):
        pod = self.get_master_pod()
        if pod:
            return pod.metadata.name
        else:
            return None

    def get_replica_pod_id(self):
        replica_pods = self.get_replica_pods()
        names = []
        for pod in replica_pods:
            names.append(pod.metadata.name)
        return names

    def get_replica_pods(self):
        pods = self.pl_lib.get_pods(self._namespace)
        return list([x for x in pods if x.metadata.labels.get('pgtype') == 'replica' and x.status.phase == 'Running'])

    def wait_replica_pods_scale_up(self):
        pg_node_qty = os.getenv("PG_NODE_QTY", 1)
        replicas = self.get_replica_pods()
        if len(replicas) == int(pg_node_qty) - 1:
            return True
        return False

    def get_switchover_replicas(self):
        pods = self.get_replica_pods()
        # filter pods by DR_MODE env var
        pods = list(
            [x for x in pods if self.check_if_switchover_possible(x.spec.containers[0].env)]
        )
        ids = []
        for pod in pods:
            ids.append(pod.metadata.name)
        logging.info("Switchover pods: {}".format(ids))
        return ids

    def check_if_switchover_possible(self, envs):
        env_value = 'false'
        for env in envs:
            if env.name == "DR_MODE":
                env_value = env.value.lower()
        return env_value == 'false'

    @keyword('Execute In Pod')
    def execute_in_pod(self, pod_name, exec_command):
        container_name = None

        if 'pg-patroni' in pod_name:
            container_name = pod_name.rsplit('-', 1)[0]

            result, errors = self.pl_lib.execute_command_in_pod(
                name=pod_name,
                namespace=self._namespace,
                command=exec_command,
                container=container_name
            )
        else:
            result, errors = self.pl_lib.execute_command_in_pod(
                name=pod_name,
                namespace=self._namespace,
                command=exec_command
            )

        if result:
            return result, errors
        else:
            return None

    def get_master_service(self):
        master_service = "pg-" + os.getenv("PG_CLUSTER_NAME", "patroni")
        return master_service

    @keyword('Execute Auth Check')
    def execute_auth_check(self):
        cluster_name = os.getenv("PG_CLUSTER_NAME", "patroni")
        config_map_name = "patroni-{}.config.yaml".format(cluster_name)
        try:
            config_map = self.pl_lib.get_config_map(config_map_name, self._namespace)
        except Exception:
            config_map_name = "{}-patroni.config.yaml".format(cluster_name)
            config_map = self.pl_lib.get_config_map(config_map_name, self._namespace)
        config_map_yaml = (config_map.to_dict())
        config_map = config_map_yaml["data"]["patroni-config-template.yaml"]
        rest_api_auth = "authentication" in yaml.safe_load(config_map)["restapi"]
        rest_api_auth_configured = False
        status_code = 0
        if rest_api_auth:
            rest_api_auth_configured = True
            master_service = self.get_master_service()
            response = requests.patch(
                "http://{}:8008/config".format(master_service),
                data=json.dumps("{\"pause\": false}"))
            status_code = response.status_code

        if rest_api_auth_configured and status_code != 401:
            return False
        return True

    def get_pods(self, **kwargs):
        pods = self.pl_lib.get_pods(self._namespace)
        for key, value in list(kwargs.items()):
            if (key == 'repl_name'):
                pods = list([x for x in pods if x.metadata.name.startswith(value)])
                pods = list([x for x in pods if not x.metadata.name.endswith('deploy')])
            if (key == 'status'):
                pods = list([x for x in pods if x.status.phase == value])
            if (key == 'label'):
                # Support both ":" and "=" as separators
                if ":" in value:
                    (k, v) = value.split(":", 1)
                else:
                    (k, v) = value.split("=", 1)
                pods = list([x for x in pods if x.metadata.labels and k in x.metadata.labels and x.metadata.labels[k] == v])
        return pods

    def get_pod(self, **kwargs):
        ignore_not_found = 'false'
        pods = self.get_pods(**kwargs)
        for key, value in list(kwargs.items()):
            if (key == 'ignore_not_found'):
                ignore_not_found = value
        if len(pods) == 0:
            if ignore_not_found == 'false':
                BuiltIn().run_keyword('Fail', '[get_pod] Pod not found')
            return None
        return pods[0]

    def get_pods_names(self, **kwargs):
        pods = self.get_pods(**kwargs)
        names = []
        for i in pods:
            names.append(i.metadata.name)
        return names

    def get_pods_names_ip(self, **kwargs):
        pods = self.get_pods(**kwargs)
        DictNameIP = {}
        for i in pods:
            DictNameIP[i.metadata.name] = i.status.pod_ip
        return DictNameIP

    def get_replica_number(self, dc_name):
        dc = self.pl_lib.get_deployment_entity(dc_name, self._namespace)
        return dc['spec']['replicas']

    def get_replica_count(self, dc_name):
        reps = self.pl_lib.get_replica_number(dc_name, self._namespace)
        return reps

    @keyword('Execute Query')
    def execute_query(self, host, query, dbname='postgres'):
        password = os.getenv("PG_ROOT_PASSWORD")
        connection_properties = {
            'host': host,
            'password': password,
            'user': 'postgres',
            'dbname': dbname
        }
        with psycopg2.connect(**connection_properties) as conn:
            conn.set_isolation_level(0)
            with conn.cursor() as cursor:
                try:
                    cursor.execute(query)
                    if query.lower().find("insert"):
                        conn.commit()
                    return cursor.fetchall()
                except Exception as e:
                    logging.info("Error {0}.  execute {1}. Service is {2}".format(e, query, host))

    @keyword('Scale Deployment ${deployment} To ${replicas}')
    def os_scale_dc(self, deployment, replicas, timeout=90):
        self.pl_lib.set_replicas_for_deployment_entity(name=deployment, namespace=self._namespace, replicas=int(replicas))
        time.sleep(10)
        for i in range(1, timeout):
            resp = self.pl_lib.get_deployment_entity(deployment, self._namespace)
            time.sleep(1)
        if resp.status.replicas:
            return resp.status.replicas
        else:
            return False

    def delete_pod(self, pod_name, grace_period=0):
        self.pl_lib.delete_pod_by_pod_name(pod_name, self._namespace, grace_period=grace_period)

    def delete_pods(self, pods):
        for pod in pods:
            self.delete_pod(pod.metadata.name)

    @keyword('Get API Version')
    def get_api_version(self):
        curl_command = f'curl -k -XGET -u dbaas-aggregator:dbaas-aggregator {self._dbaas_scheme}://dbaas-postgres-adapter.{self._namespace}:8080/api/version'
        master_pod = self.get_master_pod_id()
        result, errors = self.execute_in_pod(pod_name=master_pod, exec_command=curl_command)
        if result:
            return result
        else:
            return "API version not found in the curl output."

    def get_deployment_from_pod(self, pod):
        replica_set_name = pod.metadata.owner_references[0].name
        replica_set = self.pl_lib.get_replica_set(replica_set_name, self._namespace)
        deployment_name = replica_set.metadata.owner_references[0].name
        for deployment in self.pl_lib.get_deployment_entities(self._namespace):
            if deployment.metadata.name == deployment_name:
                return deployment

    # def get_dcs(self, **kwargs):
    #     dcs = self.pl_lib.get_dcs()  #???
    #     for key, value in list(kwargs.items()):
    #         if (key == 'name'):
    #             dcs = list([x for x in dcs if x.metadata.name.startswith(value)])
    #         if (key == 'has_unavailable_replicas'):
    #             if value == 'true':
    #                 dcs = list([x for x in dcs if x.status.unavailable_replicas > 0])
    #     return dcs

    def get_number_of_replicas_from_stateful_set(self, stateful_set_name):
        response = self.pl_lib.get_stateful_set(stateful_set_name, self._namespace)
        return response.spec.replicas

    def scale_down_stateful_set(self, stateful_set_name):
        self.pl_lib.scale_down_stateful_set(stateful_set_name, self._namespace)

    def check_stateful_set_is_scaled(self, stateful_set_names: list, direction: str, timeout: int = 300):
        return self.pl_lib.check_service_of_stateful_sets_is_scaled(stateful_set_names, self._namespace, direction=direction, timeout=timeout)

    def scale_up_stateful_set(self, stateful_set_name):
        number = self.get_number_of_replicas_from_stateful_set(stateful_set_name) + 1
        self.pl_lib.set_replicas_for_stateful_set(stateful_set_name, self._namespace, number)

    def check_affinity_rules(self, pod):
        pod_dict = pod.spec.to_dict()
        if "affinity" in pod_dict:
            return (True)
        else:
            return (False)

    def http_request(self, url):
        resp = dict.fromkeys(['response', 'json'])
        try:
            response = requests.get(url)
            resp.update(response=response)
            json = response.json()
            resp.update(json=json)
        except Exception as e:
            logging.info("Error {0}.  url: {1}".format(e, url))
        return resp

    def make_switchover_via_patroni_rest(self):
        logging.info("Manual switchover via Patroni REST is called")
        master = self.get_master_pod_id()
        master_service = self.get_master_service()
        if not (master_service):
            master_service = "pg-" + os.getenv("PG_CLUSTER_NAME", "patroni")
        replica = self.get_switchover_replicas()[0]
        logging.info("Master is {0} and replica is {1}. Service is {2}".format(master, replica, master_service))
        data = {
            "leader": master,
            "candidate": replica
        }
        user = os.getenv('PATRONI_REST_API_USER')
        password = os.getenv('PATRONI_REST_API_PASSWORD')
        from requests.auth import HTTPBasicAuth
        basic_auth = HTTPBasicAuth(user, password)
        req = requests.post("http://{0}:8008/switchover".format(master_service),
                            data=json.dumps(data),
                            auth=basic_auth)
        logging.info("Patroni Response: {}".format(req.text))
        assert req.ok
        time.sleep(10)
        new_master = self.get_master_pod_id()
        logging.info("New Master is {0}".format(new_master))
        assert new_master == replica

    def check_if_next_run_scheduled(self):
        self.get_pod(label='app:postgres-backup-daemon', status='Running')
        schedule = requests.get(f"{self._scheme}://postgres-backup-daemon:8085/schedule", verify=False)
        schedule_json = schedule['stdout']
        if "time_until_next_backup" in schedule_json:
            logging.info("Found record about scheduling in schedule status")
        else:
            raise AssertionError("Cannot find record \"time_until_next_backup\" in logs")

        health = self.http_request(f"{self._scheme}://postgres-backup-daemon:8080/health", verify=False)
        health_json = health['json']
        if "backup" in health_json and "time_until_next_backup" in health_json["backup"]:
            logging.info("time_until_next_backup: {}".format(health_json["backup"]["time_until_next_backup"]))
        else:
            raise AssertionError("Cannot find time_until_next_backup in health status")

    def yaml_to_dict(self, string_yaml):
        dict = json.loads(string_yaml)
        return dict

    def get_ip(self, IP):
        try:
            socket.inet_pton(socket.AF_INET6, IP)
            return "[{}]".format(IP)
        except socket.error:
            return IP

######################
# For backups daemon #
######################
    def get_pg_version(self):
        host = "pg-" + os.getenv("PG_CLUSTER_NAME", "patroni")
        version_unprep = (self.execute_query(host, 'SHOW SERVER_VERSION;'))[0][0]
        version = version_unprep.split(" ")[0]
        pg_ver = float(version)
        return pg_ver

    def connection_for_pg(self):
        conn = psycopg2.connect(dbname='postgres',
                                user=os.getenv('POSTGRES_USER', "postgres"),
                                password=os.getenv('PG_ROOT_PASSWORD'),
                                host="pg-" + os.getenv("PG_CLUSTER_NAME", "patroni"))
        return conn

    def create_test_db(self, *base_names):
        logging.info(base_names)
        conn = self.connection_for_pg()
        with closing(conn):
            conn.set_isolation_level(0)
            with conn.cursor() as cursor:
                cursor.execute('SELECT datname FROM pg_database;')
                for base_name in base_names:
                    row = [x[0] for x in cursor.fetchall()]
                    if base_name not in row:
                        logging.info("Base {} not existing yet, creating...".format(base_name))
                        cursor.execute('CREATE DATABASE {}'.format(base_name))

    def create_test_db_with_role(self, role_name, *base_names):
        logging.info(base_names)
        conn = self.connection_for_pg()
        with closing(conn):
            conn.set_isolation_level(0)
            with conn.cursor() as cursor:
                cursor.execute('SELECT datname FROM pg_database;')
                for base_name in base_names:
                    row = [x[0] for x in cursor.fetchall()]
                    if base_name not in row:
                        logging.info("Base {} not existing yet, creating...".format(base_name))
                        cursor.execute('CREATE DATABASE {} WITH OWNER {}'.format(base_name, role_name))

    def create_role(self, *role_names):
        logging.info(role_names)
        conn = self.connection_for_pg()
        with closing(conn):
            conn.set_isolation_level(0)
            with conn.cursor() as cursor:
                cursor.execute('SELECT rolname FROM pg_roles;')
                for role_name in role_names:
                    row = [x[0] for x in cursor.fetchall()]
                    if role_name not in row:
                        logging.info("Role {} not existing yet, creating...".format(role_name))
                        cursor.execute('CREATE ROLE {}'.format(role_name))

    def delete_test_db(self, *base_names):
        conn = self.connection_for_pg()
        with closing(conn):
            conn.set_isolation_level(0)
            with conn.cursor() as cursor:
                for base_name in base_names:
                    cursor.execute("SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE pid <> pg_backend_pid() AND datname = '{}';".format(base_name))
                    cursor.execute('DROP DATABASE IF EXISTS {}'.format(base_name))

    @keyword('Get Pod Daemon')
    def get_pod_daemon(self):
        for pod in self.pl_lib.get_pods(self._namespace):
            if "app" in pod.metadata.labels \
                    and pod.metadata.labels['app'] == 'postgres-backup-daemon' \
                    and pod.status.phase == 'Running':
                return pod
        BuiltIn().run_keyword('get_backup_count', "Postgres Backup Daemon pod not found") #??
        return None

    def get_deployment(self, label_app):
        for deployment in self.pl_lib.get_deployment_entities(self._namespace):
            if "app" in deployment.metadata.labels \
                    and deployment.metadata.labels['app'] == label_app:
                # and deployment.status.replicas == '1':
                return deployment
        BuiltIn().run_keyword('Fail', "Postgres Backup Daemon deployment not found")
        return None

    def backup_daemon_alive(self):
        req = requests.get(f"{self._scheme}://postgres-backup-daemon:8080/health", verify=False)
        logging.info("req: {}".format(req.text))
        status = req.json().get("status")
        assert status in ["UP", "WARNING"]

    @keyword('Get Auth')
    def get_auth(self):
        pod = self.get_pod_daemon()
        for env in pod.spec.containers[0].env:
            if env.name == "AUTH":
                logging.info("auth value return: {}".format(env.value.lower()))
                return env.value.lower()
        return "false"

    def wal_archiving_is_enabled(self):
        archive_mode = self.execute_query("pg-" + os.getenv("PG_CLUSTER_NAME", "patroni"), "SHOW ARCHIVE_MODE;")
        logging.info("archive_mode: {}".format(archive_mode))
        if archive_mode == 'on':
            logging.info("archive_mode is set to ON, proceeding with test")
        else:
            logging.info("archive_mode is set to OFF, aborting this test")
            BuiltIn().pass_execution('Passing execution', "archive_mode is set to OFF, can not proceed with test")

    def switch_wal_archive(self):
        pg_ver = self.get_pg_version()
        if pg_ver >= 10:
            switch_wal_query = "select pg_switch_wal();"
        else:
            switch_wal_query = "select pg_switch_xlog();"
        switch_wal_res = self.execute_query("pg-" + os.getenv("PG_CLUSTER_NAME", "patroni"), switch_wal_query)
        logging.info("Switch WALs result: {}".format(switch_wal_res))

    def number_of_wals_increased(self, old_number):
        new_number = self.get_number_of_stored_wal_archives()
        for _ in range(60):
            logging.info("Checking new number of WAL files")
            new_number = self.get_number_of_stored_wal_archives()
            logging.info("old_number of WAL files: {0} new_number of WAL files: {1}".format(old_number, new_number))
            if new_number > old_number:
                logging.info("Number of WAL files increased, exiting")
                break
            else:
                logging.info("Number of WAL files not increased, waiting for 1 sec")
                import time
                time.sleep(1)
        assert new_number > old_number

    def get_number_of_stored_wal_archives(self):
        backup_daemon_pod = self.get_pod_daemon()
        (pg_ver, backup_dir) = self.detect_pg_version_and_storage_path()
        back_up_path = backup_dir + '/archive'
        command = "ls -al {0} | wc -l".format(back_up_path)
        logging.info("will execute next {} in pod {}".format(command, backup_daemon_pod.metadata.name))
        res = self.pl_lib.execute_command_in_pod(backup_daemon_pod.metadata.name, self._namespace, command)
        output, status = res["stdout"], res["status"]
        if status == "Success":
            logging.info("Number of files: {0} in {1}".format(output, back_up_path))
            return int(output.rstrip())
        else:
            BuiltIn().run_keyword('Fail', "Fail to execute command in pod: {}".format(res))

    def get_env_for_deployment(self, label_app, env_name, default_value=None):
        depl = self.get_deployment(label_app).to_dict()
        envs = depl["spec"]["template"]["spec"]["containers"][0]["env"]
        env = list([x for x in envs if x["name"] == env_name])
        return default_value if not env else env[0]["value"]

    def get_env_for_pod(self, pod, env_name, default_value=None):
        pod_dict = pod.to_dict()
        envs = pod_dict["spec"]["containers"][0]["env"]
        env = list([x for x in envs if x["name"] == env_name])
        return default_value if not env else env[0]["value"]

    def schedule_backup(self):
        # """
        # Schedules backup.
        # If time passed since last successful backup less than 1 min - wait.
        #
        # """
        pod = self.get_pod(label='app:postgres-backup-daemon', status='Running')
        logging.info("Start backup through REST from pod {}".format(pod.metadata.name))
        exec_command = 'expr $(expr $(date +%s) \\* 1000)'
        try:
            expr_date, error = self.pl_lib.execute_command_in_pod(pod.metadata.name, self._namespace, exec_command)
            health_json = requests.get(f"{self._scheme}://postgres-backup-daemon:8080/health", verify=False).json()
            new_dump_count = int(health_json["storage"]["lastSuccessful"]["ts"])
            delta = int(expr_date) - new_dump_count
        except Exception:
            logging.exception("Cannot parse delta")
            delta = 60000
        if delta < 60000:
            sleep_time = 65 - delta / 1000
            logging.info("Will sleep {}s before next schedule".format(sleep_time))
            time.sleep(sleep_time)
        logging.info("Try to schedule backup")
        health_json = requests.post(f"{self._scheme}://postgres-backup-daemon:8080/backup", verify=False).json()
        return health_json

    def wait_for_backup_to_complete(self, replica, dump_count):
        logging.info("Wait while backup will be complete and check health status through REST for pod {}".format(replica))
        for i in range(1, 240):
            health_json = requests.get(f"{self._scheme}://postgres-backup-daemon:8080/health", verify=False).json()
            if "storage" in health_json and "dump_count" in health_json["storage"]:
                new_dump_count = health_json["storage"]["dump_count"]
            else:
                new_dump_count = 0
            backup_in_progress = health_json["backup_is_in_progress"]
            if not backup_in_progress:
                logging.info("No backup is in progress anymore. Break.")
                break
            logging.info("Wait for backup to complete. Current dump_count: {}. Previous value: {}".format(new_dump_count, dump_count))
            time.sleep(1)
        if new_dump_count > dump_count:
            logging.info("SUCCESS: dump_count increased. "
                         "Current dump_count: {}. Previous value: {}"
                         .format(new_dump_count, dump_count))
        else:
            logging.error("FAILURE: Dump count still the same. "
                          "If You are running test on old installation and backup "
                          "procedure takes more than 2 min - check is backup complete "
                          "manually and ignore this test")
            raise Exception()
        new_health = health_json["status"]
        if new_health == "UP":
            logging.info("SUCCESS: Status is UP.")
        else:
            logging.error("FAILURE: Wrong health status {} from {}".format(new_health, health_json))
            raise Exception()

    def wait_for_evict_to_complete(self, replica, backup_id):
        logging.info("Wait while eviction of {} will be complete and check health status through REST on pod {}".format(backup_id, replica))
        for i in range(1, 120):
            health_json = requests.get(f"{self._scheme}://postgres-backup-daemon:8080/health", verify=False).json()
            last_backup_id = health_json["storage"]["lastSuccessful"]["id"]
            if last_backup_id != backup_id:
                break
            logging.info("Wait for evict to complete. Current backup_id: {}".format(last_backup_id))
            time.sleep(1)
        if last_backup_id != backup_id:
            logging.info("SUCCESS: lastSuccessful dump id changed. Current dump_count: {}. Previous value: {}".format(last_backup_id, backup_id))
        else:
            logging.error("FAILURE: lastSuccessful dump remained {}".format(last_backup_id))
            raise AssertionError()
        health_json = requests.get(f"{self._scheme}://postgres-backup-daemon:8080/health", verify=False).json()
        if health_json["status"] == "UP":
            logging.info("SUCCESS: Status is UP.")
        else:
            logging.error("FAILURE: Wrong health status {} from {}".format(health_json["status"], json.dumps(health_json)))
            raise AssertionError()

    def get_backup_count(self):
        health_json = requests.get(f"{self._scheme}://postgres-backup-daemon:8080/health", verify=False).json()
        if "storage" in health_json and "dump_count" in health_json["storage"]:
            dump_count = health_json["storage"]["dump_count"]
        else:
            dump_count = 0
        return dump_count

    def detect_pg_version_and_storage_path(self):
        pg_ver = self.get_pg_version()
        if pg_ver >= 17:
            logging.info("Using pgsql 17 storage")
            backup_dir = '/backup-storage/pg17'
        elif pg_ver >= 16:
            logging.info("Using pgsql 16 storage")
            backup_dir = '/backup-storage/pg16'
        elif pg_ver >= 15:
            logging.info("Using pgsql 15 storage")
            backup_dir = '/backup-storage/pg15'
        elif pg_ver >= 14:
            logging.info("Using pgsql 14 storage")
            backup_dir = '/backup-storage/pg14'
        elif pg_ver >= 13:
            logging.info("Using pgsql 13 storage")
            backup_dir = '/backup-storage/pg13'
        elif pg_ver >= 12:
            logging.info("Using pgsql 12 storage")
            backup_dir = '/backup-storage/pg12'
        elif pg_ver >= 11:
            logging.info("Using pgsql 11 storage")
            backup_dir = '/backup-storage/pg11'
        elif pg_ver >= 10:
            logging.info("Using pgsql 10 storage")
            backup_dir = '/backup-storage/pg10'
        else:
            logging.info("Using pgsql 9.6 storage")
            backup_dir = '/backup-storage'
        return float(pg_ver), backup_dir

    def is_encryption_on(self, pod_name):
        if "KEY_SOURCE=" in (self.pl_lib.execute_command_in_pod(pod_name, self._namespace, "env")):
            return True
        return False

    def check_if_backup_files_state(self, pod, backup_id, present=True):
        logging.info("Check if backup {} present from pod {}".format(backup_id, pod.metadata.name))
        storage_type = self.get_env_for_pod(pod, "STORAGE_TYPE")
        cluster_name = self.get_env_for_pod(pod, "PG_CLUSTER_NAME")
        encryption = self.is_encryption_on(pod.metadata.name)
        logging.info("Encryption: {}".format(encryption))
        metrics_name = "{}/.metrics".format(backup_id)
        console_name = "{}/.console".format(backup_id)
        if encryption:
            archive_name = "pg_backup_{}_enc.tar.gz".format(backup_id)
        else:
            archive_name = "pg_{}_backup_{}.tar.gz".format(cluster_name, backup_id)

        if storage_type == "swift" or storage_type == "aws_s3":
            logging.info("Detected storage type {}".format(storage_type))
            if storage_type == "swift":
                cmd = "sh -c 'ST_AUTH=${SWIFT_AUTH_URL} ST_USER=${SWIFT_USER} ST_KEY=${SWIFT_PASSWORD} ST_TENANT=${TENANT_NAME} /opt/backup/scli ls ${CONTAINER}'"
            else:
                cmd = "sh -c 'aws --endpoint-url ${AWS_S3_ENDPOINT_URL} s3 ls s3://${CONTAINER} --recursive'"
            files_list, error = self.pl_lib.execute_command_in_pod(pod.metadata.name, self._namespace, cmd)
            metrics_name = "{}.metrics".format(backup_id)
            console_name = "{}.console".format(backup_id)
        elif storage_type == "pv" or storage_type == "pv_label" or storage_type == "provisioned" or storage_type == "provisioned-default":
            (pg_ver, backup_dir) = self.detect_pg_version_and_storage_path()
            if (pg_ver >= 10) and not encryption:
                archive_name = "pg_backup_{}.tar.gz".format(backup_id)
            command = "find {} | grep {} || [[ $? == 1 ]]".format(backup_dir, backup_id)
            files_list, error = self.pl_lib.execute_command_in_pod(pod.metadata.name, self._namespace, command)
        else:
            logging.error("FAILURE: Wrong storage type: {}".format(storage_type))
            raise AssertionError()
        if present:
            if console_name in files_list:
                logging.info("SUCCESS: Found console file.")
            else:
                logging.error("FAILURE: Cannot found console file for backup {}. Files: {}".format(backup_id, files_list))
                raise AssertionError()

            if metrics_name in files_list:
                logging.info("SUCCESS: Found metrics file.")
            else:
                logging.error("FAILURE: Cannot found metrics file for backup {}. Files: {}".format(backup_id, files_list))
                raise AssertionError()

            if archive_name in files_list:
                logging.info("SUCCESS: Found backup file.")
            else:
                logging.error("FAILURE: Cannot found backup file {} for backup {}. Files: {}".format(archive_name, backup_id, files_list))
                raise AssertionError()
        else:
            if console_name not in files_list:
                logging.info("SUCCESS: Does not found console file.")
            else:
                logging.error("FAILURE: Found console file for backup {}. Files: {}".format(backup_id, files_list))
                raise AssertionError()

            if metrics_name not in files_list:
                logging.info("SUCCESS: Does not found metrics file.")
            else:
                logging.error("FAILURE: Found metrics file for backup {}. Files: {}".format(backup_id, files_list))
                raise AssertionError()

            if archive_name not in files_list:
                logging.info("SUCCESS: Does not found backup file.")
            else:
                logging.error("FAILURE: Found backup file {} for backup {}. Files: {}".format(archive_name, backup_id, files_list))
                raise AssertionError()

    def check_if_backup_files_present(self, pod, backup_id):
        self.check_if_backup_files_state(pod, backup_id, present=True)

    def check_if_backup_files_absent(self, pod, backup_id):
        self.check_if_backup_files_state(pod, backup_id, present=False)

#Backup libs
    def check_backup_count(self, dump_count):
        for i in range(1, 4):
            backup_list_count = len(requests.get(f"{self._scheme}://postgres-backup-daemon:8081/list", verify=False).json())
        if backup_list_count != dump_count:
            dump_count = self.get_backup_count()
        if backup_list_count == dump_count:
            log.info("SUCCESS: List count equals health count.")
        else:
            log.error("Backup count from health status ({}) and from /list is different ({})".format(dump_count, backup_list_count))
            raise AssertionError()

    def check_last_backup_size(self):
        health_json = requests.get(f"{self._scheme}://postgres-backup-daemon:8080/health", verify=False).json()
        last_backup_size = int(health_json["storage"]["lastSuccessful"]["metrics"]["size"])
        return last_backup_size

    def check_last_backup_id(self):
        health_json = requests.get(f"{self._scheme}://postgres-backup-daemon:8080/health", verify=False).json()
        last_backup_id = health_json["storage"]["lastSuccessful"]["id"]
        return last_backup_id

    def schedule_evict(self, last_backup_id):
        health_json = requests.delete(f"{self._scheme}://postgres-backup-daemon:8081/evict?id={last_backup_id}", verify=False).json()
        return health_json

    def check_storage_type(self, pod, last_backup_id, backup_dir):
        storage_type = self.get_env_for_pod(pod, "STORAGE_TYPE")
        storage_type = storage_type.split('\n')[0].split(" ")[0]
        if storage_type == "swift" or storage_type == "aws_s3":
            log.info("Detected storage type {}".format(storage_type))
            raise AssertionError("Not implemented yet for storage type: {}".format(storage_type))
        elif storage_type == "pv" or storage_type == "pv_label" or storage_type == "provisioned" or storage_type == "provisioned-default":
            self.pl_lib.execute_command_in_pod(pod.metadata.name, self._namespace, "rm -rf {}/{}/.metrics".format(backup_dir, last_backup_id))
            log.info((self.pl_lib.execute_command_in_pod(pod.metadata.name, self._namespace, "ls -l {}/{}".format(backup_dir, last_backup_id))))
        else:
            log.error("FAILURE: Wrong storage type: {}".format(storage_type))
            raise AssertionError()

    def delete_corupted_backup(self, pod, backup_dir, corrupted_backup_id):
        self.pl_lib.execute_command_in_pod(pod, self._namespace, "rm -rf {}/{}".format(backup_dir, corrupted_backup_id))

    def get_backup_files(self, pod, last_backup_id, type='/granular', namespace='/default'):
        pg_ver, backup_dir = self.detect_pg_version_and_storage_path()
        result = self.pl_lib.execute_command_in_pod(pod.metadata.name, self._namespace, "ls -l {}{}{}/{}".format(backup_dir, type, namespace, last_backup_id))
        return result

    def get_dbaas_adapter_creds(self):
        secret = self.pl_lib.get_secret('dbaas-adapter-credentials', self._namespace)
        return base64.b64decode(secret.data.get('username')), base64.b64decode(secret.data.get("password"))

    def get_dd_images_from_config_map(self, config_map_name):
        config_map = self.pl_lib.get_config_map(config_map_name, self._namespace)
        config_map_yaml = (config_map.to_dict())
        cm = config_map_yaml["data"]["dd_images"]
        if cm:
            return cm
        else:
            return None

    def get_image_from_resource(self, type, name, container_name):
        return self.pl_lib.get_resource_image(type, name, self._namespace, container_name)
