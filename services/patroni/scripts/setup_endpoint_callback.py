#!/usr/bin/env python
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

import subprocess
import sys
import logging
import os

logging.basicConfig(
    level=logging.INFO,
    format='[%(asctime)s][%(levelname)-5s][category=%(name)s] %(message)s',
    datefmt='%Y-%m-%dT%H:%M:%S'
)
logger = logging.getLogger(__name__)
RUN_PROPAGATE_SCRIPT = os.getenv("RUN_PROPAGATE_SCRIPT", "True").lower()


def main():
    logger.info("Start callback with parameters {}".format(sys.argv))
    if len(sys.argv) == 4:
        action, role, cluster = sys.argv[1], sys.argv[2], sys.argv[3]
        logger.info("Cluster name: {}, new role: {}".format(cluster, role))
        if action not in ('on_start', 'on_role_change', 'on_restart', 'on_reload'):
            return
        if role == "master":
            logger.info("We were promoted to master. "
                        "Start configuration checks.")
            logger.info("Triggering propagate_settings script.")
            subprocess.check_call("/propagate_settings.sh")
        elif role == "replica":
            logger.info("Role is set to replica, "
                        "will terminate active applications connections")
            connection_properties = {
                'host': 'localhost',
                'user': 'postgres',
            }
            import psycopg2
            with psycopg2.connect(**connection_properties) as conn:
                with conn.cursor() as cur:
                    def execute_silently(query_):
                        logger.debug(
                            "Executing next query: {}".format(query_))
                        try:
                            cur.execute(query_)
                        except psycopg2.Error:
                            logger.exception("Exception happened during "
                                             "execution of the query")
                    # TODO handle pg-pool case
                    execute_silently("""
                        select pg_terminate_backend(pid) from 
                        pg_stat_activity where datname <> 'postgres' and
                        pid <> pg_backend_pid()
                    """)
                    logger.info("Connections are terminated successfully")
    else:
        sys.exit("Usage: {0} action role name".format(sys.argv[0]))


if __name__ == '__main__':
    main()
