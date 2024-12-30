#!/usr/bin/env python

import argparse
import logging
import os
import threading
import time

from collector_utils import collect_generic_metrics_for_pod, enrich_master_metrics, execute_query, perform_smoketests, \
    get_common_pg_metrics_by_version, __collect_pg_metrics, prepare_node_metrics, get_leader_pod
from collector_utils import linearizeJson, get_influxdb_value, mapStatuses, getPodValue, storePodValue, collect_replication_data
from collector_utils import get_port, get_host, get_region
from m_utils import safe_get, get_monitoring_user, get_monitoring_password, get_version_of_pgsql_server
from oc_utils import get_pods_info, get_configmap, get_dc_info, get_number_of_replicas_from_statefulset, get_number_of_replicas_from_deployment, oc_login, get_deployment_info , unavailable_replicas_from_deployment
from pod_utils import get_env_value_from_any_pod
from kubernetes import config
from gpdb import GpdbCollector
# todo[anin] read actual value
from metric import convert_metrics, Metric, default_tags

DEFAULT_CLUSTER_PG_NODE_QTY = 2


def get_rto(pod_data):
    return get_env_value_from_any_pod(pod_data, "PATRONI_TTL", 60)


def get_rpo(pod_data):
    return get_env_value_from_any_pod(pod_data, "PATRONI_MAXIMUM_LAG_ON_FAILOVER", 1048576)


def get_latest_master_appearance():
    return getPodValue('master','last_master_appearance',None), getPodValue('master','last_master_xlog_location',0)


def store_last_master_appearance(master):
    storePodValue('master','last_master_appearance',time.time())
    storePodValue('master','last_master_xlog_location',max(getPodValue('master','last_master_xlog_location',0), safe_get(master, ["pg_metrics", "xlog_location"], 0)))


def collect_metrics(remote_host, remote_port,
                    oc_project, pgcluster):
    temp_metrics = {
        "collector": {"start": time.time()},
        "pg.{}".format(pgcluster): {
            "nodes": {},
            "cluster": {},
            "endpoints": {},
            "patroni_dcs": "kubernetes"
        },
        "status": 0
    }
    metrics = []
    collection_start_time = time.time()
    try:
        with open('/var/run/secrets/kubernetes.io/serviceaccount/token', 'r') as f:
            token = f.read()

        pod_data = get_pods_info(remote_host, remote_port, oc_project, token, "pgcluster", pgcluster)
        for pod in safe_get(pod_data, ["items"], []):
            phase = safe_get(pod, ["status", "phase"], [])
            deployment_info = safe_get(pod, ["status", "containerStatuses"], [])
            if len(deployment_info) == 0:
                logger.info("Skipping pod with info: {}".format(pod))
                continue
            logger.debug("Deployment information returned {}" .format(deployment_info))
            deployment_name = deployment_info[0].get('name',{})
            logger.debug("Deployment Name returned from array {}" .format(deployment_name))
            unavailable_replicas = unavailable_replicas_from_deployment(remote_host, remote_port, oc_project, token, deployment_name)
            logger.debug("unavailable replica count {}" .format(unavailable_replicas))
            if phase == "Running" and unavailable_replicas != 1:
                (pod_identity, pod_description) = collect_generic_metrics_for_pod(pod, shell_metrics_collector_executors)
                metrics.extend(prepare_node_metrics(pod_identity, pod_description))
                temp_metrics["pg.{}".format(pgcluster)]["nodes"][pod_identity] = pod_description
            else:
                pod_name = safe_get(pod, ["metadata", "name"], [])
                reason = safe_get(pod, ["status", "reason"], {})
                logger.info("skipping pod: {} message: {} reason: {}".format(pod_name, phase, reason))

        temp_metrics["pg.{}".format(pgcluster)]["patroni_dcs"] = "kubernetes"
        temp_metrics["pg.{}".format(pgcluster)]["cluster"] = __get_pg_cluster_status(pgcluster, pod_data, temp_metrics)
        __get_endpoints_status(pgcluster, safe_get(temp_metrics, ["pg.{}".format(pgcluster), "cluster", "active"]))
        temp_metrics["collector"]["status"] = "OK"

        # remove old nodes metrics format. It's used in __get_pg_cluster_status to prepare cluster metrics
        del temp_metrics["pg.{}".format(pgcluster)]["nodes"]

        status_code = temp_metrics["pg.{}".format(pgcluster)]["cluster"]["status"]
        # this is a mapping for common app health dashboard.
        # 1 -> UP; 0 -> Down, other codes are just warnings
        cluster_status_health_mapping = {0: 1, 10: 0}
        app_health_code = cluster_status_health_mapping.get(status_code, status_code)
        # just print this metric so output telegraf plugin will take it
        print("app_health value={0}".format(app_health_code))
    except Exception:
        logger.exception("Cannot collect data")
        temp_metrics["collector"]["status"] = "ERROR"
    temp_metrics["collector"]["duration"] = time.time() - collection_start_time
    return temp_metrics, metrics


