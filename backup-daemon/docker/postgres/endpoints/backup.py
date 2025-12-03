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
Set of endpoints to performs actions with physical backups.
"""

import logging
import utils
from flask_restful import Resource
from flask import Response, request, stream_with_context

import requests
from requests.exceptions import HTTPError
from flask_httpauth import HTTPBasicAuth

auth = HTTPBasicAuth()


@auth.verify_password
def verify(username, password):
    return utils.validate_user(username, password)


class BackupRequest(Resource):
    __endpoints = [
        '/backup',
        '/backups/request'
    ]

    def __init__(self):
        self.__log = logging.getLogger("BackupEndpoint")

    @staticmethod
    def get_endpoints():
        return BackupRequest.__endpoints

    @auth.login_required
    def post(self):
        self.__log.debug("Endpoint /backup has been called.")
        # Redirect backup request to underlying scheduler, which keeps status of scheduled backups.
        r = requests.post('http://localhost:8085/schedule')

        if not r.ok:
            try:
                r.raise_for_status()
            except HTTPError as e:
                self.__log.exception("Something went wrong when redirecting backup request to /schedule endpoint.", e)

        self.__log.debug(r.json())

        return r.json()


class Eviction(Resource):

    __endpoints = [
        '/evict',
        '/backups/delete'
    ]

    def __init__(self, storage):
        self.__storage = storage

    @staticmethod
    def get_endpoints():
        return Eviction.__endpoints

    @auth.login_required
    def delete(self):
        backup_id = request.args.getlist('id')[0]
        vaults = self.__storage.list()
        vaults.reverse()

        # search for vault
        vault_for_eviction = None
        for vault in vaults:
            if vault.get_id() == backup_id:
                vault_for_eviction = vault
                break
        if vault_for_eviction:
            self.__storage.evict(vault_for_eviction)
            return "Ok"
        else:
            return "Not Found"


class Download(Resource):

    __endpoints = [
        '/get',
        '/backups/download'
    ]

    def __init__(self, storage):
        self.__storage = storage
        self.__log = logging.getLogger("DownloadBackupEndpoint")

    @staticmethod
    def get_endpoints():
        return Download.__endpoints

    @auth.login_required
    def get(self):
        def generate(storage, vault):
            stream = storage.get_backup_as_stream(vault)
            with stream as f:
                chunk_size = 4096
                while True:
                    data = f.read(chunk_size)
                    if len(data) == 0:
                        f.close()
                        return
                    yield data

        vaults = self.__storage.list()
        vaults.reverse()
        backup_id = request.args.getlist('id')[0] if request.args.getlist('id') else None

        # search for vault
        vault_for_streaming = None
        if backup_id:
            for vault in vaults:
                if vault.get_id() == backup_id:
                    vault_for_streaming = vault
                    break
        else:
            for vault in vaults:
                if not vault.is_failed():
                    vault_for_streaming = vault
                    break
        if vault_for_streaming:
            return Response(stream_with_context(
                generate(self.__storage, vault_for_streaming)),
                            mimetype='application/octet-stream',
                            headers=[
                                ('Content-Type', 'application/octet-stream'),
                                ('Content-Disposition',
                                 "pg_backup_{}.tar.gz".format(
                                     vault_for_streaming.get_id()))
                            ])
        else:
            return Response("Cannot find backup", status=500)
