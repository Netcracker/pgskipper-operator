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

import re
import subprocess
import time
import logging
import psycopg2
import requests

from utils import get_log_level, get_host_ip, execute_shell_command

logging.basicConfig(
    level=get_log_level(),
    format='[%(asctime)s][%(levelname)-5s][category=%(name)s] %(message)s',
    datefmt='%Y-%m-%dT%H:%M:%S'
)
logger = logging.getLogger(__name__)


def get_context_data(setting_name):
    conn = None
    cursor = None
    conn_string = "host='localhost' dbname='postgres' user='postgres' " \
                  "connect_timeout=3 options='-c statement_timeout=3000'"
    try:
        conn = psycopg2.connect(conn_string)
        cursor = conn.cursor()
        cursor.execute("select context from pg_settings where name=%(sname)s",
                       {"sname": setting_name})
        row = cursor.fetchone()
        if row:
            return row[0]
        else:
            return None
    except psycopg2.OperationalError:
        return None
    finally:
        close_connection(cursor, conn)
    # return map(lambda x: x.strip(), subprocess.check_output(
    #     "psql -U postgres -t -c \"select setting, unit, category, vartype from pg_settings where name='{}'\"".format(setting_name), shell=True).split("|"))


def get_settings_data(setting_name):
    conn = None
    cursor = None
    conn_string = "host='localhost' dbname='postgres' user='postgres' " \
                  "connect_timeout=3 options='-c statement_timeout=3000'"
    try:
        conn = psycopg2.connect(conn_string)
        cursor = conn.cursor()
        logger.info(setting_name)
        cursor.execute("select current_setting (%(sname)s, 't')",
                       {"sname": setting_name})
        row = cursor.fetchone()
        if row:
            return row[0]
        else:
            return None
    except psycopg2.OperationalError:
        return None
    finally:
        close_connection(cursor, conn)


def is_restart_pending():
    conn = None
    cursor = None
    conn_string = "host='localhost' dbname='postgres' user='postgres' " \
                  "connect_timeout=3 options='-c statement_timeout=3000'"
    try:
        conn = psycopg2.connect(conn_string)
        cursor = conn.cursor()
        cursor.execute("select count(*) from pg_settings where pending_restart = TRUE")
        row = cursor.fetchone()
        return int(row[0]) > 0
    except Exception as e:
        logger.exception("Cannot get amount of parameters which requires restart")
        raise e
    finally:
        close_connection(cursor, conn)


def schedule_restart():
    logger.debug("Schedule restart")
    restart_command = "patronictl -c /patroni/pg_node.yml restart $PG_CLUST_NAME $(hostname) --force"
#    res = execute_shell_command(restart_command)
#    logger.debug(res)
    return execute_shell_command(restart_command)


def close_connection(cursor, conn):
    # see http://initd.org/psycopg/docs/cursor.html#cursor.closed
    if cursor and not cursor.closed:
        cursor.close()
    # see http://initd.org/psycopg/docs/connection.html#connection.closed
    if conn and conn.closed == 0:
        conn.close()


def is_values_diff(value, db_value):
    logger.debug("Start comparison for value: {}, db_value: {}"
                 .format(value, db_value))
    return value != db_value


def patroni_restart_state(basic_auth, iterations=5, sleep=3):
    for i in range(iterations):
        time.sleep(sleep)
        r = requests.get("http://{}:8008".format(get_host_ip()),
                         auth=basic_auth)
        logger.info("Checking restart state... It is {}".format(r.json().get('pending_restart', False)))
        restart_required = r.json().get('pending_restart', False)
        if restart_required:
            break
    return restart_required