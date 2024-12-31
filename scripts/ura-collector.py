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

from datetime import datetime
import urllib.request, urllib.error, urllib.parse
import json
import os
from collector_utils import linearizeJson, get_influxdb_value, mapStatuses
from oc_utils import get_pods_info, get_configmaps
from prometheus_client.parser import text_string_to_metric_families

import logging


class ConfigMapCollector(object):
    SA_TOKEN_PATH = '/var/run/secrets/kubernetes.io/serviceaccount/token'

    def __init__(self, remote_host, remote_port, namespace):
        with open(ConfigMapCollector.SA_TOKEN_PATH) as f:
            self.token = f.read()

        self.remote_host = remote_host
        self.remote_port = remote_port
        self.namespace = namespace
        self.log = logging.getLogger("ura-collector")

    def get_config_maps(self):
        return get_configmaps(self.remote_host, self.remote_port, self.namespace, self.token)

    def collect(self):
        cms = get_configmaps(self.remote_host, self.remote_port, self.namespace, self.token)
        for config_map in cms['items']:
            if config_map['metadata']['name'].endswith("collector-config"):
                self.log.info(f"start to process cm: "
                              f"{config_map['metadata']['name']}")
                self.process_config_map(config_map['data'])

    def process_config_map(self, data):
        for key in data:
            json_config = json.loads(data[key])
            for module_ in json_config:
                json_data = {}

                parameters = module_.get('parameters')
                type_ = parameters.get('type', 'url')
                metrics_type = parameters.get('metrics_type', 'json')

                if type_ == 'url':
                    url = parameters.get('url')
                    try:
                        json_data = json.loads(self.process_url_collect(url)) \
                            if metrics_type == 'json' else self.get_prometheus_metrics(url, module_)
                    except Exception as exc:
                        self.log.error(f"Something went wrong during collection of data via url {url}", exc)
                        continue
                elif type_ == 'script':
                    path = parameters.get('path')
                    json_data = self.process_script_collect(path)
                    if not json_data:
                        continue
                self.handle_out_put_json(json_data, module_)

    def get_prometheus_metrics(self, monitoring_url, module):
        import requests
        request = requests.get(monitoring_url)
        request.encoding = 'utf-8'
        data = request.text
        res_data = {}
        for family in text_string_to_metric_families(data):
            for sample in family.samples:
                key, tags, value = sample[0], sample[1], get_influxdb_value("", sample[2])
                tags["namespace"] = self.namespace
                tags["pod_name"] = self.get_pod_name(module)
                tags["service_name"] = module['parameters']['service_name']
                tag = ",".join(["{0}={1}".format(t, tags[t].replace(" ", "_")) for t in tags])
                print(f"ma.{key},{tag} value={value}")
        return res_data

    @staticmethod
    def process_url_collect(url):
        contents = urllib.request.urlopen(url).read()
        return contents.decode()

    # kostyl production
    @staticmethod
    def process_script_collect(path):
        import subprocess
        proc = subprocess.Popen(['python', path], stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
        str = proc.communicate()[0].decode('UTF-8')
        if not str:
            return ""
        return json.loads(str.replace("'", "\""))

    def get_pod_name(self, module_):
        pod_data = get_pods_info(
            self.remote_host, self.remote_port, self.namespace, self.token,
            module_['parameters']['selector']['key'],
            module_['parameters']['selector']['value']
        )
        pod_name = json.dumps(pod_data['items'][0]['metadata']['name']).replace('"', '')
        return pod_name

    def handle_out_put_json(self, json_data, module_):
        pod_name = self.get_pod_name(module_)
        tags = {
            "namespace": self.namespace,
            "pod_name": pod_name,
            "selector": module_['parameters']['output_selector'],
            "service_name": module_['parameters']['service_name']
        }

        points = linearizeJson(json_data)
        points = mapStatuses(points)

        tag = ",".join([f"{t}={tags[t]}" for t in tags])

        for key in points:
            value = get_influxdb_value(key, points[key])
            msg = f"ma.{key},{tag} value={value}"
            self.log.debug("data to telegraf: {}".format(msg))
            # output to stdout in influx-format metrics
            print(msg)


def main():
    # logging setup
    logging.basicConfig(
        filename='/proc/1/fd/1',
        filemode='w',
        level=logging.INFO,
        format='[%(asctime)s,%(msecs)03d][%(levelname)-5s][category=%(name)s]'
               '[pid=%(process)d] %(message)s',
        datefmt='%Y-%m-%dT%H:%M:%S'
    )

    debug = os.environ.get('DEBUG', 'false')
    if debug == "true":
        logging.getLogger().setLevel(logging.DEBUG)

    collector = ConfigMapCollector(
        remote_host=os.environ.get('KUBERNETES_SERVICE_HOST', 'localhost'),
        remote_port=os.environ.get('KUBERNETES_SERVICE_PORT', '5432'),
        namespace=os.environ.get('NAMESPACE', 'postgres-service')
    )
    collector.collect()


if __name__ == '__main__':
    main()
