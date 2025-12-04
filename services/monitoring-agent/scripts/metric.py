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

default_tags = {
    "namespace": os.environ['NAMESPACE'],
    "pod_name": os.environ['HOSTNAME'],
    "selector": "health",
    "service_name": "pg-common-collector",
}


class Metric:
    """
    This is a simple wrapper class for metrics
    """

    def __init__(self, metric_name, tags, fields):
        self.metric_name = metric_name
        self._tags = tags.copy()
        self._fields = fields.copy()

    @property
    def fields(self):
        return ",".join(
            ["{}={}".format(t, self._fields[t]) for t in self._fields])

    @property
    def tags(self):
        return ",".join(["{}={}".format(t, self._tags[t]) for t in self._tags])

    def get_field_value(self, field, default):
        return self._fields.get(field, default)


def convert_metrics(cluster_name, metrics):
    for metric in metrics:
        print(f"ma.pg.{cluster_name}.{metric.metric_name},{metric.tags} {metric.fields}")
