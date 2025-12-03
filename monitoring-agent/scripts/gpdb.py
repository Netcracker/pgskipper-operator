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

import logging
from multiprocessing.pool import ThreadPool

import psycopg2
import time

from smart_getenv import getenv
from collector_utils import perform_smoketests, collect_pg_metrics, collect_replication_data
from metrics_query_utils import get_common_pg_metrics_by_version
from kubernetes import client
from kubernetes.stream import stream
from kubernetes.client.rest import ApiException
from math import ceil
from typing import List

from metric import Metric, default_tags

logger = logging.getLogger(__name__)


class State:
    DOWN = 14
    OK = 0
    WARNING = 3
    ERROR = 10
    DEGRADED = 6


class Queries:
    # queries for tracking cluster state
    HOSTS_STATUS = "select hostname, status, content, role, preferred_role, mode FROM gp_segment_configuration"
    # distributed query test. One row should be returned for each primary segment.
    DISTRIBUTED_QUERY_TEST = "SELECT COUNT(*) FROM (SELECT GP_SEGMENT_ID, COUNT(*) " \
                             "FROM GP_DIST_RANDOM('pg_class') GROUP BY 1) AS T;"
    # If this query fails the active master may be down.
    STANDBY_MASTER_REPLICATION = "SELECT state FROM pg_stat_replication;"


class OpenshiftHelper:

    SA_NAMESPACE_PATH = '/var/run/secrets/kubernetes.io/serviceaccount/namespace'

    def __init__(self):
        self._os_workspace = open(self.SA_NAMESPACE_PATH).read()
        self._core_api = client.CoreV1Api()

    def get_pods_by_label(self, label):
        pods = self._core_api.list_namespaced_pod(namespace=self._os_workspace, label_selector=label)
        return [pod.metadata.name for pod in pods.items]

    def execute_in_pod(self, pod, command):
        try:
            resp = stream(self._core_api.connect_get_namespaced_pod_exec, pod, self._os_workspace, command=command,
                          stderr=False, stdin=False, stdout=True, tty=False)
            return resp.strip()
        except ApiException as e:
            logger.error("Exception when calling CoreV1Api -> execute_in_pod: ", e)
        return None


class Member:
    def __init__(self, hostname, status, content, role, preferred_role, mode):
        self.hostname = hostname
        self.status = status
        self.content = content
        self.role = role
        self.preferred_role = preferred_role
        self.mode = mode

    def is_running_and_master(self):
        return self.content == -1 and self.status == 'u'

    def is_down_and_master(self):
        return self.content == -1 and self.status != 'u'

    def is_running_and_segment(self):
        return self.content != -1 and self.status == 'u'

    def is_down_and_segment(self):
        return self.content != -1 and self.status != 'u'

    def is_primary_master(self):
        return self.content == -1 and self.status == 'u' and self.role == 'p'

    def is_standby_master(self):
        return self.content == -1 and self.status == 'u' and self.role == 'm'

    def is_primary_segment(self):
        return self.content != -1 and self.status == 'u' and self.role == 'p'

    @property
    def is_up(self):
        return 1 if self.status == 'u' else 0

    @property
    def is_optimal_role(self):
        return self.preferred_role == self.role

    @property
    def is_resync(self):
        return self.mode == 'r' and self.content != -1

    @property
    def is_tracking(self):
        return self.mode == 'c' and self.content != -1


class Cluster:
    def __init__(self, members: List[Member]):
        self.members = members

    @property
    def down_segments(self):
        return list(filter(lambda x: x.is_down_and_segment(), self.members))

    @property
    def down_masters(self):
        return list(filter(lambda x: x.is_down_and_master(), self.members))

    @property
    def cluster_status(self):
        cluster_state = State.OK
        if self.down_segments:
            cluster_state = State.WARNING
        if self.down_masters:
            cluster_state = State.ERROR
        if not self.standby_leader:
            cluster_state = State.DEGRADED
        return cluster_state

    @property
    def primary_segments(self):
        return list(filter(lambda x: x.is_primary_segment(), self.members))

    @property
    def standby_leader(self):
        return list(filter(lambda x: x.is_standby_master(), self.members))

    @property
    def __str__(self):
        return [member.__str__() for member in self.members]


