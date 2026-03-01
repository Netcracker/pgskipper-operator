# Minimal PlatformLibrary stub for local testing
# This wraps kubernetes-client to provide the interface expected by pgsLibrary.py

from kubernetes import client, config
from kubernetes.stream import stream
import logging

log = logging.getLogger(__name__)

class PlatformLibrary:
    def __init__(self, managed_by_operator=None):
        try:
            # Try to load in-cluster config first
            config.load_incluster_config()
        except Exception:
            # Fall back to kubeconfig
            try:
                config.load_kube_config()
            except Exception as e:
                log.warning(f"Could not load kubernetes config: {e}")

        self.core_api = client.CoreV1Api()
        self.apps_api = client.AppsV1Api()
        self.managed_by_operator = managed_by_operator

    def get_pods(self, namespace, **kwargs):
        """Get pods in a namespace"""
        label_selector = kwargs.get('label_selector', '')
        # Note: managed_by_operator is stored but not automatically applied as a filter
        # The real PlatformLibrary likely handles this differently, or it's used elsewhere

        pods = self.core_api.list_namespaced_pod(namespace, label_selector=label_selector if label_selector else None)
        return pods.items

    def execute_command_in_pod(self, pod_name, namespace, command):
        """Execute a command in a pod"""
        try:
            if isinstance(command, str):
                command = ['/bin/sh', '-c', command]

            resp = stream(self.core_api.connect_get_namespaced_pod_exec,
                         pod_name, namespace,
                         command=command,
                         stderr=True, stdin=False,
                         stdout=True, tty=False)
            return resp, None
        except Exception as e:
            return None, str(e)

    def get_config_map(self, name, namespace):
        """Get a ConfigMap"""
        return self.core_api.read_namespaced_config_map(name, namespace)

    def get_secret(self, name, namespace):
        """Get a Secret"""
        return self.core_api.read_namespaced_secret(name, namespace)

    def get_deployment_entity(self, name, namespace):
        """Get a Deployment"""
        return self.apps_api.read_namespaced_deployment(name, namespace)

    def get_deployment_entities(self, namespace):
        """Get all Deployments in a namespace"""
        deployments = self.apps_api.list_namespaced_deployment(namespace)
        return deployments.items

    def get_replica_number(self, name, namespace):
        """Get replica count for a deployment"""
        deployment = self.apps_api.read_namespaced_deployment(name, namespace)
        return deployment.spec.replicas

    def set_replicas_for_deployment_entity(self, name, namespace, replicas):
        """Set replica count for a deployment"""
        body = {'spec': {'replicas': replicas}}
        self.apps_api.patch_namespaced_deployment_scale(name, namespace, body)

    def delete_pod_by_pod_name(self, pod_name, namespace, grace_period=0):
        """Delete a pod"""
        self.core_api.delete_namespaced_pod(pod_name, namespace,
                                           grace_period_seconds=grace_period)

    def get_replica_set(self, name, namespace):
        """Get a ReplicaSet"""
        return self.apps_api.read_namespaced_replica_set(name, namespace)

    def get_stateful_set(self, name, namespace):
        """Get a StatefulSet"""
        return self.apps_api.read_namespaced_stateful_set(name, namespace)

    def scale_down_stateful_set(self, name, namespace):
        """Scale down a StatefulSet to 0"""
        self.set_replicas_for_stateful_set(name, namespace, 0)

    def set_replicas_for_stateful_set(self, name, namespace, replicas):
        """Set replica count for a StatefulSet"""
        body = {'spec': {'replicas': replicas}}
        self.apps_api.patch_namespaced_stateful_set_scale(name, namespace, body)

    def check_service_of_stateful_sets_is_scaled(self, stateful_set_names, namespace,
                                                  direction='down', timeout=60):
        """Check if StatefulSets are scaled in a direction"""
        # Simplified implementation
        import time
        start = time.time()
        while time.time() - start < timeout:
            all_scaled = True
            for name in stateful_set_names:
                ss = self.get_stateful_set(name, namespace)
                if direction == 'down' and ss.spec.replicas > 0:
                    all_scaled = False
                elif direction == 'up' and ss.spec.replicas == 0:
                    all_scaled = False
            if all_scaled:
                return True
            time.sleep(2)
        return False

    def get_resource_image(self, resource_type, name, namespace, container_name=None):
        """Get container image for a resource"""
        if resource_type.lower() == 'deployment':
            resource = self.get_deployment_entity(name, namespace)
        elif resource_type.lower() == 'statefulset':
            resource = self.get_stateful_set(name, namespace)
        else:
            return None

        containers = resource.spec.template.spec.containers
        if container_name:
            for container in containers:
                if container.name == container_name:
                    return container.image
        elif len(containers) > 0:
            return containers[0].image
        return None
