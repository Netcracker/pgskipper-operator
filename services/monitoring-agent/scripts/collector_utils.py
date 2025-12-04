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

import decimal
import logging
import os
import subprocess
import time
import traceback
import uuid
import json
from multiprocessing.pool import ThreadPool

import psycopg2

from m_utils import safe_get, close_connection, get_version_of_pgsql_server, \
    get_connection_to_pod
from metrics_query_utils import get_metrics_type_by_version, \
    get_common_pg_metrics_by_version
from pod_utils import determine_role, get_env_value_from_pod

from kubernetes.stream import stream
from kubernetes.client.apis import core_v1_api


import pickle

from metric import Metric, default_tags

DEFAULT_STATUS_MAPPING = dict(up=0, successful=0, ok=0, warning=3, warn=3, planned=4, inprogress=5, problem=6, crit=10, fatal=10, failed=10, down=14, unknown=-1)

tmp_dir = '/tmp'

logger = logging.getLogger("metric-collector")

shell_pg_metrics = {
    "check": "echo 1",
    "df": "df /var/lib/pgsql/data",
    # "df.pcent":     "df --output=pcent /var/lib/pgsql/data | grep -v 'Use' | cut -d '%' -f1",
    # "df.avail":     "df --output=avail /var/lib/pgsql/data | grep -v 'Avail'",
    "du": "du /var/lib/pgsql/data/ --max-depth=2 --exclude=/var/lib/pgsql/data/lost+found"
    # "mem.rss":      "ps -u postgres -o pid,rss:8 | awk 'NR>1 {A+=$2} END{print A}'",
    # "loadavg":      "cat /proc/loadavg | cut -d\  -f1"
}


def spm_du_postprocessor(pod_info, result, version):
    """
    :param pod_info:
    :type pod_info: dict
    :param result:
    :type result: string
    :param version: pgsql version
    :type version: string
    :return:
    """
    pod_identity = get_env_value_from_pod(pod_info, "POD_IDENTITY", "node")
    lines = list([p for p in result.decode().split("\n") if p.strip()])
    sizes = dict(reversed(k.split("\t")) for k in lines)
    total = int(sizes["/var/lib/pgsql/data/"])
    pg_xlog = int(sizes[get_metrics_type_by_version("pg_xlog", version).format(pod_identity)])
    base = int(sizes["/var/lib/pgsql/data/postgresql_{}/base".format(pod_identity)])
    other = total - pg_xlog - base
    return {"base": base, "pg_xlog": pg_xlog, "other": other}


def spm_df_postprocessor(pod_info, result, version):
    """
    Filesystem     1K-blocks     Used Available Use% Mounted on
    /dev/vda1       83872132 65756268  18115864  79% /var/lib/pgsql/data
    :param pod_info:
    :type pod_info: dict
    :param result:
    :type result: string
    :param version: pgsql version
    :type version: string
    :return:
    """
    data = result.decode().split('\n')[1]
    metrics = data.split()
    return {"pcent": int(metrics[4][:-1]), "avail": int(metrics[3])}


shell_pg_metrics_postprocessors = {
    "du": spm_du_postprocessor,
    "df": spm_df_postprocessor,
}


