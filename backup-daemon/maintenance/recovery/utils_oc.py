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

import datetime
import json
from abc import abstractmethod, ABCMeta
import subprocess
import logging
from utils_common import *
import pprint
import time

log = logging.getLogger()

try:
    from kubernetes import client
    from kubernetes.client import configuration
    from kubernetes.client import rest
    #from openshift import client as op_client
    from kubernetes.client.rest import ApiException
    from kubernetes.stream import stream
    from kubernetes.stream.ws_client import ERROR_CHANNEL, STDOUT_CHANNEL, STDERR_CHANNEL
    import kubernetes
    from six import iteritems

    def to_dict(self):
        """
        Returns the model properties as a dict
        """
        result = {}

        for attr, _ in iteritems(self.swagger_types):
            if attr in self.attribute_map:
                attr_name = self.attribute_map[attr]
            else:
                attr_name = attr

            value = getattr(self, attr)
            if isinstance(value, list):
                result[attr_name] = list([x.to_dict() if hasattr(x, "to_dict") else x for x in value])
            elif hasattr(value, "to_dict"):
                result[attr_name] = value.to_dict()
            elif isinstance(value, dict):
                result[attr_name] = dict([(item[0], item[1].to_dict())
                    if hasattr(item[1], "to_dict") else item for item in list(value.items())])
            else:
                result[attr_name] = value

        return result

    # kubernetes.client.models.v1_pod.V1Pod.to_dict = to_dict
    # kubernetes.client.models.v1_container.V1Container.to_dict = to_dict
    # kubernetes.client.models.v1_probe.V1Probe.to_dict = to_dict
    # kubernetes.client.models.v1_config_map.V1ConfigMap.to_dict = to_dict
    # kubernetes.client.models.v1_deployment.V1Deployment.to_dict = to_dict
    # openshift.client.models.v1_deployment_config.V1DeploymentConfig.to_dict = to_dict
    # kubernetes.client.models.v1_replication_controller.V1ReplicationController.to_dict = to_dict
    # kubernetes.client.models.v1_stateful_set.V1StatefulSet.to_dict = to_dict
    # kubernetes.client.models.v1beta1_stateful_set.V1beta1StatefulSet.to_dict = to_dict
    # kubernetes.client.models.apps_v1beta1_deployment.AppsV1beta1Deployment.to_dict = to_dict
    # kubernetes.client.models.v1_object_meta.V1ObjectMeta.to_dict = to_dict
    # kubernetes.client.models.v1_stateful_set_status.V1StatefulSetStatus.to_dict = to_dict
    # kubernetes.client.models.v1beta1_stateful_set_status.V1beta1StatefulSetStatus.to_dict = to_dict

    class ObjectEncoder(json.JSONEncoder):

        def default(self, o):
            if isinstance(o, datetime.datetime):
                return o.strftime("%Y-%m-%dT%H:%M:%SZ")
            return super(ObjectEncoder, self).default(o)

    use_kube_client = True
except ImportError as e:
    log.exception("Cannot use python client")
    use_kube_client = False


def get_api_token(oc_url, username, password):
    import base64
    import requests
    import re

    auth_url = oc_url + "/oauth/authorize?" \
                        "client_id=openshift-challenging-client&" \
                        "response_type=token"
    headers = {}
    user_password = username + b":" + password
    encoding = base64.b64encode(user_password)
    headers["Authorization"] = b"Basic " + encoding
    headers["X-CSRF-Token"] = b"1"

    result = requests.get(url=auth_url, headers=headers, verify=False, allow_redirects=False)
    if result.status_code != 302:
        raise ApiException(status=result.status_code, http_resp=result.text)
    location = result.headers["Location"]
    p = re.compile("access_token=([a-zA-Z0-9-_]+)")
    token = p.findall(location)[0]
    return token


