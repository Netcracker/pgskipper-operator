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
import psycopg2
import logging

log = logging.getLogger("utils")


def execute_query(conn_properties, query):
    conn = None
    try:
        conn = psycopg2.connect(**conn_properties)
        with conn.cursor() as cur:
            cur.execute(query)
            return cur.fetchone()[0]
    finally:
        if conn:
            conn.close()

def get_version_of_pgsql_server():
    # this is very bad, need to reuse code from granular backups
    conn_properties = {
        'host': os.getenv('POSTGRES_HOST'),
        'port': os.getenv('POSTGRES_PORT'),
        'user': os.getenv('POSTGRES_USER') or 'postgres',
        'password': os.getenv('POSTGRES_PASSWORD'),
        'database': 'postgres',
        'connect_timeout': int(os.getenv("CONNECT_TIMEOUT", "5")),
    }
    try:
        server_version = execute_query(conn_properties, 'SHOW SERVER_VERSION;')
    except psycopg2.OperationalError as e:
        log.exception(e)
        return None
    version_as_list = list(map(int, server_version.split(' ')[0].split('.')))
    if [10, 0] <= version_as_list < [11, 0]:
        return "pg10"
    elif [11, 0] <= version_as_list < [12, 0]:
        return "pg11"
    elif [12, 0] <= version_as_list < [13, 0]:
        return "pg12"
    elif [13, 0] <= version_as_list < [14, 0]:
        return "pg13"
    elif [14, 0] <= version_as_list < [15, 0]:
        return "pg14"
    elif [15, 0] <= version_as_list < [16, 0]:
        return "pg15"
    elif [16, 0] <= version_as_list < [17, 0]:
        return "pg16"
    elif version_as_list >= [17, 0]:
        return "pg17"
    return ""


def retry_if_storage_error(exception):
    """
    Return True if we should retry (in this case when it's an psycopg2.OperationalError can happen on db connect error)
    False otherwise
    """
    log.info("During initialization of storage next error occurred: %s", exception)
    import storage
    return isinstance(exception, storage.StorageException)


def get_encryption():
    encrypt_backups = os.getenv("KEY_SOURCE", 'false').lower()
    return encrypt_backups != 'false'


def validate_user(username, password):
    if not os.getenv("AUTH", "false").lower() == "false":
        return username == os.getenv("POSTGRES_USER") and \
               password == os.getenv("POSTGRES_PASSWORD")
    else:
        return True