def collect_generic_metrics_for_pod(pod, shell_metrics_collector_executors=3):
    """
    Collect metrics from postgres and through shell
    Returns dict with pod metrics {"pod": {}, "pg_metrics": {}, "metrics": {}}.
    Metrics determined by common_pg_metrics and shell_pg_metrics lists.
    :type pod dict
    :type shell_metrics_collector_executors int
    """
    pod_id = safe_get(pod, ["metadata", "name"], None)
    pod_ip = safe_get(pod, ["status", "podIP"], "None")
    logger.debug("Start metric collection for {} [{}]".format(pod_id, pod_ip))

    pod_identity = get_env_value_from_pod(pod, "POD_IDENTITY", "node")

    if pod_identity == "node":
        if pod_id.startswith('postgresql'):
            pod_identity = "node1"
        else:
            pod_identity = "node" + str(int(pod_id.strip()[-1]) + 1)

    logger.info("pod_identity: {}".format(pod_identity))

    conn_string = get_connection_to_pod(pod_ip)

    pod_description = {
        "pod": {
            "name": pod_id,
            "ip": pod_ip,
            "role": determine_role(pod),
            "status": safe_get(pod, ["status", "phase"], "None"),
            "startedAt": safe_get(pod, ["status", "startTime"], "None")
        }
    }

    # Calculates Patroni Member status by annotation on pod. It is temporary metric to identify problem with incrorrect Patroni status during DR managemnt opertaions
    try:
        state = ''
        status = safe_get(pod, ["metadata", "annotations", "status"])
        if not status:
            # Get status from containerStatuses if annotations doesn't contain status
            status = safe_get(pod, ["status", "containerStatuses"])
            logger.debug("pod status: {}".format(status))
            for k in status[0].get('state', {}):
                state = k
        else:
            json_status = json.loads(status)
            state = json_status['state']

        logger.debug("state: {}".format(state))
        if state == "running":
            pod_description["patroni_status"] = 1
        else:
            pod_description["patroni_status"] = 0
    except Exception:
        pod_description["patroni_status"] = 0
        logging.exception("Cannot get Patroni status from {}".format(pod_id))

    version = None
    try:
        version = get_version_of_pgsql_server(conn_string)
    except Exception:
        logging.exception("Cannot get version from {}".format(pod_id))

    if version:
        appropriate_pg_metrics = get_common_pg_metrics_by_version(version)
        pod_description["pg_metrics"] = __collect_pg_metrics(conn_string, appropriate_pg_metrics, pod_identity)
        pod_description["metrics"] = __collect_shell_metrics(pod_id, pod, shell_pg_metrics,
                                                             shell_pg_metrics_postprocessors,
                                                             version, shell_metrics_collector_executors)

    logger.debug("Collected generic metrics: {}".format(pod_description))
    return pod_identity, pod_description


def perform_smoketests(conn_string):
    logger.debug("Start smoketests")
    conn = None
    cursor = None
    smoketest = []
    try:
        conn = psycopg2.connect(conn_string)

        # prepare table for smoketests
        cursor = conn.cursor()
        cursor.execute("create table if not exists monitor_test (id int primary key not null, value text not null);")
        cursor.execute("SELECT to_regclass('monitor_test_seq');")
        monitor_test_seq_exist = cursor.fetchone()[0]

        if not monitor_test_seq_exist:
            logger.info("creating missing sequence for tests")
            cursor.execute("create sequence monitor_test_seq start 10001;")
        cursor.execute("SELECT nextval('monitor_test_seq');")

        new_id = int(cursor.fetchone()[0])
        # in case if we reach maximum for pg type integer
        # (~ 300 years for 10 sec cycle x2 checks)
        # see https://www.postgresql.org/docs/9.6/static/datatype-numeric.html
        if new_id == 2147483647:
            cursor.execute("delete from monitor_test;")
            cursor.execute("SELECT setval(monitor_test_seq, 10001);")
            new_id = int(cursor.fetchone()[0])
        conn.commit()
        cursor.close()
        conn.close()

        new_uuid = str(uuid.uuid4()).lower()
        second_uuid = str(uuid.uuid4()).lower()

        insert_result, exec_time, output = __perform_single_smoketest(conn_string,
                                   "insert into monitor_test values ({}, '{}')".format(new_id, new_uuid),
                                   "insert 0 1", "insert", smoketest)

        if insert_result == 1:
            select_result, exec_time, output = __perform_single_smoketest(conn_string,
                                       "select * from monitor_test where id={}".format(new_id),
                                       "select 1", "select", smoketest)
            update_result, exec_time, output = __perform_single_smoketest(conn_string,
                                       "update monitor_test set value='{}' where id={}".format(second_uuid, new_id),
                                       'update 1', "update", smoketest)
            delete_result, exec_time, output = __perform_single_smoketest(conn_string,
                                       "delete from monitor_test where id={}".format(new_id),
                                       'delete 1', "delete", smoketest)

        if insert_result == 1 and select_result == 1 and update_result == 1 and delete_result == 1:
            passed = 1
        else:
            passed = 0
        smoketest.append(Metric(metric_name="endpoints.cluster.smoketest.passed", tags=default_tags.copy(), fields={"value": passed}))

    except Exception:
        logger.exception("Cannot collect metrics. Most likely cannot connect to postgres")
    finally:
        close_connection(cursor, conn)

    return smoketest