def __is_xlog_location_actual(pod_identity, pod_metrics):
    last_master_xlog_location=getPodValue('master','last_master_xlog_location',0)
    pg_metrics = safe_get(pod_metrics, ["pg_metrics"])
    if safe_get(pg_metrics, ["running"], None) == 1:
        current_xlog_location = safe_get(pg_metrics, ["xlog_location"], 0)
        last_xlog_location = int(getPodValue(pod_identity, "xlog_location", current_xlog_location))
        storePodValue(pod_identity, "xlog_location", current_xlog_location)
        logger.debug("For {}: current_xlog_location={}, last_xlog_locatioqn={}, last_master_xlog_location={}"
                     .format(pod_identity, current_xlog_location, last_xlog_location, last_master_xlog_location))
        delta1 = current_xlog_location - last_xlog_location
        delta2 = current_xlog_location - last_master_xlog_location
        if delta2 < 0 and delta1 == 0:
            logger.debug("Node {} has zero progress for xlog_location while master is ahead of node. "
                         "Calculated metrics: delta1: {}, delta2: {}".format(pod_identity, delta1, delta2))
            return False
    return True


def __calculate_pg_cluster_status_for_missing_master(pod_data, nodes):
    """
    This method checks if cluster ready for auto failover without master.
    Cluster can be ready for failover, not ready but downtime less than RTO and not ready but downtime more than RTO
    :param pod_data:
    :param nodes:
    :return:
    """
    logger.warn("Master not found or in read only mode. Calculating cluster status.")
    error_status = "None"
    error_message = "None"
    error_description = "None"

    RTO = float(get_rto(pod_data))
    RPO = float(get_rpo(pod_data))
    logger.info("RTO: {}, RPO: {}".format(RTO, RPO))

    (lma, lmxl) = get_latest_master_appearance()
    max_xlog_location = 0 if len(nodes) == 0 else \
        max([safe_get(x, ["pg_metrics", "xlog_location"], 0) for x in nodes])
    if lma:
        time_since_master_disappear = time.time() - lma
        minimum_lag = lmxl - max_xlog_location
        logger.info("last_master_appearance: {}, last_master_xlog_location: {}, max_xlog_location: {}"
                    .format(lma, lmxl, max_xlog_location))
        if time_since_master_disappear < RTO and minimum_lag > RPO:
            error_status = "WARNING"
            error_message = "No replica to promote but service is down less than RTO"
            error_description = "Supposed action: check master state and restore if possible" \
                                "Stats: [ " \
                                "last_master_appearance: {}, " \
                                "last_master_xlog_location: {}, " \
                                "max_xlog_location: {} ]" \
                .format(lma, lmxl, max_xlog_location)
        if time_since_master_disappear > RTO and minimum_lag > RPO:
            error_status = "CRITICAL"
            error_message = "No replica to promote and service is down more than RTO"
            error_description = "Supposed action: try to restore master manually if possible or perform failover. " \
                                "Stats: [ " \
                                "last_master_appearance: {}, " \
                                "last_master_xlog_location: {}, " \
                                "max_xlog_location: {} ]" \
                .format(lma, lmxl, max_xlog_location)
    else:
        error_status = "CRITICAL"
        error_message = "Monitoring does not have record if master ever existed."
        error_description = "Supposed action: Check cluster state. Ignore this message if cluster is staring up" \
                            "Stats: [ " \
                            "last_master_appearance: {}, " \
                            "last_master_xlog_location: {}, " \
                            "max_xlog_location: {} ]" \
            .format(lma, lmxl, max_xlog_location)
    return error_status, error_message, error_description


