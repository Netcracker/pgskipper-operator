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

import time
import logging
import sys
from utils_common import RecoveryException

log = logging.getLogger()


class PostgresqlClient:

    def __init__(self, oc_client, retry_count=100):
        self.oc = oc_client
        self.retry_count = retry_count

    def execute_select_query(self, query):
        import psycopg2
        from psycopg2 import Error
        import os
        try:
            connection = psycopg2.connect(user="postgres",
                                          password=os.getenv('POSTGRES_PASSWORD'),
                                          host=os.getenv('POSTGRES_HOST'),
                                          port="5432",
                                          database="postgres")

            cursor = connection.cursor()
            cursor.execute(query)
            for p in cursor.fetchall():
                return p[0]
        except (Exception, Error) as error:
            log.error("Postgres communication failed: {}".format(error))

    def execute_local_query(self, pod_id, query):
        return self.oc.oc_exec(pod_id, "psql -h localhost -p 5432 postgres -t -c \"{}\"".format(query)).strip()

    def wait_db_response(self, pod_id, query, result):
        log.info("Start waiting for response '{}' for query '{}' from DB on {}".format(result, query, pod_id))
        wait_database_start_time = time.time()
        query_result = None
        select_counter = 0
        for i in range(1, self.retry_count):
            log.debug("{} try to check DB response.".format(i))
            if query_result == result:
                select_counter = select_counter + 1
                if select_counter == 5:
                    break
            else:
                select_counter = 0
            time.sleep(1)
            try:
                query_result = self.execute_select_query(query)
                log.debug("Response from DB: {}".format(query_result))
            except Exception as e:
                if 'current phase is Pending' in str(e) or \
                   'Is the server running on host' in str(e) or \
                   'server closed the connection' in str(e):
                    log.debug("One of allowed error occurred during request.")
                else:
                    raise e
        wait_database_time = time.time() - wait_database_start_time
        if query_result == result and select_counter == 5:
            log.info("SUCCESS: Received response {} in {} sec".format(query_result, wait_database_time))
        else:
            raise RecoveryException("FAILURE: Cannot get expected result '{}' for query '{}' "
                                    "from DB on pod {} in {} sec. Check if database working properly."
                                    .format(result, query, pod_id, wait_database_time))

    def wait_pg_recovery_complete(self, pod_id):
        self.wait_db_response(pod_id, "select pg_is_in_recovery()", False)

    def wait_database(self, pod_id):
        self.wait_db_response(pod_id, "select 1", 1)