def collect_replication_lag(pgcluster):
    import requests
    response = requests.get("http://pg-{}-api:8008/cluster".format(pgcluster))
    if response:
        json_response = response.json()
        for member in json_response["members"]:
            if member["role"] in {"leader", "standby_leader"}:
                continue
            if member["state"] == "running":
                tags = {"namespace": os.environ['NAMESPACE'], "hostname": member["name"]}
                tag = ",".join(["{}={}".format(t, tags[t]) for t in tags])
                print("ma.pg.patroni.replication_lag,{0} value={1}".format(tag, member["lag"]))
    else:
        logger.warning("were not able to collect replication lag from patroni", response.text)


def get_leader_pod(pgcluster):
    import requests
    response = requests.get("http://pg-{}-api:8008/cluster".format(pgcluster))
    if response:
        json_response = response.json()
        for member in json_response["members"]:
            if member["role"] in {"leader", "standby_leader"}:
                return member
    else:
        logger.warning("Can not find leader of patroni cluster", response.text)


def collect_replication_data(conn_string):
    logger.debug("Collect data about replication")
    conn = None
    cursor = None
    replication = {}
    try:
        conn = psycopg2.connect(conn_string)

        # get pgsql server version
        version = None
        try:
            version = get_version_of_pgsql_server(conn_string)
        except Exception:
            logging.exception("Cannot get version")
        if version:
            # collect replication info
            cursor = conn.cursor()
            cursor.execute(get_metrics_type_by_version("replication_data", version))
            for row in cursor:
                replication[row[1].replace(" ", "_")] = {
                    "usename": row[0] if row[0] else "empty",
                    "client_addr": row[2] if row[2] else "empty",
                    "state": row[3] if row[3] else "empty",
                    "sync_priority": row[4] if row[4] else 0,
                    "sync_state": row[5] if row[5] else "empty",
                    "sent_replay_lag": row[6] if row[6] else 0,
                    "sent_lag": row[7] if row[7] else 0,
                }
    except Exception:
        logger.exception("Cannot collect metrics. Most likely cannot connect to postgres")
    finally:
        close_connection(cursor, conn)
    return replication


def collect_archive_data(conn_string):
    logger.debug("Collect data about wal archive")
    conn = None
    cursor = None
    try:
        conn = psycopg2.connect(conn_string)
        cursor = conn.cursor()
        cursor.execute("""SELECT 
(select setting from pg_settings where name='archive_mode'), 
EXTRACT(EPOCH FROM (now()-last_archived_time)), 
archived_count, 
failed_count FROM pg_stat_archiver""")
        for row in cursor:
            result = {
                "mode": row[0],
                # this is a for backward compatibility
                "mode_prom": 0 if row[0] == "off" else 1
            }
            if row[0] == "on":
                if row[1]:  # assuming we just started and no record was archived yet
                    result["delay"] = row[1]
                result["archived_count"] = row[2]
                result["failed_count"] = row[3]
            return result
    except Exception:
        logger.exception("Cannot collect metrics. Most likely cannot connect to postgres")
    finally:
        close_connection(cursor, conn)