def __get_pg_cluster_status(pgcluster, pod_data, temp_metrics):
    pod_identities = list(safe_get(temp_metrics, ["pg.{}".format(pgcluster), "nodes"], {}).keys())
    nodes = list(safe_get(temp_metrics, ["pg.{}".format(pgcluster), "nodes"], {}).values())
    logger.debug("Process data from pods - get working nodes count")
    working_nodes = len([x for x in nodes if safe_get(x, ["pg_metrics", "running"]) == 1])
    logger.debug("Process data from pods - collect info about master")
    masters = [x for x in nodes if safe_get(x, ["pod", "role"]) == "master"]
    replicas = [x for x in nodes if safe_get(x, ["pod", "role"]) == "replica"]
    replica_names = [x["pod"]["name"] for x in replicas]
    master_present = len(masters) > 0
    master = masters[0] if master_present else None
    logger.debug("Process master pod {}", master)
    master_name = safe_get(master, ["pod", "name"], default="")
    leader_role = get_leader_pod(pgcluster)
    standby_cluster = True if (safe_get(leader_role, ["name"], default="") == master_name) \
                              and (safe_get(leader_role, ["role"], default="") == "standby_leader") else False

    error_status = "None"
    error_message = "None"
    error_description = "None"
    if master:
        logger.debug("Master found. Starting smoketests")
        monitoring_user = get_monitoring_user()
        monitoring_password = get_monitoring_password()
        logger.info("Starting smoketests on behalf of {} user"
                    .format(monitoring_user))

        # todo[dmsh] add port
        conn_string = f"host='{get_host()}' dbname='postgres' " \
                      f"user='{monitoring_user}' " \
                      f"password='{monitoring_password}' " \
                      "connect_timeout=3 options='-c " \
                      "statement_timeout=3000'"

        enrich_master_metrics(conn_string, master, pgcluster, not standby_cluster, replica_names)
        store_last_master_appearance(master)
    else:
        error_status, error_message, error_description = __calculate_pg_cluster_status_for_missing_master(pod_data,
                                                                                                          nodes)

    actual_nodes = len([x for x in pod_identities if __is_xlog_location_actual(x, safe_get(temp_metrics,
                                                                                           ["pg.{}".format(pgcluster),
                                                                                            "nodes", x], {}))])
    master_writable = (safe_get(master, ["pg_metrics", "writable"], False))
    # check standby cluster settings here and proceed with OK to prevent metrics rewriting.
    # It's expected that standby_leader is not writable.
    if (master_writable or standby_cluster) and working_nodes == DEFAULT_CLUSTER_PG_NODE_QTY and actual_nodes == DEFAULT_CLUSTER_PG_NODE_QTY:
        cluster_state = "OK"
        cluster_status_code = 0
    elif master_writable or standby_cluster:
        cluster_state = "DEGRADED"
        cluster_status_code = 6
        if working_nodes != DEFAULT_CLUSTER_PG_NODE_QTY:
            error_status = "WARNING"
            error_message = "One or more replicas does not have running postgresql."
            error_description = "Supposed action: Check logs and follow troubleshooting guide."
        else:
            error_status = "WARNING"
            error_message = "One or more replicas cannot start replication."
            error_description = "Supposed action: Check logs and follow troubleshooting guide."
    else:
        cluster_state = "ERROR"
        cluster_status_code = 10
        if master:
            error_status, error_message, error_description = __calculate_pg_cluster_status_for_missing_master(pod_data,
                                                                                                              nodes)

    cluster_status = {
        "nodes": len(nodes),
        "working_nodes": working_nodes,
        "actual_nodes": actual_nodes,
        "master_writable": master_writable,
        "state": cluster_state,
        "status": cluster_status_code,
        "active": not standby_cluster,
        "error_status": error_status,
        "error_message": error_message,
        "error_description": error_description,
    }
    if safe_get(master, ["pod"]):
        cluster_status["master"] = safe_get(master, ["pod"])

    logger.debug("Cluster status: {}".format(cluster_status))
    return cluster_status


