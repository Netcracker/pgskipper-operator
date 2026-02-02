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
    Set of endpoints for PostgreSQL to manipulate with write-ahead logs (WAL) archives.
"""
import threading
from threading import Thread

from flask_restful import Resource
from flask import Response, request, stream_with_context
from flask_httpauth import HTTPBasicAuth
import utils
import logging
from filelock import Timeout, FileLock

auth = HTTPBasicAuth()

RESP_NO_FILE = "Please send file using curl -XPOST -F 'file=@somefile'"
RESP_EXIST_SHA_DIFF = "Archive file already exists with different sha256."
RESP_EXIST_SHA_SAME = "Archive file already exists with same sha256."
RESP_SHA_MISMATCH = "Provided sha256 does not match sha256 of stream"
RESP_WAL_PROC_BUSY = "WAL processor is busy"

@auth.verify_password
def verify(username, password):
    return utils.validate_user(username, password)


class Upload(Resource):
    __log = logging.getLogger("UploadArchive")

    __endpoints = [
        '/archive/put',
        '/archive/upload'
    ]

    __evict_lock = threading.Lock()

    def __init__(self, storage):
        self.__storage = storage
        self.wal_processing_lock = FileLock("/tmp/wal.processing.lock")
        self.wal_eviction_lock = FileLock("/tmp/wal.eviction.lock")

    @staticmethod
    def get_endpoints():
        return Upload.__endpoints

    def __evict_archive_if_rule_exist(self):
        self.__log.info("Check if eviction rule is specified and run eviction if needed")
        if self.__storage.is_archive_evict_policy_set():
            try:
                with self.wal_eviction_lock.acquire(timeout=5):
                    self.__storage.evict_archive()
            except Timeout:
                self.__log.warning("Evict is in progress. Skip new evict.")

    def __process_wall_post(self):
        filename = None
        sha256 = None
        if request.args.getlist('filename'):
            filename = request.args.getlist('filename')[0]
        if request.args.getlist('sha256'):
            sha256 = request.args.getlist('sha256')[0]

        self.__log.info(request.args)
        self.__log.info(
            "Start upload processing for {} with sha256 {}".format(
                filename, sha256))

        if self.__storage.is_archive_exists(filename):
            stored_sha = self.__storage.get_sha256sum_for_archive(filename)
            calc_sha = self.__storage.calculate_sha256sum_for_archive(filename)
            if stored_sha == sha256:
                if calc_sha != sha256:
                    self.__log.warning(
                        "Looks like file in storage broken. "
                        "Will receive new one as replacement. "
                        "Calculated sha256: {}.".format(calc_sha))
                else:
                    return Response(RESP_EXIST_SHA_SAME, status=208)
            elif not stored_sha and calc_sha == sha256:
                self.__log.warning(
                    "Found file without sha. Will store metainfo for future.")
                self.__storage.store_archive_checksum(filename, sha256)
                return Response(RESP_EXIST_SHA_SAME, status=208)
            else:
                return Response(RESP_EXIST_SHA_DIFF, status=409)

        if 'file' not in request.files:
            return Response(RESP_NO_FILE, status=400)

        file_obj = request.files['file']
        try:
            self.__storage.store_archive_checksum(filename, sha256)
            sha256_processed = self.__storage.put_archive_as_stream(
                filename, file_obj.stream)
            if sha256 == sha256_processed:
                return Response("Ok", status=200)
            else:
                self.__log.info(
                    "Wrong result sha256 hash: {}".format(sha256_processed))
                self.__storage.delete_archive(filename)
                return Response(RESP_SHA_MISMATCH, status=400)
        except Exception:
            self.__log.exception("Cannot store archive log.")
            for i in range(5):
                try:
                    self.__storage.delete_archive(filename)
                    break
                except Exception:
                    self.__log.exception("Cannot cleanup failed archive log.")
            return Response("Internal error occurred.", status=500)

    @auth.login_required
    def post(self):
        try:
            thread = Thread(target=self.__evict_archive_if_rule_exist)
            thread.start()
        except Exception as e:
            self.__log.exception("Cannot start archive eviction during post.")
        try:
            with self.wal_processing_lock.acquire(timeout=5):
                return self.__process_wall_post()
        except Timeout:
            self.__log.warning("Cannot process WAL because another in progress")
            return Response(RESP_WAL_PROC_BUSY, status=503)


class Download(Resource):

    __endpoints = [
        '/archive/get',
        '/archive/download'
    ]

    def __init__(self, storage):
        self.__storage = storage

    @staticmethod
    def get_endpoints():
        return Download.__endpoints

    @auth.login_required
    def get(self):
        def generate(storage, filename):
            with storage.get_archive_as_stream(filename) as f:
                chunk_size = 4096
                while True:
                    data = f.read(chunk_size)
                    if len(data) == 0:
                        f.close()
                        return
                    yield data

        file_name = request.args.getlist('filename')[
            0] if request.args.getlist('filename') else None
        if file_name:
            if self.__storage.is_archive_exists(file_name):
                return Response(stream_with_context(
                    generate(self.__storage, file_name)),
                                mimetype='application/octet-stream',
                                headers=[
                                    ('Content-Type',
                                     'application/octet-stream'),
                                    ('Content-Disposition', file_name)
                                ])
            else:
                return Response(
                    "Cannot find file {} in archive".format(file_name),
                    status=404)


class Delete(Resource):

    __endpoints = [
        '/archive/delete'
    ]

    def __init__(self, storage):
        self.__storage = storage

    @staticmethod
    def get_endpoints():
        return Delete.__endpoints

    @auth.login_required
    def delete(self):
        filename = request.args.getlist('filename')[0] if request.args.getlist('filename') else None
        if filename:
            self.__storage.delete_archive(filename)