def enrich_master_metrics(conn_string, master, pgcluster, active, replica_names):
    """
    Performs smoketests on master and calculates writable attribute.
    Writes result in master
    """
    logger.debug("Enrich metrics for master {}".format(master))
    if safe_get(master, ["pg_metrics", "running"]) == 1:
        smoke_test = []
        if active:
            logger.debug("Starting smoke tests")
            smoke_test = perform_smoketests(conn_string)
        else:
            logger.debug("Cluster is in standby mode. Skip smoke tests")
        master["pg_metrics"]["replication"] = collect_replication_data(conn_string)
        master["pg_metrics"]["archive"] = collect_archive_data(conn_string)
    else:
        smoke_test = [(Metric(metric_name="endpoints.cluster.smoketest.passed", tags=default_tags.copy(), fields={"value": 0}))]
        master["pg_metrics"]["replication"] = []
        master["pg_metrics"]["archive"] = {}
    passed = 0
    for metric in smoke_test:
        if metric.metric_name == "endpoints.cluster.smoketest.passed":
            passed = metric.get_field_value("value", 0)
    master["pg_metrics"]["writable"] = True if passed is 1 else False

    enrich_prometheus_metrics(master, replica_names)
    collect_replication_lag(pgcluster)


def enrich_prometheus_metrics(master_metrics, replica_names):
    replication_state = master_metrics["pg_metrics"]["replication"]
    for key, value in replication_state.items():
        sent_lag = value.get("sent_lag", 0)
        sent_replay_lag = value.get("sent_replay_lag", 0)
        tags = {
            "namespace": os.environ['NAMESPACE'],
            "hostname": key,
        }
        tag = ",".join(["{}={}".format(t, tags[t]) for t in tags])
        print("ma.pg.patroni.replication_state.sent_lag,{0} value={1}".format(tag, sent_lag))
        print("ma.pg.patroni.replication_state.sent_replay_lag,{0} value={1}".format(tag, sent_replay_lag))

    if os.getenv("SITE_MANAGER") == "on":
        sm_replication_state = 1
        tags = {
            "namespace": os.environ['NAMESPACE'],
        }
        tag = ",".join(["{}={}".format(t, tags[t]) for t in tags])
        for name in replica_names:
            if name in replication_state and len(replica_names) == len(replication_state):
                sm_replication_state = 0
            print("ma.pg.patroni.replication_state.sm_replication_state,{0} value={1}".format(tag, sm_replication_state))

    archive = master_metrics["pg_metrics"]["archive"]
    points = linearizeJson(archive)
    tags = default_tags.copy()
    tag = ",".join(["{}={}".format(t, tags[t]) for t in tags])
    for key in points:
        value = get_influxdb_value(key, points[key])
        print("ma.pg.patroni.pg_metrics_archive.{0},{1} value={2}".format(key, tag, value))


def __collect_single_pg_metric(conn_string, query):
    conn = None
    cursor = None
    try:
        conn = psycopg2.connect(conn_string)
        cursor = conn.cursor()
        logger.debug("Executing query {}".format(query))
        cursor.execute(query)
        return cursor.fetchone()[0]
    finally:
        close_connection(cursor, conn)


def collect_pg_metrics(conn_string, requests):
    pg_metrics = {}
    pool = ThreadPool(processes=len(requests))
    tasks = {}
    for (key, value) in list(requests.items()):
        tasks[key] = pool.apply_async(__collect_single_pg_metric, (conn_string, value))

    for (key, value) in list(tasks.items()):
        try:
            result = value.get(timeout=5)
            pg_metrics[key] = result
        except Exception as e:
            logger.exception('Cannot collect metric  "{}"'.format(key))

    pool.close()
    return pg_metrics


def __collect_pg_metrics(conn_string, requests, pod_identity):
    """
    This method executes SELECT requests defined in requests dict and returns dict with results
    :type conn_string str
    :type requests dict
    :rtype dict
    """
    start_time = time.time()
    pg_metrics = collect_pg_metrics(conn_string, requests)

    pg_metrics["xact_sum"] = safe_get(pg_metrics, ["xact_sum"], 0)
    pg_metrics["replication_slots"] = __collect_replication_slots_data(conn_string)

    logger.debug("Pg metrics loading time: {}".format((time.time() - start_time)))
    return pg_metrics


