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
Set of endpoints that provide information about backups.
"""

import logging
import utils
import requests
import os.path
import tempfile
import json

from flask import Response
from flask_restful import Resource
from flask_httpauth import HTTPBasicAuth

import storage_s3
import eviction


auth = HTTPBasicAuth()


@auth.verify_password
def verify(username, password):
    return utils.validate_user(username, password)


class Status(Resource):
    __endpoints = [
        '/health',
        '/status'
    ]

    def __init__(self, storage):
        self.__log = logging.getLogger("HealthEndpoint")
        self.__storage = storage

    @staticmethod
    def get_endpoints():
        return Status.__endpoints

    def get(self):
        self.__log.debug("Endpoint /health has been called.")
        result = {
            "status": "UP",
            "storage": {},
            "encryption": "Off"
        }

        result.update(self.__storage.get_backup_in_progress_metrics())

        schedule_rs = requests.get('http://localhost:8085/schedule')

        if not schedule_rs.ok:
            result['status'] = "PROBLEM"
            eviction_rule = None

            try:
                schedule_rs.raise_for_status()
            except requests.HTTPError as e:
                result['message'] = e.__str__
        else:
            schedule_metrics = schedule_rs.json()
            eviction_rule = schedule_metrics['eviction_rule']

            result.update({
                'backup': schedule_metrics
            })

        vaults = list([v for v in self.__storage.list() if v.is_back_up_archive_exists()])
        vaults.reverse()

        fs_free, fs_total = self.__storage.fs_space()

        dump_count = len(vaults)
        successful_dump_count = len([x for x in vaults if not x.is_failed()])

        result["storage"] = {
            "dump_count": dump_count,
            "successful_dump_count": successful_dump_count,
            "size": self.__storage.size(),
            "archive_size": self.__storage.archive_size(),
            "free_space": fs_free,
            "total_space": fs_total,
            "type": self.__storage.get_type(),
            "type_id": self.__storage.get_type_id()
        }

        if eviction_rule:
            outdated_vaults = eviction.evict(vaults, eviction_rule,
                                             accessor=lambda x: x.create_time())

            result['storage']['outdated_backup_count'] = len(outdated_vaults)

        # calculate last successful
        for vault in vaults:
            if not vault.is_failed():
                result["storage"]["lastSuccessful"] = vault.to_json()
                break

        if len(vaults) > 0:
            last_vault = vaults[:1][0]
            result["storage"]["last"] = last_vault.to_json()
            if last_vault.is_failed():
                result["status"] = "WARNING"

        if self.__storage.get_encryption():
            result["encryption"] = "On"

        if self.__log.isEnabledFor(logging.DEBUG):
            debug_info = {
                'debug': True,
                'endpoint': '/health',
                'response': result
            }
            self.__log.debug(debug_info)

        return result

class List(Resource):
    __endpoints = [
        '/list',
        '/backups/list'
    ]

    def __init__(self, storage):
        self.__storage = storage

    @staticmethod
    def get_endpoints():
        return List.__endpoints

    @auth.login_required
    def get(self):
        result = {
        }

        vaults = list([v for v in self.__storage.list() if v.is_back_up_archive_exists()])
        vaults.reverse()

        # calculate last successful
        for vault in vaults:
            result[vault.get_id()] = vault.to_json()

        return result


class BackupStatus(Resource):
    __endpoints = [
        '/backup/status/<backup_id>'
    ]

    def __init__(self, storage):
        self.__log = logging.getLogger('BackupRequestEndpoint')
        self.__storage = storage

    @staticmethod
    def get_endpoints():
        return BackupStatus.__endpoints

    def get(self, backup_id):
        vault = None
        result = None
        vaults = self.__storage.list()
        vaults.reverse()

        for vault in vaults:
            if vault.get_id() == backup_id:
                result = vault
                break
        if not result:
            self.__log.info("Backup %s not found" % backup_id)
            return Response("Backup %s not found \n" % backup_id, status=404)
        else:
            if vault.is_locked():
                self.__log.info("In Progress %s" % backup_id)
                return Response("In Progress \n", status=200)
            elif vault.is_failed():
                self.__log.info("Backup Failed %s" % backup_id)
                return Response("Backup Failed \n", status=200)
            elif vault.is_done():
                self.__log.info("Backup Done %s" % backup_id)
                return Response("Backup Done \n", status=200)
            else:
                self.__log.info("Backup Failed %s" % backup_id)
                return Response("Backup Failed \n", status=200)


class Health(Resource):

    __endpoints = [
        '/v2/health'
    ]

    def __init__(self, storage):
        self.__log = logging.getLogger("HealthEndpoint")
        self.__root = storage.root

    @staticmethod
    def get_endpoints():
        return Health.__endpoints

    def get(self):
        schedule_rs = requests.get('http://localhost:8085/schedule')
        # we also have to check that granular app works correctly
        protocol = "http"
        if os.getenv("TLS", "false").lower() == "true":
            protocol += "s"
        gr_backups = requests.get(protocol + '://localhost:9000/health', verify=False)
        if (schedule_rs.status_code == 200) and (self.volume_liveliness_check()) \
                and gr_backups.status_code == 200:
            return Response("OK", status=200)
        else:
            return Response("Internal server error", status=500)

    def volume_liveliness_check(self):
        mount_path = self.__root
        if os.environ['STORAGE_TYPE'] == "s3":
            try:
                with open(mount_path + "/health", "w") as f:
                    f.write("Health check")
                if self.s3_health_check(mount_path + "/health"):
                    return True
                else:
                    return False
            except (IOError, Exception) as ex:
                self.__log.exception(ex)
                return False
        else:
            try:
                f = tempfile.TemporaryFile(mode='w+t', suffix='.txt', prefix='volume_check_', dir=mount_path)
                f.write("Test")
                f.seek(0)
                contents = f.read()
                f.close()
                return True
            except (IOError, Exception) as ex:
                self.__log.exception(ex)
                return False

    def s3_health_check(self, filepath):
        bucket = os.getenv("CONTAINER")
        prefixed_filepath = os.getenv("AWS_S3_PREFIX", "") + filepath
        try:
            storage_s3.AwsS3Vault.get_s3_client().upload_file(filepath, bucket, prefixed_filepath)
        except (IOError, Exception) as ex:
            self.__log.exception(ex)
            return False
        try:
            storage_s3.AwsS3Vault.get_s3_client().delete_object(Bucket=bucket, Key=prefixed_filepath)
        except (IOError, Exception) as ex:
            self.__log.exception(ex)
            return False
        return True


class ExternalRestoreStatus(Resource):
    __endpoints = [
        '/external/restore/<restore_id>'
    ]

    def __init__(self, storage):
        self.__log = logging.getLogger('ExternalRestoreStatusEndpoint')
        self.restore_folder = f"{storage.root}/external/restore"
        self.allowable_db_types = ['AZURE', 'RDS']

    @staticmethod
    def get_endpoints():
        return ExternalRestoreStatus.__endpoints

    @auth.login_required
    def get(self, restore_id):
        self.__log.info("Get restore status request")

        external_pg_type = os.getenv("EXTERNAL_POSTGRESQL", "FALSE").upper()

        if external_pg_type not in self.allowable_db_types:
            return Response(response=json.dumps({"status": "Not Supported"}), status=501)

        restore_file_path = f"{self.restore_folder}/{restore_id}"
        if os.path.isfile(restore_file_path):
            with open(restore_file_path, 'r') as f:
                status_map = json.load(f)
                f.close()
                return status_map, 200
        else:
            self.__log.info(f"Restore process not found {restore_id}")
            return Response(response=json.dumps({"status": "Not Found"}), status=404)
