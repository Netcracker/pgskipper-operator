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

from google.cloud import monitoring_v3
from googleapiclient import discovery
import time
import logging
import os

SA_PATH = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"


class CloudSqlMetricApiCaller:
    def __init__(self):
        self.client = monitoring_v3.MetricServiceClient()
        self.log = logging.getLogger('MetricRequestEndpoint')
        self.project = os.getenv("EXT_DB_PROJECT_NAME")
        self.instance = self.get_cloud_sql_instance()
        self.metric_dict = {
            "df.pcent": "database/disk/utilization",
            "du.base": "database/disk/bytes_used",
            "cpu.pcent": "database/cpu/utilization",
            "cpu.reserved_cores": "database/cpu/reserved_cores",
            "memory.usage": "database/memory/usage",
            "memory.quota": "database/memory/quota",
            "current_connections": "database/postgresql/num_backends",
            "running": "database/up"
        }

    def get_cloud_sql_instance(self):
        import googleapiclient
        instance_from_env = os.getenv("EXT_DB_INSTANCE_NAME")
        service = discovery.build('sqladmin', 'v1beta4')
        request = service.instances().get(project=self.project, instance=instance_from_env)
        instance_exists = True
        try:
            request.execute()
        except googleapiclient.errors.HttpError as err:
            if err.resp.status == 404:
                self.log.warning(f"Requested resource {instance_from_env} not found\n"
                                 f"Will try to find instance with namespace label")
                instance_exists = False
        if instance_exists:
            return instance_from_env
        else:
            request = service.instances().list(project=self.project)
            response = request.execute()
            cur_region = self.get_current_region()
            for database_instance in response.get('items', []):
                region = database_instance["region"]
                labels = database_instance["settings"]["userLabels"]
                if labels.get("namespace", "") == os.environ['NAMESPACE'] \
                        and region == cur_region:
                    instance_name = database_instance["name"]
                    self.log.info(f"Will collect metrics from next instance: {instance_name}")
                    return instance_name
                else:
                    continue

    @staticmethod
    def get_current_region():
        from kubernetes import config
        from kubernetes.client.apis import core_v1_api
        config.load_incluster_config()
        api = core_v1_api.CoreV1Api()
        namespace = open(SA_PATH).read()
        cm = api.read_namespaced_config_map("cloud-sql-configuration", namespace)
        return cm.data["region"]

    def set_interval(self):
        now = time.time()
        seconds = int(now)
        nanos = int((now - seconds) * 10 ** 9)
        interval = monitoring_v3.TimeInterval(
            {
                "end_time": {"seconds": seconds, "nanos": nanos},
                "start_time": {"seconds": (seconds - 210), "nanos": nanos},
            }
        )
        return interval

    def collect_all_metrics(self):
        interval = self.set_interval()
        tmp_metric = {}
        for key, endpoint in self.metric_dict.items():
            tmp_metric[key] = self.list_time_series(endpoint, interval)
        return tmp_metric

    def collect_status_metric(self):
        interval = self.set_interval()
        tmp_metric = {"cluster.status": self.list_time_series("database/state", interval)}
        return tmp_metric

    def map_cluster_state(self, value):
        if value == "RUNNING":
            return 0
        elif value == "FAILED":
            return 10
        else:
            return 6

    def list_time_series(self, metric_type, interval):
        project_name = "projects/" + self.project
        result = self.client.list_time_series(
            request={
                "name": project_name,
                "filter": f'metric.type = "cloudsql.googleapis.com/{metric_type}" AND resource.labels.database_id = "{self.project}:{self.instance}"',
                "interval": interval,
            }
        )
        for time_series in result._response._pb.time_series:
            if time_series.value_type == 1:
                for point in time_series.points:
                    return point.value.bool_value
            elif time_series.value_type == 2:
                for point in time_series.points:
                    return point.value.int64_value
            elif time_series.value_type == 3:
                for point in time_series.points:
                    return point.value.double_value
            elif time_series.value_type == 4:
                for point in time_series.points:
                    if metric_type == "database/state":
                        return self.map_cluster_state(point.value.string_value)
                    return point.value.string_value
