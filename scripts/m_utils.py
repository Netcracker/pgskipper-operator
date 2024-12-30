import logging
import os

import collector_utils

logger = logging.getLogger("metric-collector")


def safe_get(data, path, default=None):
    """
    This method tries to get element from map|array tree by provided path ["key", "second_key"].
    If element exists returns element otherwise return default.
    :type data dict
    :type data List
    :type path List
    :type default any
    """
    if data:
        current = data
        for path_element in path:
            if isinstance(path_element, (bytes, str)):
                if path_element in current:
                    current = current[path_element]
                else:
                    return default
            elif isinstance(path_element, int):
                if len(current) > path_element:
                    current = current[path_element]
                else:
                    return default
            else:
                return default
        return current
    else:
        return default


def deep_merge(target, update):
    for key in update:
        if key in target:
            if isinstance(target[key], dict) and isinstance(update[key], dict):
                deep_merge(target[key], update[key])
            elif target[key] != update[key]:
                raise Exception(
                    'Conflict occurred for {} and {}.'.format(target[key],
                                                              update[key]))
        else:
            target[key] = update[key]
    return target


def get_version_of_pgsql_server(conn_string):
    result = collector_utils.execute_query(conn_string, 'SHOW SERVER_VERSION;')
    return list(map(int, result.split(' ')[0].split('.')))


def close_connection(cursor, conn):
    # see http://initd.org/psycopg/docs/cursor.html#cursor.closed
    if cursor and not cursor.closed:
        try:
            cursor.close()
        except Exception as cursor_close_exc:
            logger.exception("Error while closing cursor")
    # see http://initd.org/psycopg/docs/connection.html#connection.closed
    if conn and conn.closed == 0:
        try:
            conn.close()
        except Exception as conn_close_exc:
            logger.exception("Error while closing connection")


def get_monitoring_user():
    return os.environ.get("MONITORING_USER", "monitoring_role")


def get_monitoring_password():
    return os.environ.get("MONITORING_PASSWORD", "monitoring_password")


def get_connection_to_pod(pod_ip):
    conn_string = f"host='{pod_ip}' dbname='postgres' " \
                  f"user='{get_monitoring_user()}' " \
                  f"password='{get_monitoring_password()}' " \
                  "connect_timeout=3 options='-c " \
                  "statement_timeout=3500'"
    return conn_string