def __collect_replication_slots_data(conn_string):
    logger.debug("Collect data about replication slots")
    conn = None
    cursor = None
    replication = {}
    try:
        conn = psycopg2.connect(conn_string)

        # get pgsql server version
        version = None
        try:
            version = get_version_of_pgsql_server(conn_string)
        except Exception:
            logging.exception("Cannot get version")
        if version:
            logger.debug("Collecting replication slots data for pgsql server: %s" % version)
            # collect replication info
            cursor = conn.cursor()
            cursor.execute(get_metrics_type_by_version("replication_slots", version))
            for row in cursor:
                replication[row[0].replace(" ", "_")] = {
                    "restart_lsn_lag": row[1] if row[1] else 0,
                    "confirmed_flush_lsn_lag": row[2] if row[2] else 0
                }
    except Exception:
        logger.exception("Cannot collect metrics. Most likely cannot connect to postgres")
    finally:
        close_connection(cursor, conn)
    return replication


def execute_query(conn_string, query, collect_data=True):
    conn = None
    cursor = None
    try:
        conn = psycopg2.connect(conn_string)
        conn.autocommit = True
        cursor = conn.cursor()

        try:
            cursor.execute(query)
            if collect_data:
                data = cursor.fetchone()
                return data[0] if data else None
        except Exception as e:
            logger.exception('Cannot check execute  "{}"'.format(query))
    except Exception:
        logger.exception("Cannot execute query {}. Most likely cannot connect to postgres".format(query))
    finally:
        close_connection(cursor, conn)
    return None


def exec_in_pod (name, command):
    api = core_v1_api.CoreV1Api()
    resp = stream(api.connect_get_namespaced_pod_exec, name, os.environ.get('NAMESPACE'),
                  command=command.split(" "),
                  stderr=True, stdin=False,
                  stdout=True, tty=False)
    return resp


def __collect_single_shell_metric(pod_id, pod_info, command, pg_version, postprocessor):
    oc_exec_timeout = int(os.environ.get('METRIC_COLLECTOR_OC_EXEC_TIMEOUT', 10))
    logger.debug("Executing command {}".format(command))
    try:
        result = exec_in_pod(pod_id, command)
        command_result = result.encode()

        if postprocessor:
            return postprocessor(pod_info, command_result, pg_version)
        else:
            try:
                return float(command_result)
            except ValueError:
                return command_result
    except subprocess.TimeoutExpired:
        logger.exception("Timeout exception raised for cmd: {}".format(command))


def __collect_shell_metrics(pod_id, pod_info, metrics, postprocessors, pg_version, executors=3):
    """
    This method executes SELECT requests defined in requests dict and returns dict with results
    :type pod_id str
    :type metrics dict
    :rtype dict
    """
    start_time = time.time()
    shell_metrics = {}

    pool = ThreadPool(processes=executors)
    tasks = {}
    for (key, value) in list(metrics.items()):
        tasks[key] = pool.apply_async(__collect_single_shell_metric,
                                      (pod_id, pod_info, value, pg_version, postprocessors.get(key)))

    for (key, value) in list(tasks.items()):
        try:
            shell_metrics[key] = value.get(timeout=int(os.environ.get('METRIC_COLLECTOR_OC_EXEC_TIMEOUT', 5)))
        except Exception as e:
            logger.exception('Cannot collect metric  "{}"'.format(key))

    pool.close()

    logger.debug("Shell metrics loading time: {}".format((time.time() - start_time)))
    return shell_metrics