class OpenshiftClient(metaclass=ABCMeta):
    @abstractmethod
    def login(self, oc_url, username, password, project, skip_tls_verify=False):
        pass

    @abstractmethod
    def use_token(self, oc_url, oc_token, project, skip_tls_verify=False):
        pass

    @abstractmethod
    def get_entities(self, entity_type):
        """
        :param entity_type:
        :return: parsed entity
        :rtype: []
        """
        pass

    @abstractmethod
    def get_entity(self, entity_type, entity_name):
        """
        :param entity_type:
        :param entity_name:
        :return: parsed entity
        :rtype: {}
        """
        pass

    @abstractmethod
    def get_entity_safe(self, entity_type, entity_name):
        """
        :param entity_type:
        :param entity_name:
        :return: parsed entity or None if entity does not exist
        :rtype: {}
        """
        pass

    def get_configmap(self, cm_name):
        """
        :param cm_name:
        :return: parsed entity
        :rtype: {}
        """
        return self.get_entity_safe("configmap", cm_name)

    def get_deployment(self, dc_name, type="dc"):
        """
        :param dc_name:
        :return: parsed entity
        :rtype: {}
        """
        return self.get_entity(type, dc_name)

    def get_stateful_set(self, stateful_set_name):
        return self.get_entity("statefulset", stateful_set_name)

    def get_env_for_pod(self, pod_id, env_name, default_value=None):
        pod = self.get_entity("pod", pod_id)
        envs = pod["spec"]["containers"][0]["env"]
        env = list([x for x in envs if x["name"] == env_name])
        return default_value if not env else env[0]["value"]

    def get_env_for_dc(self, dc_name, env_name, default_value=None):
        dc = self.get_entity("dc", dc_name)
        envs = dc["spec"]["template"]["spec"]["containers"][0]["env"]
        env = list([x for x in envs if x["name"] == env_name])
        return default_value if not env else env[0]["value"]

    @abstractmethod
    def set_env_for_dc(self, dc_name, env_name, env_value):
        pass

    @abstractmethod
    def get_deployment_names(self, dc_name_part):
        """
        :param dc_name_part:
        :return: list of deployment configs names which contain `dc_name_part`
        :rtype: [string]
        """
        pass

    def get_deployment_replicas_count(self, dc_name, type="dc"):
        """
        :param dc_name:
        :return: replica parameter from deployment corresponding to dc_name
        :rtype: int
        """
        deployment = self.get_deployment(dc_name, type)
        return int(deployment["spec"]["replicas"])

    def get_stateful_set_replicas_count(self, stateful_set_name):
        stateful_set = self.get_stateful_set(stateful_set_name)
        return int(stateful_set.get("spec").get("replicas"))

    def get_running_stateful_set_replicas_count(self, stateful_set_name):
        stateful_set = self.get_stateful_set(stateful_set_name)
        status = stateful_set.get("status")
        return min(int(status.get("ready_replicas") or "0"),
                   int(status.get("updated_replicas") or "0"))

    def get_liveness_probe_from_stateful_set(self, stateful_set_name):
        stateful_set = self.get_stateful_set(stateful_set_name)
        return stateful_set["spec"]["template"]["spec"]["containers"][0]["livenessProbe"]

    @abstractmethod
    def get_replicas_desc(self, dc_name, running=True):
        """
        :param dc_name:
        :param running:
        :return: list of replicas descriptions
        :rtype: []
        """
        pass

    def get_replicas_names(self, dc_name, running=True):
        """
        :param dc_name:
        :param running:
        :return:
        :rtype: [string]
        """
        pod_names = list([p["metadata"]["name"] for p in self.get_replicas_desc(dc_name, running)])
        return pod_names

    @abstractmethod
    def delete_entity(self, entity_type, entity_name, ignore_not_found=True):
        pass

    @abstractmethod
    def oc_exec(self, pod_id, command):
        """
        :param pod_id:
        :param command:
        :return:
        :rtype: string
        """
        pass

    @abstractmethod
    def get_logs(self, pod_id, since=None):
        """
        :param pod_id:
        :param since:
        :return:
        :rtype: string
        """
        pass

    @abstractmethod
    def rsync(self, source, target):
        pass

    @abstractmethod
    def delete_pod(self, pod_id, grace_period=None):
        pass

    @abstractmethod
    def scale(self, dc_name, count, entity="dc"):
        pass

    @abstractmethod
    def apply_object(self, data):
        pass

    @abstractmethod
    def replace_object(self, data):
        pass

    @abstractmethod
    def get_cluster_pods(self, cluster_name, running=True):
        pass

    @abstractmethod
    def get_cluster_pods_desc(self, cluster_name, running=True):
        pass

    @abstractmethod
    def get_pods_by_label(self, label_selector):
        pass

    def get_pod_status(self, pod_id):
        items = self.get_entities("pod")
        for x in items:
            if x["metadata"]["name"] == pod_id:
                return x["status"]["phase"]
            else:
                log.info("Pod {} not found".format(pod_id))

    def is_pod_ready(self, pod_id, attempts=5):
        for i in range(1, attempts):
            time.sleep(5)
            status = self.get_pod_status(pod_id)
            log.info("Pod state is {}".format(status))
            if status.lower() == "running":
                return True
            else:
                log.info("Retrying...")
        log.info("Can't get pod {} status".format(pod_id))
        return False

    def get_postgres_backup_daemon_pod(self):
        pods_from_dcs = self.get_pods_by_label("app=postgres-backup-daemon")
        pods_from_deployments = self.get_pods_by_label("component=postgres-backup-daemon")
        return pods_from_dcs + pods_from_deployments


