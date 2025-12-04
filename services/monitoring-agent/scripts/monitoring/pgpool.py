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

from scripts.collector_utils import get_host, get_port


def main():
    monitoring_pw = os.environ.get("MONITORING_PASSWORD", "monitoring_role")
    monitoring_role = os.environ.get("MONITORING_USER", "monitoring_role")

    conn_properties = dict(
        host=get_host(), port=get_port(), user=monitoring_role,
        password=monitoring_pw, database='postgres'
    )
    result_data = {"status": "UP"}

    nodes = {}
    pool_nodes_info = []

    try:
        pool_nodes_info = get_pool_nodes(get_connection_properties(monitoring_role, monitoring_pw))
    except Exception as e:

        return

    for node in pool_nodes_info:
        if node[3] != 'up':
            result_data.update({"status": "PROBLEM"})
        nodes.update({
            node[1]: {
                "status": node[3],
                "select_count": node[6]
            }})

    result_data.update({"pg-pool": nodes})

    print(result_data)



def get_connection_properties(user, password):
    conn_string = f"host='{get_host()}' dbname='postgres' " \
                  f"user='{user}' " \
                  f"password='{password}' " \
                  f"port='{get_port()}' "
    return conn_string



def get_pool_nodes(conn_properties):
    with psycopg2.connect(**conn_properties) as conn:
        with conn.cursor() as cur:
            cur.execute('show pool_nodes;')
            return cur.fetchall()


if __name__ == '__main__':
    main()
