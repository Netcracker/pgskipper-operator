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

"""
Endpoints for physical restore data-validation marker.

POST /api/v1/data-validation/marker  — write (overwrite) the marker
GET  /api/v1/data-validation/marker  — read the current marker
"""

import logging
import os

import psycopg2
import utils
from flask import request
from flask_httpauth import HTTPBasicAuth
from flask_restful import Resource

log = logging.getLogger("DataValidationEndpoint")

auth = HTTPBasicAuth()


@auth.verify_password
def verify(username, password):
    return utils.validate_user(username, password)


def _connection_props():
    return {
        'host': os.getenv('POSTGRES_HOST', 'localhost'),
        'port': os.getenv('POSTGRES_PORT', '5432'),
        'user': os.getenv('POSTGRES_USER', 'postgres'),
        'password': os.getenv('POSTGRES_PASSWORD'),
        'database': 'postgres',
        'connect_timeout': int(os.getenv('CONNECT_TIMEOUT', '5')),
    }


_CREATE_TABLE_SQL = """
CREATE TABLE IF NOT EXISTS backup_restore_markers (
    sentinel   TEXT PRIMARY KEY,
    marker     TEXT NOT NULL,
    written_at TIMESTAMPTZ DEFAULT NOW()
)
"""

_UPSERT_SQL = """
INSERT INTO backup_restore_markers (sentinel, marker, written_at)
VALUES ('current', %s, NOW())
ON CONFLICT (sentinel) DO UPDATE
    SET marker = EXCLUDED.marker, written_at = NOW()
"""

_SELECT_SQL = "SELECT marker FROM backup_restore_markers WHERE sentinel = 'current'"


class MarkerResource(Resource):

    __endpoints = ['/api/v1/data-validation/marker']

    @staticmethod
    def get_endpoints():
        return MarkerResource.__endpoints

    @auth.login_required
    def post(self):
        body = request.get_json(silent=True)
        if not body or 'marker' not in body:
            return {'error': 'Request body must contain "marker" field'}, 400

        marker = body['marker']
        if not isinstance(marker, str) or not marker:
            return {'error': '"marker" must be a non-empty string'}, 400

        try:
            conn = psycopg2.connect(**_connection_props())
            try:
                with conn:
                    with conn.cursor() as cur:
                        cur.execute(_CREATE_TABLE_SQL)
                        cur.execute(_UPSERT_SQL, (marker,))
            finally:
                conn.close()
        except Exception:
            log.exception("Failed to write marker to database")
            return {'error': 'Internal server error'}, 500

        return None, 201

    @auth.login_required
    def get(self):
        try:
            conn = psycopg2.connect(**_connection_props())
            try:
                with conn.cursor() as cur:
                    cur.execute(_SELECT_SQL)
                    row = cur.fetchone()
            finally:
                conn.close()
        except Exception:
            log.exception("Failed to read marker from database")
            return {'error': 'Internal server error'}, 500

        if row is None:
            return {'error': 'No marker found'}, 404

        return {'marker': row[0]}, 200