class GpdbCollector:
    def __init__(self, cluster_name):
        self._cluster_name = cluster_name
        self._conn_properties = dict(
            host=f'pg-{cluster_name}', port=5432, password=getenv('MONITORING_PASSWORD', type=str, default=""),
            user=getenv('MONITORING_USER', type=str, default=""), dbname="postgres",
            connect_timeout=10
        )
        self._start_time = time.time()
        self._collection_state = State.OK
        self._oshift_helper = OpenshiftHelper()
        self._metrics = {}
        self._metrics_list = []

    @property
    def conn_link(self):
        return ' '.join("%s=%r" % (key, val) for (key, val) in self._conn_properties.items())

    def get_db_connection(self):
        try:
            connection = psycopg2.connect(**self._conn_properties)
            connection.autocommit = True
        except Exception as e:
            logger.error("Can not connect to PostgreSQL", e)
            return None
        return connection

    def _collect_cluster_state(self):
        logger.info("Collecting cluster state")
        cluster_metrics = {}

        conn = self.get_db_connection()
        if not conn:
            # DB is down, marking state as DOWN
            cluster_state = State.DOWN
        else:
            # DB is accessible, check cluster state
            members = list()
            running_pods = self._oshift_helper.get_pods_by_label("service=gpdb")
            with conn.cursor() as cur:
                cur.execute(Queries.HOSTS_STATUS)
                for hostname, status, content, role, preferred_role, mode in cur:
                    hostname = hostname.split(".")[0]
                    member = Member(hostname, status, content, role, preferred_role, mode)
                    members.append(member)
                cur.execute(Queries.STANDBY_MASTER_REPLICATION)
                if cur.fetchone():
                    cluster_metrics.update({"master_replication_state": State.OK})
                else:
                    cluster_metrics.update({"master_replication_state": State.DOWN})
                try:
                    cur.execute(Queries.DISTRIBUTED_QUERY_TEST)
                    dist_query_test = int(cur.fetchone()[0])
                except psycopg2.OperationalError as e:
                    logger.error("DISTRIBUTED_QUERY_TEST is failed", e)
                    dist_query_test = 0

            cluster = Cluster(members)
            members_metrics = {x.hostname: x.is_up and x.hostname in running_pods for x in members}
            cluster_metrics.update({"members": members_metrics})

            optimal_roles = {x.hostname: x.is_optimal_role for x in members}
            cluster_metrics.update({"optimal_roles": optimal_roles})

            syncing = {x.hostname: x.is_resync for x in members}
            cluster_metrics.update({"syncing": syncing})

            tracking = {x.hostname: x.is_tracking for x in members}
            cluster_metrics.update({"tracking": tracking})

            primary_segments = len(cluster.primary_segments)
            cluster_metrics.update({"dist_query_state": State.ERROR if dist_query_test != primary_segments else State.OK})

            cluster_state = cluster.cluster_status
        for role in ["leader", "standby"]:
            role_pod = self._oshift_helper.get_pods_by_label(f"role={role}")
            role_ = role_pod[0] if role_pod else "None"
            role_idx = role_pod[0].split("-")[2] if role_pod else 0
            cluster_metrics.update({role: role_})
            cluster_metrics.update({role + "_idx": role_idx})
            cluster_metrics.update({role + "_count": 1})

        cluster_metrics.update({"status": cluster_state})
        self._metrics.update({"cluster": cluster_metrics})

    def _collect_common_pg_metrics(self):
        logger.info("Collecting common PostgreSQL metrics")
        # lets hardcode version of pgsql in case of gpdb
        common_pg_metrics = get_common_pg_metrics_by_version([9, 4])
        pg_metrics = collect_pg_metrics(self.conn_link, common_pg_metrics)
        pg_metrics["replication_data"] = collect_replication_data(self.conn_link)
        self._metrics.update({"pg_metrics": pg_metrics})

    def collect(self):
        try:
            self._collect_cluster_state()
            self._perform_smoke_tests()
            self._collect_common_pg_metrics()
            self._collect_storage_metrics()
        except Exception as e:
            self._collection_state = State.ERROR
            logger.error("Collection of GPDB metrics are failed", e)
        logger.info("Collection of GPDB metrics suceeded")

    def convert(self):
        return {"gpdb": self._metrics, "status": self._collection_state,
                "collector": {"duration": self._start_time - time.time()}}, self._metrics_list

    def _perform_smoke_tests(self):
        logger.info("Performing smoketests")
        smoke_test = perform_smoketests(self.conn_link)
        passed = 0
        for metric in smoke_test:
            if metric.metric_name == "endpoints.cluster.smoketest.passed":
                passed = metric.fields.split("=")[1]
        smoke_test.append(Metric(metric_name="endpoints.cluster.running", tags=default_tags, fields={"value": passed}))
        self._metrics_list.extend(smoke_test)

    @staticmethod
    def collect_storage_metric(pod):
        helper = OpenshiftHelper()
        # output in bytes
        du_command = ['/bin/bash', '-c', "du /data/ --max-depth=1 --exclude=/data/lost+found | tail -n 1 | cut -f1"]
        du_res = ceil(int(helper.execute_in_pod(pod=pod, command=du_command)) / 1024)
        df_command = ['/bin/bash', '-c', "df --output=pcent /data/ | grep -v 'Use' | cut -d '%' -f1"]
        df_res = helper.execute_in_pod(pod=pod, command=df_command)
        storage_metrics = {
            "du": du_res,
            "df": df_res
        }
        return {pod: storage_metrics}

    def _collect_storage_metrics(self):
        logger.info("Collecting storage metrics")
        gpdb_pods = self._oshift_helper.get_pods_by_label(label="service=gpdb")
        # collect
        pool = ThreadPool(len(gpdb_pods))
        results = pool.map(self.collect_storage_metric, gpdb_pods)  # Run exec calls on pods in parallel
        pool.close()
        pool.join()
        # TODO fix this
        final_res = {}
        for x in results: final_res.update(x)
        self._metrics.update({"storage": final_res})