def __perform_single_smoketest(conn_string, query, expected_status, prefix, smoketests):
    """
    Performs query and checks if status message contains required message
    """
    logger.debug("Execute '{}'".format(query))
    exec_time = -1
    result = 0
    output = None
    conn = None
    cursor = None
    try:
        conn = psycopg2.connect(conn_string)
        cursor = conn.cursor()
        # todo[anin] collect time from pg itself
        start_time = time.time()
        cursor.execute(query)
        cc = cursor.statusmessage
        exec_time = 1000 * (time.time() - start_time)
        if expected_status in cc.lower():
            result = 1
            output = cc
        else:
            result = -1

        conn.commit()
    except Exception:
        logger.exception("Exception during smoketest execution")
        output = traceback.format_exc()
        result = -1
    finally:
        close_connection(cursor, conn)
    tags = default_tags.copy()
    tags.update({"action": prefix})

    smoketests.append(Metric(metric_name="endpoints.cluster.smoketest.check", tags=tags, fields={"value": result}))
    smoketests.append(Metric(metric_name="endpoints.cluster.smoketest.time_ms", tags=tags, fields={"value": exec_time}))
    if result != 1:
        smoketests.append(Metric(metric_name="endpoints.cluster.smoketest.output", tags=tags, fields={"value": output}))
    return result, exec_time, output


def linearizeJson(obj):
    def __walk(obj, prefix, res):
        if isinstance(obj, dict):
            for key in obj:
                propKey = key if prefix is None else "%s.%s" % (prefix, key)
                __walk(obj[key], propKey, res)

        elif isinstance(obj, list):
            for i, item in enumerate(obj):
                propKey = str(i) if prefix is None else "%s.%d" % (prefix, i)
                __walk(item, propKey, res)

        else:
            res[prefix] = obj

    res = {}
    __walk(obj, None, res)
    return res


def get_influxdb_value(key, value):
    if type(value) == int:
        rvalue = value
    elif type(value) == bool:
        if value == True:
            rvalue = 1
        else:
            rvalue = 0
    elif type(value) == float:
        rvalue = value
    elif type(value) == decimal.Decimal:
        rvalue = value
    elif not value:
        rvalue = ""
    elif value == "None":
        rvalue = '"None"'
    else:
        # some time type function detect number as string. Why? i don't know
        try:
            val = int(value)
            rvalue = val
        except ValueError:
            rvalue = '"' + str(value) + '"'
    # fix for existing cloud db metric
    # if key in ['storage.size', 'storage.archive_size', 'storage.last.metrics.size',
    #            'storage.lastSuccessful.metrics.size']:
    #     rvalue = '"' + str(value) + '"'
    # fix in case of last backup is failed,
    # skipping exception metric because its not used in dashboard
    if key == "storage.last.metrics.exception":
        rvalue = "\"Exception happend during backup\""
    return rvalue


def mapStatuses(obj, mapping=DEFAULT_STATUS_MAPPING):
    res = {}
    for key in obj:
        if (key == 'status' or key.endswith('.status')) and isinstance(obj[key], (bytes, str)):
            if obj[key].lower() in mapping:
                res[key] = mapping[obj[key].lower().replace(" ","")]
            else:
                logger.debug("Unknown status for mapping: %s" % obj[key])
                res[key] = DEFAULT_STATUS_MAPPING['unknown']
        else:
            res[key] = obj[key]

    return res


def storePodValue(pod, key, value):
    global tmp_dir
    fh = open("{}/{}.{}.tmp".format(tmp_dir, pod, key), 'wb')
    pickle.dump(value, fh)


def getPodValue(pod, key, default=None):
    global tmp_dir
    filename = "{}/{}.{}.tmp".format(tmp_dir, pod, key)
    if os.path.exists(filename):
        fh = open(filename, 'rb')
        value=pickle.load(fh)
    else:
        return default
    return value


def get_host():
    cluster_name = os.getenv('PGCLUSTER', 'patroni')
    namespace = os.getenv('NAMESPACE', 'postgres-service')
    host = os.getenv('POSTGRES_HOST', f'pg-{cluster_name}.{namespace}')
    return host


def get_port():
    return int(os.getenv('POSTGRES_PORT', 5432))


def get_region():
    return os.getenv('REGION', '')


def prepare_node_metrics(pod_identity, pod_description):
    node_metrics = []
    tags = default_tags.copy()
    tags.update({"pg_node": pod_identity})

    points = linearizeJson(pod_description)
    for key in points:
        value = get_influxdb_value(key, points[key])
        node_metrics.append(Metric(metric_name=key, tags=tags, fields={"value": value}))
    return node_metrics
