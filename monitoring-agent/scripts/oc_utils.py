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

import base64
import json
import logging

import re
import requests
import time

from kubernetes.client import configuration

logger = logging.getLogger("metric-collector")


def is_ipv4(host):
    p = re.compile("^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$")
    return p.match(host)


def get_host_url(remote_host, remote_port):
    if is_ipv4(remote_host):
        return "https://{}:{}".format(remote_host, remote_port)
    else:
        return "https://[{}]:{}".format(remote_host, remote_port)


def get_configmap(remote_host, remote_port, oc_project, token, name):
    start_time = time.time()
    pod_data_url = "{}/api/v1/namespaces/{}/configmaps/{}" \
        .format(get_host_url(remote_host, remote_port), oc_project, name)
    response = requests.get(
        pod_data_url,
        headers={"Authorization": "Bearer {}".format(token)},
        verify="/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
    logger.debug("Load configmap {} info time: {}".format(name, (time.time() - start_time)))
    if response.status_code == 404:
        return None
    if response.status_code >= 400:
        raise Exception("Cannot get data for configmap {}".format(name))
    cm_data = json.loads(response.text)
    logger.debug("Collected info about configmap {}: {}".format(name, cm_data))
    return cm_data


def get_dc_info(remote_host, remote_port, oc_project, token):
    start_time = time.time()
    import os
    if os.environ.get("PATRONI_ENTITY_TYPE"):
        api_url = "{}/apis/apps/v1/namespaces/{}/deployments"
    else:
        api_url = "{}/oapi/v1/namespaces/{}/deploymentconfigs"
    dc_data_url = api_url.format(get_host_url(remote_host, remote_port), oc_project)
    response = requests.get(dc_data_url,
                            headers={"Authorization": "Bearer {}".format(token)},
                            verify="/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
    logger.debug("Load dc info time: {}".format((time.time() - start_time)))
    if response.status_code >= 400:
        raise Exception("Cannot collect dc data with code {}: {}".format(response.status_code, response.text))
    dcs_data = json.loads(response.text)
    logger.debug("Collected info about dc: {}".format(dcs_data))
    return dcs_data


def get_pods_info(remote_host, remote_port, oc_project, token, selector, selector_value):
    start_time = time.time()
    get_pods_data_url = "{}/api/v1/namespaces/{}/pods?labelSelector={}={}" \
        .format(get_host_url(remote_host, remote_port), oc_project, selector, selector_value)
    response = requests.get(
        get_pods_data_url,

        headers={"Authorization": "Bearer {}".format(token)},
        verify="/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
    logger.debug("Load pods info time for {}={}: {}".format(selector, selector_value, (time.time() - start_time)))
    if response.status_code >= 400:
        raise Exception("Cannot collect pods data with code {}: {}".format(response.status_code, response.text))
    pods_data = json.loads(response.text)
    logger.debug("Collected info about {}={} pods: {}".format(selector, selector_value, pods_data))
    return pods_data


def get_auth_token(remote_host, remote_port, oc_username, oc_password):
    auth_url = "{}/oauth/authorize?client_id=openshift-challenging-client&response_type=token" \
        .format(get_host_url(remote_host, remote_port))
    response = requests.get(
        auth_url,
        headers={
            "Authorization": "Basic {}".format(base64.b64encode("{}:{}".format(oc_username, oc_password))),
            "X-CSRF-Token": "1",
        },
        verify=False,
        allow_redirects=False
    )
    location_header = response.headers["Location"]
    m = re.search('access_token=([a-zA-Z0-9-_]+)', location_header)
    token = m.group(1)
    return token


def get_configmaps(remote_host, remote_port, oc_project, token):
    start_time = time.time()
    api_data_url = "{}/api/v1/namespaces/{}/configmaps/" \
        .format(get_host_url(remote_host, remote_port), oc_project)
    response = requests.get(
        api_data_url,
        headers={"Authorization": "Bearer {}".format(token)},
        verify="/var/run/secrets/kubernetes.io/serviceaccount/ca.crt")
    logger.debug("Load configmaps info time: {}".format((time.time() - start_time)))
    if response.status_code == 404:
        return None
    if response.status_code >= 400:
        raise Exception("Cannot get data for configmaps ")
    cm_data = json.loads(response.text)
    logger.debug("Collected info about configmaps: {}".format(cm_data))
    return cm_data


def get_statefulset_info(remote_host, remote_port, oc_project, token, statefulset_name):
    api_data_url = "{}/apis/apps/v1/namespaces/{}/statefulsets/{}" \
        .format(get_host_url(remote_host, remote_port), oc_project, statefulset_name)
    response = requests.get(
        api_data_url,
        headers={"Authorization": "Bearer {}".format(token)},
        verify="/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
    )
    if response.status_code == 404:
        return None
    if response.status_code >= 400:
        raise Exception("Cannot get data for statefulset")
    cm_data = json.loads(response.text)
    logger.debug("Collected info about statefulset: {}".format(cm_data))
    return cm_data


def get_deployment_info(remote_host, remote_port, oc_project, token, deployment_name):
    deployments = get_dc_info(remote_host, remote_port, oc_project, token)
    deployment = next(filter(lambda d: d['metadata']['name'] == deployment_name, deployments.get('items', [])), None)
    logger.debug("deployment with name {}\n{}".format(deployment_name, deployment))
    return deployment if deployment else None


def get_number_of_replicas_from_statefulset(remote_host, remote_port, oc_project, token, statefulset_name):
    statefulset_info = get_statefulset_info(remote_host, remote_port, oc_project, token, statefulset_name)
    return statefulset_info["spec"]["replicas"]


def get_number_of_replicas_from_deployment(remote_host, remote_port, oc_project, token, deployment_name):
    dc_info = get_deployment_info(remote_host, remote_port, oc_project, token, deployment_name)
    if dc_info:
        return dc_info.get('spec', {}).get('replicas', 0)
    else:
        return 0

def unavailable_replicas_from_deployment(remote_host, remote_port, oc_project, token, deployment_name):
    dc_info = get_deployment_info(remote_host, remote_port, oc_project, token, deployment_name)
    if dc_info:
        return dc_info.get('status', {}).get('unavailableReplicas', 0)
    else:
        return 0

def oc_login(remote_host, remote_port, oc_project, token):
    os_config = configuration.Configuration()
    os_config.verify_ssl = False
    os_config.assert_hostname = False
    os_config.host = get_host_url(remote_host, remote_port)
    os_config.api_key = {"authorization": "Bearer " + token}
    configuration.Configuration.set_default(os_config)


def load_file(path):
    with open(path, 'r') as f:
        file = f.read()
    return file