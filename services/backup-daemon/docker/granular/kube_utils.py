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
import logging
from kubernetes import client, config
from kubernetes.client.rest import ApiException

NS_PATH = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"

log = logging.getLogger("KubernetesAPI")
namespace = open(NS_PATH).read()

config.load_incluster_config()
api_instance = client.CoreV1Api()

def get_configmap(name):
    try:
        api_response = api_instance.read_namespaced_config_map(name, namespace)
        return api_response
    except ApiException as e:
        if e.status != 404:
            log.error(f'cannot get cm {namespace}/{name}')
            raise e 
        