class OpenshiftShellClient(OpenshiftClient):
    def __init__(self, oc_path="oc", config_file="./oc_config_file.yaml"):
        self.oc = oc_path + " --config={}".format(config_file)

    def login(self, oc_url, username, password, project, skip_tls_verify=False):
        log.info("Log in as {} to {}".format(username, oc_url))
        subprocess.check_call(
            '{} login -u "{}" -p "{}" {} {}'
                .format(self.oc,
                        username, password, oc_url,
                        ("--insecure-skip-tls-verify=true" if skip_tls_verify else "")),
            shell=True, stdout=subprocess.PIPE)

        if project:
            log.info("Change project to {}".format(project))
            subprocess.check_call(
                '{} project {}'.format(self.oc, project),
                shell=True, stdout=subprocess.PIPE)

    def use_token(self, oc_url, oc_token, project, skip_tls_verify=False):
        raise NotImplementedError("Cannot use token with oc client")

    def get_entities(self, entity_type):
        entity = json.loads(subprocess.Popen(
            '{} get {} -o json'.format(self.oc, entity_type),
            shell=True, stdout=subprocess.PIPE).stdout.read())
        return entity["items"]

    def get_entity(self, entity_type, entity_name):
        entity = json.loads(subprocess.Popen(
            '{} get {} {} -o json'.format(self.oc, entity_type, entity_name),
            shell=True, stdout=subprocess.PIPE).stdout.read())
        return entity

    def get_entity_safe(self, entity_type, entity_name):
        entities_data = json.loads(subprocess.Popen(
            '{} get {} -o json'.format(self.oc, entity_type), shell=True, stdout=subprocess.PIPE).stdout.read())
        entities = list([p for p in entities_data["items"] if entity_name == p["metadata"]["name"]])
        if entities:
            return entities[0]
        return None

    def set_env_for_dc(self, dc_name, env_name, env_value):
        log.info("Try to set env {}={} to deployment {}".format(env_name, env_value, dc_name))
        env_repr = "{}={}".format(env_name, env_value) if env_value else "{}-".format(env_name)
        subprocess.check_call("{} set env dc {} {}".format(self.oc, dc_name, env_repr), shell=True)

    def get_deployment_names(self, dc_name_part, type="dc"):
        deployments_data = json.loads(subprocess.Popen(
            '{} get {} -o json'.format(self.oc, type), shell=True, stdout=subprocess.PIPE).stdout.read())
        deployments = list(
            [p["metadata"]["name"] for p in [p for p in deployments_data["items"] if dc_name_part in p["metadata"]["name"]]])
        return deployments

    def get_replicas_desc(self, dc_name, running=True):
        pods_data = json.loads(subprocess.Popen(
            "{} get pods {} -o json".format(self.oc, ("-a" if not running else "")),
            shell=True, stdout=subprocess.PIPE).stdout.read())
        pods = list(
            [p for p in pods_data["items"] if "deploymentconfig" in p["metadata"]["labels"] and
                   dc_name in p["metadata"]["labels"]["deploymentconfig"]])
        if running:
            pods = list([p for p in pods if "running" == p["status"]["phase"].lower()])
        return pods

    def delete_entity(self, entity_type, entity_name, ignore_not_found=True):
        log.debug("Try to delete entity {} {}".format(entity_type, entity_name))
        inf_value = "true" if ignore_not_found else "false"
        subprocess.check_call("{} delete {} {} --ignore-not-found={}"
                              .format(self.oc, entity_type, entity_name, inf_value),
                              shell=True)

    def oc_exec(self, pod_id, command):
        log.info("Try to execute '{}' on pod {}".format(command, pod_id))
        process = subprocess.Popen("{} exec {} -- {}".format(self.oc, pod_id, command), shell=True,
                                   stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        if process.wait() != 0:
            raise Exception("Error occured during execution. "
                            "Return code: {}, stderr: {}, stdout: {}"
                            .format(process.returncode, process.stderr.read(), process.stdout.read()))
        return process.stdout.read().decode()

    def get_logs(self, pod_id, since=None):
        log.debug("Try to obtain logs from pod {} for last {}s.".format(pod_id, since))
        cmd = "{} logs {} --since={}s".format(self.oc, pod_id, since)
        if not since:
            cmd = "{} logs {}".format(self.oc, pod_id)
        process = subprocess.Popen(cmd, shell=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        result = process.communicate()
        log.debug("Received data len: {}".format(0 if not result[0] else len(result[0])))
        if process.returncode != 0 or result[1]:
            log.warning("Error during log obtain. code{}, message: {}".format(process.returncode, result[1]))
        return result[0].decode()

    def rsync(self, source, target):
        subprocess.check_call("{} rsync {} {}".format(self.oc, source, target), shell=True)

    def delete_pod(self, pod_id, grace_period=None):
        log.debug("Remove pod {} with grace-period {}".format(pod_id, grace_period))
        grace_period = "--grace-period={}".format(grace_period) if grace_period else ""
        p = subprocess.Popen("{} delete pod {} {}".format(self.oc, pod_id, grace_period),
                             shell=True, stdin=subprocess.PIPE, stderr=subprocess.PIPE)
        if p.wait() != 0:
            error = p.stderr.read()
            if 'pods "{}" not found'.format(pod_id) in error.decode():
                log.warning("Cannot remove pod {} - no such pod.".format(pod_id))
                pass
            else:
                raise Exception("Cannot remove pod. Error: {}".format(error))

    @retry(tries=5)  # handle case when DC version is Unknown
    def scale(self, name, count, entity="dc"):
        subprocess.check_call("{} scale --replicas={} {} {}"
                              .format(self.oc, count, entity, name), shell=True)

    def apply_object(self, data):
        p = subprocess.Popen("{} apply -f -".format(self.oc), shell=True, stdin=subprocess.PIPE)
        p.communicate(input=json.dumps(data).encode())

    def replace_object(self, data):
        p = subprocess.Popen("{} replace -f -".format(self.oc), shell=True, stdin=subprocess.PIPE)
        p.communicate(input=json.dumps(data).encode())

    def get_cluster_pods(self, cluster_name, running=True):
        pods_data = json.loads(
            subprocess.Popen("{} get pods {} --selector=\"pgcluster={}\" -o json"
                             .format(self.oc, ("-a" if not running else ""), cluster_name),
                             shell=True, stdout=subprocess.PIPE).stdout.read())
        pods = [pod["metadata"]["name"] for pod in pods_data["items"]]
        return pods

    def get_cluster_pods_desc(self, cluster_name, running=True):
        pods_data = json.loads(
            subprocess.Popen("{} get pods {} --selector=\"pgcluster={}\" -o json"
                             .format(self.oc, ("-a" if not running else ""), cluster_name),
                             shell=True, stdout=subprocess.PIPE).stdout.read())
        return pods_data["items"]

    def get_pods_by_label(self, label_selector):
        pods_data = json.loads(
            subprocess.Popen("{0} get pods {1} -l={2} -o json".format(self.oc, "-a", label_selector),
                             shell=True, stdout=subprocess.PIPE).stdout.read())
        pods = [pod["metadata"]["name"] for pod in pods_data["items"]]
        return pods


class OpenshiftPyClient(OpenshiftClient):

    def __init__(self):
        super(OpenshiftPyClient, self).__init__()
        self._api_client = None
        self.project = None

    def login(self, oc_url, username, password, project, skip_tls_verify=False):
        log.info("Log in as {} to {}".format(username, oc_url))
        
        # Configuration for Kubernetes client
        os_config = client.Configuration()
        os_config.verify_ssl = not skip_tls_verify
        os_config.assert_hostname = False
        os_config.host = oc_url
        
        openshift_token = get_api_token(oc_url, b"admin", b"admin")
        os_config.api_key = {"authorization": "Bearer " + openshift_token}
        
        self._api_client = client.ApiClient(configuration=os_config)

        log.info("Will use namespace {}".format(project))
        self.project = project

    # This method is called if OC_TOKEN env variable is presented,
    # In our cases, this env is set only in case of robot tests
    def use_token(self, oc_url, oc_token, project, skip_tls_verify=False):
        log.info("Log in to {} with token".format(oc_url))
        from kubernetes import config
        config.load_incluster_config()
        log.info("Will use namespace {}".format(project))
        self.project = project

    def __list_entities(self, entity_type):
        if entity_type == "pod":
            core_api = client.CoreV1Api(self._api_client)
            items = core_api.list_namespaced_pod(self.project).items
        elif entity_type == "configmap":
            core_api = client.CoreV1Api(self._api_client)
            items = core_api.list_namespaced_config_map(self.project).items
        elif entity_type == "dc":
            core_api = client.CoreV1Api(self._api_client)
            items = core_api.list_namespaced_deployment_config(self.project).items
        elif entity_type == "rc":
            core_api = client.CoreV1Api(self._api_client)
            items = core_api.list_namespaced_replication_controller(self.project).items
        elif entity_type == "statefulset":
            try:
                apps_api = client.AppsV1Api(self._api_client)
                items = apps_api.list_namespaced_stateful_set(self.project).items
            except:
                apps_api = client.AppsV1beta1Api(self._api_client)
                items = apps_api.list_namespaced_stateful_set(self.project).items
        elif entity_type == "deployment":
            try:
                apps_api = client.AppsV1beta1Api(self._api_client)
                items = apps_api.list_namespaced_deployment(self.project).items
            except:
                apps_api = client.AppsV1Api(self._api_client)
                items = apps_api.list_namespaced_deployment(self.project).items
        else:
            raise NotImplementedError("Cannot list {}".format(entity_type))
        return items

    def __to_dict(self, entity):
        data = entity.to_dict()
        if isinstance(entity, kubernetes.client.models.v1_pod.V1Pod):
            data["kind"] = "Pod"
        elif isinstance(entity,
                        kubernetes.client.models.v1_config_map.V1ConfigMap):
            data["kind"] = "ConfigMap"
        elif isinstance(entity,
                        kubernetes.client.models.v1_deployment.V1Deployment):
            data["kind"] = "Deployment"
        elif isinstance(entity,
                        kubernetes.client.models.v1_replication_controller.V1ReplicationController):
            data["kind"] = "ReplicationController"
        elif isinstance(entity, kubernetes.client.models.v1_stateful_set.V1StatefulSet):
            data["kind"] = "StatefulSet"
        else:
            raise NotImplementedError("Cannot transform to dict entity {}"
                                      .format(entity))
        data = json.loads(json.dumps(data, cls=ObjectEncoder))
        return data

    def get_entities(self, entity_type):
        items = self.__list_entities(entity_type)
        return list([self.__to_dict(x) for x in items])

    def get_entity(self, entity_type, entity_name):
        items = self.__list_entities(entity_type)
        if entity_type == "statefulset":
            filtered_items = [item for item in items if "patroni" in item.metadata.name]
        else:
            filtered_items = list(filter(lambda x: x.metadata.name == entity_name, items))
        if entity_name == "pg-patroni-node1":
            returned_value = self.__to_dict(filtered_items[0])
        elif entity_name == "pg-patroni-node2":
            returned_value = self.__to_dict(filtered_items[1])
        else:
            returned_value = self.__to_dict(filtered_items[0])
        return returned_value

    def get_entity_safe(self, entity_type, entity_name):
        try:
            return self.get_entity(entity_type, entity_name)
        except Exception as e:
            return None

    def set_env_for_dc(self, dc_name, env_name, env_value):
        log.info("Try to set env {}={} to deployment {}".format(env_name, env_value, dc_name))
        core_api = client.CoreV1Api(self._api_client)
        dc = core_api.read_namespaced_deployment_config(dc_name, self.project)
        base_envs = dc.spec.template.spec.containers[0].env
        base_env = list([x for x in base_envs if x.name == env_name])
        if env_value:
            if base_env:
                base_env[0].value = env_value
            else:
                base_envs.append(client.V1EnvVar(name=env_name, value=env_value))
            log.info(core_api.patch_namespaced_deployment_config(dc_name, self.project, dc))
        else:
            if base_env:
                base_envs.remove(base_env[0])
                log.info(core_api.replace_namespaced_deployment_config(dc_name, self.project, dc))

    def get_deployment_names(self, dc_name_part, type="dc"):
        items = self.__list_entities(type)
        deployments = list(
            [p.metadata.name for p in [p for p in items if dc_name_part in p.metadata.name]])
        return deployments

    def get_replicas_desc(self, dc_name, type="dc", running=True):
        items = self.__list_entities("pod")
        pods = list(
            [p for p in items if "deploymentconfig" in p.metadata.labels and
                   dc_name in p.metadata.labels["deploymentconfig"]])
        if running:
            pods = list(
                [self.__to_dict(x) for x in [p for p in pods if "running" == p.status.phase.lower()]])
        else:
            pods = list([self.__to_dict(x) for x in pods])
        return pods

    def delete_entity(self, entity_type, entity_name, ignore_not_found=True):
        log.debug("Try to delete entity {} {}".format(entity_type, entity_name))
        try:
            if entity_type == "pod":
                core_api = client.CoreV1Api(self._api_client)
                core_api.delete_namespaced_pod(entity_name, self.project, {})
            elif entity_type == "configmap":
                body = client.V1DeleteOptions()
                core_api = client.CoreV1Api(self._api_client)
                core_api.delete_namespaced_config_map(entity_name, self.project, body=body,)
            elif entity_type == "dc":
                core_api = client.CoreV1Api(self._api_client)
                core_api.delete_namespaced_deployment_config(entity_name,
                                                             self.project, {})
            elif entity_type == "rc":
                core_api = client.CoreV1Api(self._api_client)
                core_api.delete_namespaced_replication_controller(entity_name,
                                                                  self.project, {})
            elif entity_type == "statefulset":
                core_api = client.AppsV1Api(self._api_client)
                core_api.delete_namespaced_stateful_set(entity_name,
                                                        self.project, {})
            else:
                raise NotImplementedError("Cannot delete {}".format(entity_type))
        except kubernetes.client.rest.ApiException as e:
            if ignore_not_found and e.reason == "Not Found":
                return
            else:
                raise e

    @retry(tries=30, delay=5)
    def oc_exec(self, pod_id, command):
        log.debug(f"Try to execute '{command}' on pod {pod_id}")
        core_api = client.CoreV1Api(self._api_client)

        exec_command = [
            '/bin/sh', '-c', command
        ]

        try:
            resp = stream(core_api.connect_get_namespaced_pod_exec,
                          pod_id,
                          self.project,
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

        log.debug(f"Command '{command}' completed for pod {pod_id}")
        return None

    def get_logs(self, pod_id, since=None):
        log.debug("Try to obtain logs from pod {} for last {}s."
                  .format(pod_id, since))
        # time.sleep(20)
        core_api = client.CoreV1Api(self._api_client)
        if since:
            return core_api.read_namespaced_pod_log(pod_id, self.project, since_seconds=since)
        else:
            return core_api.read_namespaced_pod_log(pod_id, self.project)

    def rsync(self, source, target):
        if ":" in source:
            raise Exception("Cannot load files from pod yet")
        (pod_id, target_dir) = target.split(":")
        log.debug("Try to upload files from {} to pod {} in dir {}"
                  .format(source, pod_id, target_dir))

        import tarfile
        import tempfile
        tempfile_fd = tempfile.TemporaryFile()
        tar = tarfile.open(fileobj=tempfile_fd, mode='w:gz')
        tar.add(source)
        tar.close()
        tempfile_fd.flush()
        tempfile_fd.seek(0)

        core_api = client.CoreV1Api(self._api_client)
        exec_command = ['tar', 'xzvf', '-', '-C', target_dir]
        resp = stream(core_api.connect_get_namespaced_pod_exec, pod_id,
                      self.project,
                      command=exec_command,
                      stderr=True, stdin=True,
                      stdout=True, tty=False,
                      _preload_content=False)

        resp.write_stdin(tempfile_fd.read())
        resp.update(1)
        if resp.peek_stdout():
            log.debug("STDOUT: %s" % resp.read_stdout())
        if resp.peek_stderr():
            log.debug("STDERR: %s" % resp.read_stderr())
        resp.close()
        tempfile_fd.close()
        error = resp.read_channel(ERROR_CHANNEL)
        if error and "Success" != json.loads(error).get("status"):
            raise Exception("Error occurred during execution: {}. "
                            .format(error))

    def delete_pod(self, pod_id, grace_period=None):
        log.debug("Try to remove pod {} with grace-period {}"
                  .format(pod_id, grace_period))
        core_api = client.CoreV1Api(self._api_client)
        try:
            status = core_api.delete_namespaced_pod(pod_id, self.project, {},
                                                    grace_period_seconds=grace_period)
            log.debug(status)
        except ApiException as ae:
            if 'pods \\"{}\\" not found'.format(pod_id) in ae.body:
                log.warning("Cannot remove pod {} - no such pod."
                            .format(pod_id))
                pass
            else:
                raise ae

    @retry(tries=5)  # handle case when DC version is Unknown
    def scale(self, name, count, entity="dc"):
        log.debug("Try to scale {} {} to {} replicas".format(entity, name, count))
        if entity == "dc":
            core_api = client.CoreV1Api(self._api_client)
            data = core_api.patch_namespaced_deployment_config(name, self.project, {"spec": {"replicas": count}})
        elif entity == "statefulset":
            core_api = client.AppsV1Api(self._api_client)
            data = core_api.patch_namespaced_stateful_set(name, self.project, {"spec": {"replicas": count}})
        elif entity == "deployment":
            try:
                core_api = client.AppsV1beta1Api(self._api_client)
                data = core_api.patch_namespaced_deployment(name, self.project, {"spec": {"replicas": count}})
            except:
                core_api = client.AppsV1Api(self._api_client)
                data = core_api.patch_namespaced_deployment(name, self.project, {"spec": {"replicas": count}})
        else:
            raise NotImplementedError("Cannot scale entity {} of type {}".format(name, entity))
        return self.__to_dict(data)

    def get_json_diff(self, source, data):
        return Differ().get_json_diff(source, data, keep_name=True)

    def apply_object(self, data):
        log.debug("Try to apply {}".format(data))
        entity_type = data["kind"]
        entity_name = data["metadata"]["name"]
        if entity_type == "Pod":
            core_api = client.CoreV1Api(self._api_client)
            source = reset_last_applied(self.get_entity("pod", entity_name))
            diff = self.get_json_diff(source, data)
            if diff:
                return self.__to_dict(core_api.patch_namespaced_pod(entity_name, self.project, diff))
            return data
        elif entity_type == "ConfigMap":
            core_api = client.CoreV1Api(self._api_client)
            source = self.get_entity("configmap", entity_name)
            diff = self.get_json_diff(source, data)
            if diff:
                return self.__to_dict(core_api.patch_namespaced_config_map(entity_name, self.project, diff))
            return data
        elif entity_type == "DeploymentConfig":
            core_api = client.CoreV1Api(self._api_client)
            source = reset_last_applied(self.get_entity("dc", entity_name))
            diff = self.get_json_diff(source, data)
            if diff:
                return self.__to_dict(core_api.patch_namespaced_deployment_config(entity_name, self.project, diff))
            return data
        elif entity_type == "ReplicationController":
            # todo[anin] check container for same bug as above
            core_api = client.CoreV1Api(self._api_client)
            source = self.get_entity("rc", entity_name)
            diff = self.get_json_diff(source, data)
            if diff:
                return self.__to_dict(core_api.patch_namespaced_replication_controller(entity_name, self.project, diff))
            return data
        elif entity_type == "StatefulSet":
            # todo[anin] check container for same bug as above
            apps_api = client.AppsV1Api(self._api_client)
            source = reset_last_applied(self.get_entity("statefulset", entity_name))
            # return self.__to_dict(apps_api.patch_namespaced_stateful_set(entity_name, self.project, data))
            # TODO: It's not working at all, we are just returning same without patching, just rolled back coz of release
            return data
        elif entity_type == "Deployment":
            try:
                apps_api = client.AppsV1beta1ApiApi(self._api_client)
                source = reset_last_applied(self.get_entity("deployment", entity_name))
            except:
                apps_api = client.AppsV1Api(self._api_client)
                source = reset_last_applied(self.get_entity("deployment", entity_name))

            diff = self.get_json_diff(source, data)
            if diff:
                return self.__to_dict(apps_api.patch_namespaced_deployment(entity_name, self.project, diff))
            return data
        else:
            raise NotImplementedError("Cannot apply {}".format(entity_type))

    def replace_object(self, data):
        log.debug("Try to apply {}".format(data))
        entity_type = data["kind"]
        entity_name = data["metadata"]["name"]
        if entity_type == "Pod":
            core_api = client.CoreV1Api(self._api_client)
            return self.__to_dict(core_api.replace_namespaced_pod(entity_name, self.project, data))
        elif entity_type == "ConfigMap":
            core_api = client.CoreV1Api(self._api_client)
            return self.__to_dict(core_api.replace_namespaced_config_map(entity_name, self.project, data))
        elif entity_type == "DeploymentConfig":
            core_api = client.CoreV1Api(self._api_client)
            return self.__to_dict(core_api.replace_namespaced_deployment_config(entity_name, self.project, data))
        elif entity_type == "ReplicationController":
            core_api = client.CoreV1Api(self._api_client)
            return self.__to_dict(core_api.replace_namespaced_replication_controller(entity_name, self.project, data))
        elif entity_type == "StatefulSet":
            core_api = client.AppsV1Api(self._api_client)
            return self.__to_dict(core_api.replace_namespaced_stateful_set(entity_name, self.project, data))
        else:
            raise NotImplementedError("Cannot replace {}".format(entity_type))

    def get_cluster_pods(self, cluster_name, running=True):
        pods = [x for x in self.__list_entities("pod") if "pgcluster" in x.metadata.labels and x.metadata.labels["pgcluster"] == cluster_name]
        pods = list([x.metadata.name for x in pods])
        return pods

    def get_cluster_pods_desc(self, cluster_name, running=True):
        pods = [x for x in self.__list_entities("pod") if "pgcluster" in x.metadata.labels and x.metadata.labels["pgcluster"] == cluster_name]
        pods = list([self.__to_dict(x) for x in pods])
        return pods

    def get_pods_by_label(self, label_selector):
        core_api = client.CoreV1Api(self._api_client)
        items = core_api.list_namespaced_pod(self.project, label_selector=label_selector).items
        pods = list([x.metadata.name for x in items])
        return pods

    def get_secret_data(self, secret_name):
        core_api = client.CoreV1Api()
        try:
            api_response = core_api.read_namespaced_secret(secret_name, self.project)
            import base64
            data = api_response.data
            password = base64.b64decode(data.get("password")).decode('utf-8')
            user_data = data.get("user")
            if not user_data:
                user_data = data.get("username")
            user = base64.b64decode(user_data).decode('utf-8')
            return user, password
        except ApiException as exc:
            log.error(exc)
            raise exc


def get_client(oc_path="oc", oc_config_file="./oc_config_file.yaml"):
    """
    Returns wrapper over shell if oc client present
    otherwise tries to return wrapper over kubernetes.client
    :return:
    :rtype: OpenshiftClient
    """
    if use_kube_client:
        return OpenshiftPyClient()
    else:
        return OpenshiftShellClient(oc_path, oc_config_file)


def reset_last_applied(entity):
    entity["metadata"].pop("namespace", None)
    entity["metadata"].pop("selfLink", None)
    entity["metadata"].pop("uid", None)
    entity["metadata"].pop("resourceVersion", None)
    entity["metadata"].pop("generation", None)
    entity["metadata"].pop("creationTimestamp", None)
    entity["metadata"].pop("managedFields", None)
    entity.pop("status", None)
    return entity


class OpenshiftOrchestrator:
    def __init__(self, client, retry_count=100):
        self.oc = client
        self.retry_count = retry_count

    def ensure_scale(self, dc_name, replicas, type="dc"):
        log.info("Try to scale dc {} to {} replicas.".format(dc_name, replicas))
        for i in range(1, self.retry_count):
            self.oc.scale(dc_name, replicas, type)
            time.sleep(1)
            if replicas == self.oc.get_deployment_replicas_count(dc_name, type):
                log.debug("dc {} was scaled successfully.".format(dc_name))
                return
        raise Exception("Was not able to scale deployment {}".format(dc_name))

    def wait_replicas(self, dc_name, replicas, running=False):
        log.info("Wait {} replicas of dc {}".format(replicas, dc_name))
        replica_names = None
        for i in range(1, self.retry_count):
            replica_names = self.oc.get_replicas_names(dc_name, running=running)
            log.info("Wait {} replicas of dc {}. Actual replicas: {}".format(replicas, dc_name, replica_names))
            log.debug("Wait {} replicas of dc {}. Actual replicas: {}".format(replicas, dc_name, replica_names))
            if len(replica_names) == replicas:
                log.debug("Found {} replicas of dc {}.".format(replicas, dc_name))
                return
            time.sleep(1)
        raise Exception("Expected replicas count was {} but actual replicas: {}".format(replicas, replica_names))

    def wait_replicas_statefulset(self, stateful_set_name, replicas_number):
        for i in range(1, self.retry_count):
            log.info("Waiting till all replicas are ready")
            time.sleep(1)
            ready_replicas = self.oc.get_running_stateful_set_replicas_count(stateful_set_name)
            if replicas_number == ready_replicas:
                log.debug("Statefulset {} was scaled successfully.".format(stateful_set_name))
                return
        raise Exception("Was not able to scale statefulset {}".format(stateful_set_name))

    def set_env_on_dc(self, dc_name, env_name, env_value, scale_up=True):
        log.info("Try to set env {}={} to deployment {}".format(env_name, env_value, dc_name))
        log.info("Scale down before changes")
        self.ensure_scale(dc_name, 0)
        replica_names = self.oc.get_replicas_names(dc_name, running=False)
        for replica in replica_names:
            self.oc.delete_pod(replica, 1)
        self.wait_replicas(dc_name, 0)

        log.info("Change env")
        self.oc.set_env_for_dc(dc_name, env_name, env_value)

        log.debug("Check if env present on actual version of dc")
        for i in range(1, self.retry_count):
            dc = self.oc.get_deployment(dc_name)
            envs = dc["spec"]["template"]["spec"]["containers"][0]["env"]
            env = list([x for x in envs if x["name"] == env_name])
            version = dc["metadata"].get("resourceVersion") if dc["metadata"].get("resourceVersion") else dc["metadata"].get("resource_version")
            log.debug("Env: {}. Version: {}".format(env, version))
            if env_value:
                if env and env[0]["value"] == env_value and version != "Unknown":
                    break
            else:
                if not env and version != "Unknown":
                    break
            log.debug("Wait for changes to apply")
            time.sleep(1)

        if scale_up:
            log.info("Scale up after changes")
            self.ensure_scale(dc_name, 1)
            self.wait_replicas(dc_name, 1, running=True)

    def replace_command_on_deployment(self, deployment_name, command, scale_down=False):
        log.info("Try to set command {} to deployment {}".format(command, deployment_name))
        deployment_entity = self.oc.get_deployment(deployment_name, "deployment")
        deployment_entity["spec"]["template"]["spec"]["containers"][0]["command"] = command
        old_generation = deployment_entity["status"]["observed_generation"]
        log.info("Observed generation before update: {}".format(old_generation))
        if scale_down:
            deployment_entity["spec"]["replicas"] = 0
        self.oc.apply_object(deployment_entity)
        for _ in range(1, self.retry_count):
            updated_deployment = self.oc.get_deployment(deployment_name, "deployment")
            new_generation = updated_deployment["status"].get("observed_generation")
            ready_replicas = updated_deployment["status"].get("ready_replicas")
            if new_generation == (old_generation + 1) and ready_replicas:
                break
            else:
                time.sleep(1)


    def replace_command_on_dc(self, dc_name, command, scale_up=True):
        log.info("Try to set command {} to deployment config {}".format(command, dc_name))

        log.info("Scale down before changes")
        self.ensure_scale(dc_name, 0)
        replica_names = self.oc.get_replicas_names(dc_name, running=False)
        for replica in replica_names:
            self.oc.delete_pod(replica, 1)
        self.wait_replicas(dc_name, 0)

        log.info("Change command")
        dc = self.oc.get_deployment(dc_name)
        dc["spec"]["template"]["spec"]["containers"][0]["command"] = command

        last_applied = dc["metadata"]["annotations"]["kubectl.kubernetes.io/last-applied-configuration"]
        if last_applied:
            dc = reset_last_applied(dc)
            log.debug(json.dumps(dc))
            self.oc.apply_object(dc)
        else:
            log.debug(json.dumps(dc))
            self.oc.replace_object(dc)

        log.debug("Check if command present on actual version of dc")
        for i in range(1, self.retry_count):
            dc = self.oc.get_deployment(dc_name)
            container_def = dc["spec"]["template"]["spec"]["containers"][0]
            current_command = None
            if "command" in container_def:
                current_command = container_def["command"]
            version = dc["metadata"].get("resourceVersion") if dc["metadata"].get("resourceVersion") else dc["metadata"].get("resource_version")
            log.debug("Command: {}. Version: {}".format(current_command, version))
            if current_command == command and version != "Unknown":
                break
            log.debug("Wait for changes to apply")
            time.sleep(1)

        if scale_up:
            log.info("Scale up after changes")
            self.ensure_scale(dc_name, 1)
            self.wait_replicas(dc_name, 1, running=True)

    def replace_command_on_statefulset(self, stateful_set_name, command, scale_up=True):
        log.info("Try to set command {} to statefulset {}".format(command, stateful_set_name))

        stateful_set = self.oc.get_stateful_set(stateful_set_name)
        replicas_number = self.oc.get_stateful_set_replicas_count(stateful_set_name)
        log.info("Scale down before changes")
        self.scale_stateful_set(stateful_set_name, 0)

        log.info("Change command")

        stateful_set["spec"]["template"]["spec"]["containers"][0]["command"] = command

        stateful_set = reset_last_applied(stateful_set)
        log.debug(json.dumps(stateful_set))
        self.oc.apply_object(stateful_set)

        time.sleep(5)
        if scale_up:
            log.info("Scale up after changes")
            self.scale_stateful_set(stateful_set_name, replicas_number)

    def wait_for_one_of_records_in_logs_since(self, pod_id, records, start_time,
                                              wait_message=None, restart_timer_records=None):
        """
        Receives logs from specified pod and checks if logs contain one of records since start_time.
        Process will wait for record until retry counter reaches self.retry_count or
        logs will be filled with one of restart_timer_records.
        Sleep interval is 5 seconds. Between intervals Method can inform user about process with wait_message
        :param pod_id:
        :param records:
        :type records: list
        :param start_time:
        :param wait_message:
        :param restart_timer_records:
        :type restart_timer_records: list
        :return: Nothing.
        :raise: Exception if record was not found
        """
        log.debug("Wait for records {} in pod {} from {}.".format(records, pod_id, start_time))
        counter = 0
        sleep_time = 20
        fetch_start_time = start_time
        while counter < self.retry_count / 5:
            fetch_end_time = time.time()
            # add small overlap to ensure that we dont miss peace of logs
            time_passed = int(fetch_end_time - fetch_start_time) + sleep_time
            logs = self.oc.get_logs(pod_id, time_passed)
            # check if logs contains expected record
            for record in records:
                record_count = logs.count(record)
                if record_count > 0:
                    log.debug("Found record '{}' in logs. Found {} records.".format(record, record_count))
                    return
            # check if logs contains new restart records.
            if restart_timer_records:
                for record in restart_timer_records:
                    record_count = logs.count(record)
                    if record_count > 0:
                        if wait_message:
                            log.info(wait_message)
                        log.debug("Found record '{}' in logs. Will prolong wait time. ".format(record))
                        counter = 0
            time.sleep(sleep_time)
            counter = counter + 1
            fetch_start_time = fetch_end_time
        raise Exception("Cannot find records in logs")

    def wait_for_record_in_logs_since(self, pod_id, record, start_time, wait_message=None, restart_timer_record=None):
        self.wait_for_one_of_records_in_logs_since(pod_id,
                                                   [record],
                                                   start_time,
                                                   wait_message,
                                                   [restart_timer_record] if restart_timer_record else None)

    def scale_stateful_set(self, stateful_set_name, replicas_number):
        log.info("Try to scale statefulset {} to {} replicas.".format(stateful_set_name, replicas_number))
        self.oc.scale(stateful_set_name, replicas_number, entity="statefulset")
        self.wait_replicas_statefulset(stateful_set_name, replicas_number)

    def get_liveness_probe_from_stateful_set(self, stateful_set_name):
        return self.oc.get_liveness_probe_from_stateful_set(stateful_set_name)

    def return_liveness_readiness_probes_for_stateful_set(self, stateful_set_name, probe):
        stateful_set = self.oc.get_stateful_set(stateful_set_name)
        stateful_set["spec"]["template"]["spec"]["containers"][0]["readinessProbe"] = probe
        stateful_set["spec"]["template"]["spec"]["containers"][0]["livenessProbe"] = probe
        self.oc.apply_object(stateful_set)