def __get_endpoints_status(pgcluster, active):
    logger.debug("Start endpoint check")

    # todo[anin] configuration point
    cluster_address = get_host()
    logger.debug("Start check for {}".format(cluster_address))
    cluster_metrics = []
    monitoring_user = get_monitoring_user()
    monitoring_password = get_monitoring_password()

    # todo[dmsh] add port parameter
    cluster_conn_string = \
        f"host='{cluster_address}' dbname='postgres' " \
        f"user='{monitoring_user}' " \
        f"password='{monitoring_password}' " \
        "connect_timeout=3 options='-c statement_timeout=3000'"

    cluster_endpoint_status = execute_query(cluster_conn_string, "select 1")
    if cluster_endpoint_status == 1:
        if active:
            logger.debug("Endpoint received connection. Starting smoke tests")
            cluster_metrics = perform_smoketests(cluster_conn_string)
        else:
            logger.debug("Endpoint received connection. Cluster is in standby mode. Skip smoke tests")

    else:
        logger.debug("Cannot connect to endpoint")
        _tags = default_tags.copy()
        cluster_metrics.append(Metric(metric_name="endpoints.cluster.smoketest.passed", tags=_tags, fields={"value": 0}))
        _tags.update({"action": "insert"})
        cluster_metrics.append(Metric(metric_name="endpoints.cluster.smoketest.check", tags=_tags, fields={"value": 0}))
        _tags.update({"action": "select"})
        cluster_metrics.append(Metric(metric_name="endpoints.cluster.smoketest.check", tags=_tags, fields={"value": 0}))
        _tags.update({"action": "update"})
        cluster_metrics.append(Metric(metric_name="endpoints.cluster.smoketest.check", tags=_tags, fields={"value": 0}))
        _tags.update({"action": "delete"})
        cluster_metrics.append(Metric(metric_name="endpoints.cluster.smoketest.check", tags=_tags, fields={"value": 0}))

    cluster_metrics.append(Metric(metric_name="endpoints.cluster.running", tags=default_tags, fields={"value": cluster_endpoint_status or 0}))
    cluster_metrics.append(Metric(metric_name="endpoints.cluster.url", tags=default_tags, fields={"value": "\"{}\"".format(cluster_address)}))

    convert_metrics(pgcluster, cluster_metrics)

    return cluster_metrics

def is_last_collection_expired(soft=False):
    last_collection_time=getPodValue('general','last_collection_time',None)
    return last_collection_time is None or time.time() - last_collection_time > (2 if soft else 1) * collection_timeout


def collect_metrics_sync():
    return collect_metrics(remote_host, remote_port, oc_project, pgcluster)


def async_metric_collector():
    logger.info("Started collector thread")
    last_collection_data = getPodValue('general', 'last_collection_data', None)
    last_collection_time = getPodValue('general', 'last_collection_time', None)
    while True:
        try:
            if is_last_collection_expired():
                with collection_lock:
                    if is_last_collection_expired():
                        try:
                            logger.info("Start metric collection")
                            last_collection_time = time.time()
                            storePodValue('general','last_collection_time',last_collection_time)
                            last_collection_data = collect_metrics_sync()
                            storePodValue('general','last_collection_data',last_collection_data)
                        except Exception as e:
                            logger.exception("Cannot collect metrics")
                            last_collection_data = "{}"
                            storePodValue('general', 'last_collection_data', last_collection_data)
                            last_collection_time = None
                            storePodValue('general', 'last_collection_time', last_collection_time)
            if last_collection_time:
                sleep_time = collection_timeout - (time.time() - last_collection_time)
                if sleep_time > 0:
                    logger.debug("Looks like metrics is up to date. Sleep {} sec".format(sleep_time))
                    time.sleep(sleep_time)
                else:
                    logger.debug("Looks like last check had taken too long. sleep_time is {} sec".format(sleep_time))
        except Exception as e:
            logger.exception("Something happen in main update loop")


