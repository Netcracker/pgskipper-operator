import logging
from m_utils import safe_get

logger = logging.getLogger("metric-collector")


def determine_role(pod):
    """
    Return role of pod. Role determined by pgtype label on pod
    :type pod dict
    """
    pod_labels = safe_get(pod, ["metadata", "labels"], {})
    if "pgtype" in pod_labels:
        return "master" if pod_labels["pgtype"] == "master" else "replica"
    return "replica"


def get_container_image_from_any_pod(pods_data):
    """
    Get first pod from list and tries to get image parameter from first container.
    Returns image or None
    """
    pod = safe_get(pods_data, ["items", 0], None)
    if pod:
        return safe_get(pod, ["spec", "containers", 0, "image"], None)
    return None


def get_env_value_from_pod(pod, name, default):
    """
    Get first pod from list and tries to get specified env variable.
    Returns variable value or default
    """
    return safe_get(
        [x for x in safe_get(pod, ["spec", "containers", 0, "env"], []) if safe_get(x, ["name"]) == name],
        [0, "value"], default)


def get_env_value_from_any_pod(pods_data, name, default):
    """
    Get first pod from list and tries to get specified env variable.
    Returns variable value or default
    """
    return get_env_value_from_pod(safe_get(pods_data, ["items", 0], {}), name, default)