def collect_gpdb_metrics(pgcluster):
    gpdb_collector = GpdbCollector(pgcluster)
    gpdb_collector.collect()
    return gpdb_collector.convert()


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='metric collector for PGSQL')
    parser.add_argument('--host', dest='host', default='localhost', help='address of openshift console')
    parser.add_argument('--port', dest='port', default='8443', help='port of openshift console')
    parser.add_argument('--project', dest='project', default=None, help='name of openshift project')
    parser.add_argument('--pgcluster', dest='pgcluster', default="common", help='name of pgcluster')
    parser.add_argument('--loglevel', dest='loglevel', default="info",
                        choices=["info", "debug", None], help='log level. info/debug')
    parser.add_argument('--shell-metrics-collector-executors', dest='shell_metrics_collector_executors', default=3,
                        help='Defines amount of subprocesses which can collect shell metrics')

    args = parser.parse_args()

    remote_host = args.host
    remote_port = int(args.port)
    oc_project = args.project
    pgcluster = args.pgcluster
    loglevel = args.loglevel
    shell_metrics_collector_executors = int(args.shell_metrics_collector_executors)

    logger_level = logging.INFO
    if loglevel and loglevel == "debug":
        logger_level = logging.DEBUG

    logging.basicConfig(
        filename='/proc/1/fd/1',
        filemode='w',
        level=logger_level,
        format='[%(asctime)s,%(msecs)03d][%(levelname)-5s][category=%(name)s]'
               '[pid=%(process)d] %(message)s',
        datefmt='%Y-%m-%dT%H:%M:%S'
    )
    logger = logging.getLogger("metric-collector")

    collection_timeout = 30
    collection_lock = threading.RLock()

    with open('/var/run/secrets/kubernetes.io/serviceaccount/token', 'r') as f:
        token = f.read()

    # load config from cluster (work only inside kubernetes)
    # for local debug purposes use:
    # oc_login(remote_host, remote_port, oc_project, token)
    config.load_incluster_config()
    metrics = []

    if pgcluster == "gpdb":
        logger.info("Type of the cluster is set to GPDB, "
                    "start to collect Greenplum DB metrics")
        data, metrics_list = collect_gpdb_metrics(pgcluster)
        metrics.extend(metrics_list)
    elif get_deployment_info(remote_host, remote_port, oc_project, token, "postgresql"):
            DEFAULT_CLUSTER_PG_NODE_QTY = get_number_of_replicas_from_deployment(remote_host, remote_port, oc_project,
                                                                         token, "postgresql")
            logger.info("setting DEFAULT_CLUSTER_PG_NODE_QTY as {}, as a number of replicas from Deployment"
                        .format(DEFAULT_CLUSTER_PG_NODE_QTY))
            data, metrics = collect_metrics_sync()  # get metrics here
    else:
        dc_data = get_dc_info(remote_host, remote_port, oc_project, token)
        dcs = safe_get(dc_data, ["items"], [])
        patroni_nodes_dcs = [x for x in dcs if
                             safe_get(x, ["metadata", "name"], "").startswith("pg-{}-node".format(pgcluster))]
        patroni_nodes_dcs_num = len(list(patroni_nodes_dcs))
        # check if dcs number not equals 0
        if patroni_nodes_dcs_num != 0:
            DEFAULT_CLUSTER_PG_NODE_QTY = patroni_nodes_dcs_num
            logger.info("setting DEFAULT_CLUSTER_PG_NODE_QTY as {}, as a number of DCs".format(DEFAULT_CLUSTER_PG_NODE_QTY))
        else:
            # if dcs are not exists, try to collect number of replicas from statefulset
            DEFAULT_CLUSTER_PG_NODE_QTY = get_number_of_replicas_from_statefulset(remote_host, remote_port, oc_project,
                                                                                  token, "patroni")
            logger.info("setting DEFAULT_CLUSTER_PG_NODE_QTY as {}, as a number of replicas from Statefulset"
                        .format(DEFAULT_CLUSTER_PG_NODE_QTY))
        data, node_metrics = collect_metrics_sync()
        metrics.extend(node_metrics)

    tags = default_tags.copy()

    points = linearizeJson(data)
    points = mapStatuses(points)

    tag = ",".join(["{}={}".format(t, tags[t]) for t in tags])

    convert_metrics("patroni", metrics)

    for key in sorted(points):
        value = get_influxdb_value(key, points[key])
        print("ma.{},{} value={}".format(key, tag, value))
        if loglevel == "debug":
            logger.info(("ma.{},{} value={}".format(key, tag, value)))

else:
    raise Exception("Please run as main script. For more details about script parameters try --help option.")
