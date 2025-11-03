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

import http.client
import json
import logging
import os
import io

import flask
import flask_restful
from flask import Flask, Response
from apscheduler.schedulers.background import BackgroundScheduler

import requests
import backups
import configs
import pg_backup
import pg_restore
import utils
import glob
import storage_s3
import psycopg2
import threading
from functools import wraps

import shutil
from backups import build_backup_path, build_namespace_path, is_valid_namespace, build_backup_status_file_path
from flask_httpauth import HTTPBasicAuth

from flask import request, abort, Response, stream_with_context

from opentelemetry.instrumentation.flask import FlaskInstrumentor
from opentelemetry.sdk.trace import TracerProvider
from opentelemetry.sdk.trace.export import BatchSpanProcessor
from opentelemetry.exporter.otlp.proto.grpc.trace_exporter import OTLPSpanExporter
from opentelemetry.sdk.resources import SERVICE_NAME, Resource


auth = HTTPBasicAuth()


def superuser_authorization(func_to_decorate):
    @wraps(func_to_decorate)
    def wrap(self, *args, **kwargs):
        if utils.is_auth_needed():
            if request.authorization.username == configs.postgresql_user():
                return func_to_decorate(self, *args, **kwargs)
            else:
                abort(403, 'You are not authorized to perform such action')
        else:
            return func_to_decorate(self, *args, **kwargs)

    return wrap


@auth.verify_password
def authenticate_user(username, password):
    if utils.is_auth_needed():

        connection_properties = configs.connection_properties(username=username, password=password)
        connect = None
        try:
            connect = psycopg2.connect(**connection_properties)
            connect.cursor()
            return True
        except psycopg2.Error:
            return False
        finally:
            if connect:
                connect.close()
    else:
        return True


def common_authorization(func_to_decorate):
    @wraps(func_to_decorate)
    def wrap(self, *args, **kwargs):
        if utils.is_auth_needed():
            content_type = request.headers.get('Content-Type')

            if content_type and content_type.split(";")[0] != 'application/json' \
                    and request.headers.get('Content-Length'):
                return "Invalid request body: Content Type is not json", http.client.BAD_REQUEST

            backup_request = request.get_json() or {}

            for k in list(backup_request.keys()):
                if k not in self.allowed_fields:
                    return "Unknown field: %s" % k.encode('utf-8'), http.client.BAD_REQUEST

            databases = backup_request.get('databases') or []

            cred = request.authorization
            if not cred:
                abort(401, 'Credentials should be provided for this endpoint')

            databases_count = len(databases)
            if databases_count == 1:
                dbname = databases[0]
                connection_properties = \
                    configs.connection_properties(username=cred.username, password=cred.password, database='postgres')
                connect = None
                try:
                    connect = psycopg2.connect(**connection_properties)
                    with connect.cursor() as cur:
                        cur.execute("""
                            SELECT pg_catalog.pg_get_userbyid(d.datdba) as Owner
                            FROM pg_catalog.pg_database d WHERE d.datname = %s
                            ORDER BY 1;
                            """, (dbname,))
                        database_owner = cur.fetchone()[0]
                        if database_owner == cred.username:
                            return func_to_decorate(self, *args, **kwargs)
                        else:
                            abort(403, 'You are not authorized to perform such action')
                finally:
                    if connect:
                        connect.close()
            elif not cred.username == configs.postgresql_user():
                abort(403, 'You are not authorized to perform such action')
            else:
                return func_to_decorate(self, *args, **kwargs)
        else:
            return func_to_decorate(self, *args, **kwargs)

    return wrap


if os.getenv("DEBUG") and os.getenv("DEBUG").lower() == 'true':
    logging.getLogger().setLevel(logging.DEBUG)


def schedule_granular_backup(scheduler):
    cron_pattern = configs.granular_cron_pattern()
    if cron_pattern.lower() != 'none' and os.getenv("GRANULAR_BACKUP_SCHEDULE") != "":
        if utils.is_mirror_env():
            logging.info('It is a mirror env')
            return
        logging.info('Start schedule granular backup')
        databases = configs.dbs_to_granular_backup()
        backup_request = {'databases': databases, 'namespace': 'schedule'}
        items = cron_pattern.split(' ', 5)
        minute, hour, day, month, day_of_week = items[0], items[1], items[2], items[3], items[4]

        granular_backup_request = GranularBackupRequestEndpoint()

        return scheduler.add_job(
            granular_backup_request.perform_granular_backup,
            'cron',
            [backup_request],
            minute=minute,
            hour=hour,
            day=day,
            month=month,
            day_of_week=day_of_week)


def schedule_diff_backup(scheduler):
    cron_pattern = configs.diff_cron_pattern()
    logging.info(f'DIFF SHEDULE {os.getenv("DIFF_SCHEDULE")}')
    if cron_pattern.lower() != 'none' and os.getenv("DIFF_SCHEDULE") is not None:
        logging.info('Start schedule diff backup')
        items = cron_pattern.split(' ', 5)
        logging.info(f"{items} cron items")
        minute, hour, day, month, day_of_week = items[0], items[1], items[2], items[3], items[4]

        diff_backup_request = DiffBackupRequestEndpoint()

        return scheduler.add_job(
            diff_backup_request.perform_diff_backup,
            'cron',
            minute=minute,
            hour=hour,
            day=day,
            month=month,
            day_of_week=day_of_week)

def schedule_incr_backup(scheduler):
    cron_pattern = configs.incr_cron_pattern()
    logging.info(f'INCR SHEDULE {os.getenv("INCR_SCHEDULE")}')
    if cron_pattern.lower() != 'none' and os.getenv("INCR_SCHEDULE") is not None:
        logging.info('Start schedule incr backup')
        items = cron_pattern.split(' ', 5)
        logging.info(f"{items} cron items")
        minute, hour, day, month, day_of_week = items[0], items[1], items[2], items[3], items[4]

        incr_backup_request = IncrBackupRequestEndpoint()

        return scheduler.add_job(
            incr_backup_request.perform_incr_backup,
            'cron',
            minute=minute,
            hour=hour,
            day=day,
            month=month,
            day_of_week=day_of_week)

class GranularBackupsListEndpoint(flask_restful.Resource):

    def __init__(self):
        self.log = logging.getLogger('BackupsListEndpoint')
        self.s3 = storage_s3.AwsS3Vault() if os.environ['STORAGE_TYPE'] == "s3" else None

    @auth.login_required
    def get(self):
        # for gke full backup
        # if os.getenv("GOOGLE_APPLICATION_CREDENTIALS"):
        #     self.log.info('Getting GKE backup list')
        #     client = utils.GkeBackupApiCaller()
        #     response = client.backup_list()
        #     return response
        status = {}
        storage = configs.backups_storage()

        if self.s3:
            if not self.s3.is_s3_storage_path_exist(storage):
                return "Backups in s3 storage does not exist.", http.client.NOT_FOUND
            namespaces = self.s3.get_granular_namespaces(storage)
        elif not os.path.exists(storage):
            return "Backups storage does not exist.", http.client.NOT_FOUND

        for namespace in os.listdir(storage) if not self.s3 else namespaces:
            if not backups.is_valid_namespace(namespace):
                continue

            status[namespace] = {}
            if self.s3:
                backup_ids = self.s3.get_backup_ids(storage, namespace)
            for backup in os.listdir(backups.build_namespace_path(namespace)) if not self.s3 else backup_ids:
                status_file = backups.build_backup_status_file_path(backup, namespace)
                if self.s3:
                    try:
                        if self.s3.is_file_exists(status_file):
                            status_file = self.s3.read_object(status_file)
                            backup_status = json.loads(status_file)
                            status[namespace][backup] = {
                                'status': backup_status.get('status'),
                                'created': backup_status.get('created'),
                                'expirationDate': backup_status.get('expirationDate')
                            }
                        else:
                            self.log.error("Cannot find status file in bucket with backup id {}".format(backup))
                            status[namespace][backup] = {'status': 'Unknown'}

                    except ValueError:
                        self.log.exception("Cannot read status file")
                        status[namespace][backup] = {'status': 'Unknown'}

                elif os.path.isfile(status_file):
                    with open(status_file, 'r') as f:
                        try:
                            backup_status = json.load(f)
                            status[namespace][backup] = {
                                'status': backup_status.get('status'),
                                'created': backup_status.get('created'),
                                'expirationDate': backup_status.get('expirationDate')
                            }
                        except ValueError:
                            self.log.exception("Cannot read status file")
                            status[namespace][backup] = {'status': 'Unknown'}
                else:
                    status[namespace][backup] = {'status': 'Unknown'}

        return status, http.client.OK


class GranularBackupRequestEndpoint(flask_restful.Resource):

    def __init__(self):
        self.log = logging.getLogger('BackupRequestEndpoint')
        self.allowed_fields = ['backupId',
                               'namespace',
                               'databases',
                               'keep',
                               'compressionLevel',
                               'externalBackupPath',
                               'storageName',
                               'blobPath']

    def perform_granular_backup(self, backup_request):
        # # for gke full backup
        # if os.getenv("GOOGLE_APPLICATION_CREDENTIALS"):
        #     self.log.info('Perform GKE backup')
        #     client = utils.GkeBackupApiCaller()
        #     backup_id = client.perform_backup()
        #     if "error" not in backup_id:
        #         return {
        #             'backupId': backup_id
        #         }, http.client.ACCEPTED
        #     else:
        #         return backup_id, http.client.BAD_REQUEST

        self.log.info('Perform granular backup')

        for k in list(backup_request.keys()):
            if k not in self.allowed_fields:
                self.log.exception("Unknown field: %s" % k.encode('utf-8'))
                return "Unknown field: %s" % k.encode('utf-8'), http.client.BAD_REQUEST

        databases = backup_request.get('databases') or []
        namespace = backup_request.get('namespace') or configs.default_namespace()
        if databases:
            baselist = utils.get_database_list(databases)

        if not isinstance(databases, list) and not isinstance(databases, tuple):
            self.log.exception("Field 'database' must be an array.")
            return "Field 'database' must be an array.", http.client.BAD_REQUEST

        if not backups.is_valid_namespace(namespace):
            self.log.exception("Invalid namespace name: %s." % namespace.encode('utf-8'))
            return "Invalid namespace name: %s." % namespace.encode('utf-8'), http.client.BAD_REQUEST

        for database in databases:
            if backups.is_database_protected(database):
                self.log.exception("Database '%s' is not suitable for backup/restore." % database)
                return "Database '%s' is not suitable for backup/restore." % database, http.client.FORBIDDEN

            if database not in baselist:
                self.log.exception("Database '%s' does not exist" % database)
                return "Database '%s' does not exist" % database, http.client.BAD_REQUEST

        backup_id = backups.generate_backup_id()
        backup_request['backupId'] = backup_id

        worker = pg_backup.PostgreSQLDumpWorker(databases, backup_request, backup_request.get('blobPath'))

        worker.start()

        return {
                   'backupId': backup_id
               }, http.client.ACCEPTED

    @auth.login_required
    @common_authorization
    def post(self):
        content_type = request.headers.get('Content-Type')

        if content_type and content_type.split(";")[0] != 'application/json' \
                and request.headers.get('Content-Length'):
            return "Invalid request body: Content Type is not json", http.client.BAD_REQUEST

        backup_request = request.get_json() or {}

        return self.perform_granular_backup(backup_request)


class GranularBackupStatusEndpoint(flask_restful.Resource):

    def __init__(self):
        self.log = logging.getLogger('BackupRequestEndpoint')
        self.s3 = storage_s3.AwsS3Vault() if os.environ['STORAGE_TYPE'] == "s3" else None

    @auth.login_required
    def get(self, backup_id):
        if not backup_id:
            return "Backup ID is not specified.", http.client.BAD_REQUEST
        # for gke full backup
        # if os.getenv("GOOGLE_APPLICATION_CREDENTIALS"):
        #     self.log.info('Getting GKE backup status')
        #     client = utils.GkeBackupApiCaller()
        #     response = client.backup_status(backup_id)
        #     return response

        namespace = flask.request.args.get('namespace') or configs.default_namespace()

        if not backups.is_valid_namespace(namespace):
            return "Invalid namespace name: %s." % namespace.encode('utf-8'), http.client.BAD_REQUEST

        external_backup_path = flask.request.args.get('externalBackupPath') or None
        external_backup_root = None
        if external_backup_path is not None:
            external_backup_root = backups.build_external_backup_root(external_backup_path)
        backup_status_file = backups.build_backup_status_file_path(backup_id, namespace, external_backup_root)
        if self.s3:
            try:
                status = self.s3.read_object(backup_status_file)
                logging.info(status)

            except:
                return "Backup in bucket is not found.", http.client.NOT_FOUND
            return json.loads(status), http.client.OK
        else:
            if not os.path.isfile(backup_status_file):
                return "Backup is not found.", http.client.NOT_FOUND

            return utils.get_json_by_path(backup_status_file), http.client.OK


class GranularBackupStatusJSONEndpoint(flask_restful.Resource):

    def __init__(self):
        self.log = logging.getLogger('BackupRequestEndpoint')
        self.allowed_fields = ['backupId', 'namespace', 'externalBackupPath']
        self.s3 = storage_s3.AwsS3Vault() if os.environ['STORAGE_TYPE'] == "s3" else None

    @auth.login_required
    def post(self):
        backup_request = flask.request.get_json() or {}

        for k in list(backup_request.keys()):
            if k not in self.allowed_fields:
                return "Unknown field: %s" % k.encode('utf-8'), http.client.BAD_REQUEST

        backup_id = backup_request.get('backupId')
        namespace = backup_request.get('namespace') or configs.default_namespace()

        if not backups.is_valid_namespace(namespace):
            return "Invalid namespace name: %s." % namespace.encode('utf-8'), http.client.BAD_REQUEST

        if not backup_request:
            return "Request body is empty.", http.client.BAD_REQUEST

        if not backup_id:
            return "Backup ID is not specified.", http.client.BAD_REQUEST

        external_backup_path = backup_request.get('externalBackupPath')
        external_backup_root = None
        if external_backup_path is not None:
            external_backup_root = backups.build_external_backup_root(external_backup_path)
        status_path = backups.build_backup_status_file_path(backup_id, namespace, external_backup_root)

        if self.s3:
            try:
                status = self.s3.read_object(status_path)
                logging.info(status)

            except:
                return "Backup in bucket is not found.", http.client.NOT_FOUND
            return json.loads(status), http.client.OK
        else:
            if not os.path.isfile(status_path):
                return "Backup is not found.", http.client.NOT_FOUND

            with open(status_path) as f:
                return json.load(f), http.client.OK


class GranularRestoreRequestEndpoint(flask_restful.Resource):

    def __init__(self):
        self.log = logging.getLogger('BackupRequestEndpoint')
        self.allowed_fields = ['backupId', 'namespace', 'databases', 'force', 'restoreRoles', 'databasesMapping',
                               'externalBackupPath', 'singleTransaction', "dbaasClone"]
        self.s3 = storage_s3.AwsS3Vault() if os.environ['STORAGE_TYPE'] == "s3" else None

    @auth.login_required
    @superuser_authorization
    def post(self):
        restore_request = flask.request.get_json() or {}
        # for gke full backup
        # if os.getenv("GOOGLE_APPLICATION_CREDENTIALS"):
        #     self.log.info('Perform GKE restore')
        #     client = utils.GkeBackupApiCaller()
        #     response = client.restore(restore_request)
        #     return response

        for k in list(restore_request.keys()):
            if k not in self.allowed_fields:
                return "Unknown field: %s" % k.encode('utf-8'), http.client.BAD_REQUEST

        databases = restore_request.get('databases') or []
        databases_mapping = restore_request.get('databasesMapping') or {}

        if not isinstance(databases, list) and not isinstance(databases, tuple):
            return "Field 'database' must be an array.", http.client.BAD_REQUEST

        if not isinstance(databases_mapping, dict):
            return "Field 'database_mapping' must be a dictionary.", http.client.BAD_REQUEST

        backup_id = restore_request.get('backupId')
        if not backup_id:
            return "Backup ID is not specified.", http.client.BAD_REQUEST

        namespace = restore_request.get('namespace') or configs.default_namespace()

        if not backups.is_valid_namespace(namespace):
            return "Invalid namespace name: %s." % namespace.encode('utf-8'), http.client.BAD_REQUEST

        external_backup_path = restore_request.get('externalBackupPath')
        external_backup_root = None
        if external_backup_path is not None:
            external_backup_root = backups.build_external_backup_root(external_backup_path)
        backup_details_file = backups.build_backup_status_file_path(backup_id, namespace, external_backup_root)

        if self.s3:
            try:
                status = self.s3.read_object(backup_details_file)
            except:
                return "Backup in bucket is not found.", http.client.NOT_FOUND
            backup_details = json.loads(status)

        else:
            if not os.path.isfile(backup_details_file):
                return "Backup is not found.", http.client.NOT_FOUND

            with open(backup_details_file, 'r') as f:
                backup_details = json.load(f)

        backup_status = backup_details['status']

        if backup_status != backups.BackupStatus.SUCCESSFUL:
            return "Backup status '%s' is unsuitable status for restore." % backup_status, http.client.FORBIDDEN
        if self.s3:
            databases = list(backup_details.get('databases', {}).keys())
            for database in databases:
                if not self.s3.is_file_exists(backups.build_database_backup_path(backup_id, database,
                                                                                 namespace, external_backup_root)):
                    return "Backup in bucket is not found.", http.client.NOT_FOUND
        elif not backups.backup_exists(backup_id, namespace, external_backup_root):
            return "Backup is not found.", http.client.NOT_FOUND

        ghost_databases = []
        uncompleted_backups = []

        databases = restore_request.get('databases') or list(backup_details.get('databases', {}).keys())

        # dict of owners {"db": "db_owner", ..}
        owners_mapping = {}

        for database in databases:
            database_details = backup_details['databases'].get(database)
            if not database_details:
                ghost_databases.append(database)
                continue
            if database_details['status'] != backups.BackupStatus.SUCCESSFUL:
                uncompleted_backups.append((database, database_details['status']))
                continue

            owners_mapping[database] = database_details.get('owner', 'postgres')

        if ghost_databases:
            return "Databases are not found: %s." % ', '.join([db.encode('utf-8') for db in ghost_databases]), \
                   http.client.NOT_FOUND

        if uncompleted_backups:
            return "Database backup is in unsuitable status for restore: %s." \
                   % ', '.join(['%s: %s' % (i[0].encode('utf-8'), i[1]) for i in uncompleted_backups]), \
                   http.client.FORBIDDEN

        tracking_id = backups.generate_restore_id(backup_id, namespace)
        restore_request['trackingId'] = tracking_id

        # force is false by default
        force = False
        force_param = restore_request.get('force')

        if force_param:
            if isinstance(force_param, str):
                force = force_param == 'true'
            elif type(force_param) is bool:
                force = force_param


        # restore_roles is true by default
        restore_roles = True
        restore_roles_param = restore_request.get('restoreRoles', True)

        if restore_roles_param:
            if isinstance(restore_roles_param, str):
                restore_roles = restore_roles_param == 'true'
            elif type(restore_roles_param) is bool:
                restore_roles = restore_roles_param
        single_transaction = False
        single_transaction_param = restore_request.get('singleTransaction', True)
        if single_transaction_param:
            if isinstance(single_transaction_param, str):
                single_transaction = single_transaction_param == 'true'
        elif type(single_transaction_param) is bool:
            single_transaction = single_transaction_param

        is_dbaas_clone= restore_request.get('dbaasClone')
        worker = pg_restore.PostgreSQLRestoreWorker(databases, force, restore_request, databases_mapping,
                                                    owners_mapping, restore_roles,single_transaction, is_dbaas_clone)

        worker.start()

        return {
                   'trackingId': tracking_id
               }, http.client.ACCEPTED


class TerminateBackupEndpoint(flask_restful.Resource):

    def __init__(self):
        self.log = logging.getLogger("TerminateBackupEndpoint")

    @auth.login_required
    def post(self, backup_id):
        self.log.info("Terminate request accepted for backup {}".format(backup_id))
        cancelled = False

        try:
            for thread in threading.enumerate():
                if thread.name == str(backup_id):
                    thread.cancel()
                    cancelled = thread.is_cancelled()
            if cancelled:
                self.log.info("Backup {} terminated successfully".format(thread.name))
                return Response("Backup %s terminated successfully\n" % backup_id, status=200)
            else:
                self.log.info("There is no active backup with id {}".format(backup_id))
                return Response("There is no active backup with id: %s\n" % backup_id, status=404)
        except Exception as e:
            self.log.exception("Backup {0} termination failed. \n {1}".format(backup_id, str(e)))
            return Response("Backup {} termination failed".format(backup_id), status=500)


class TerminateRestoreEndpoint(flask_restful.Resource):

    def __init__(self):
        self.log = logging.getLogger("TerminateRestoreEndpoint")

    @auth.login_required
    def post(self, tracking_id):
        self.log.info("Terminate request accepted for id {}".format(tracking_id))
        cancelled = False

        try:
            for thread in threading.enumerate():
                if thread.name == str(tracking_id):
                    thread.cancel()
                    cancelled = thread.is_cancelled()
            if cancelled:
                self.log.info("Restore {} terminated successfully".format(thread.name))
                return Response("Restore %s terminated successfully\n" % tracking_id, status=200)
            else:
                self.log.info("There is no active restore with id {}".format(tracking_id))
                return Response("There is no active backup with id: %s\n" % tracking_id, status=404)
        except Exception as e:
            self.log.exception("Restore {0} termination failed. \n {1}".format(tracking_id, str(e)))
            return Response("Restore {} termination failed".format(tracking_id), status=500)

class GranularRestoreStatusEndpoint(flask_restful.Resource):

    def __init__(self):
        self.log = logging.getLogger('BackupRequestEndpoint')
        self.s3 = storage_s3.AwsS3Vault() if os.environ['STORAGE_TYPE'] == "s3" else None

    @auth.login_required
    @superuser_authorization
    def get(self, tracking_id):
        if not tracking_id:
            return http.client.BAD_REQUEST, "Restore tracking ID is not specified."

        # for gke full backup
        # if os.getenv("GOOGLE_APPLICATION_CREDENTIALS"):
        #     self.log.info('Getting GKE restore status')
        #     client = utils.GkeBackupApiCaller()
        #     response = client.restore_status(tracking_id)
        #     return response

        try:
            backup_id, namespace = backups.extract_backup_id_from_tracking_id(tracking_id)
        except Exception as e:
            self.log.exception(e)
            return 'Malformed restore tracking ID.', http.client.BAD_REQUEST

        external_backup_path = flask.request.args.get('externalBackupPath') or None
        external_backup_root = None
        if external_backup_path is not None:
            external_backup_root = backups.build_external_backup_root(external_backup_path)
        restore_status_file = backups.build_restore_status_file_path(backup_id, tracking_id, namespace,
                                                                     external_backup_root)

        if not backups.is_valid_namespace(namespace):
            return "Invalid namespace name: %s." % namespace.encode('utf-8'), http.client.BAD_REQUEST
        if self.s3:
            try:
                status = self.s3.read_object(restore_status_file)
                logging.info(status)

            except:
                return "Backup in bucket is not found.", http.client.NOT_FOUND
            return json.loads(status), http.client.OK
        else:
            if not os.path.isfile(restore_status_file):
                return "Restore is not found.", http.client.NOT_FOUND

            return utils.get_json_by_path(restore_status_file), http.client.OK


class GranularRestoreStatusJSONEndpoint(flask_restful.Resource):

    def __init__(self):
        self.log = logging.getLogger('BackupRequestEndpoint')
        self.allowed_fields = ['trackingId', 'externalBackupPath']
        self.s3 = storage_s3.AwsS3Vault() if os.environ['STORAGE_TYPE'] == "s3" else None

    @auth.login_required
    @superuser_authorization
    def post(self):
        tracking_request = flask.request.get_json() or {}

        for k in list(tracking_request.keys()):
            if k not in self.allowed_fields:
                return "Unknown field: %s" % k.encode('utf-8'), http.client.BAD_REQUEST

        if not tracking_request:
            return "Restore tracking request has empty body.", http.client.BAD_REQUEST

        tracking_id = tracking_request.get('trackingId')

        if not tracking_id:
            return "Restore tracking ID is not specified.", http.client.BAD_REQUEST

        try:
            backup_id, namespace = backups.extract_backup_id_from_tracking_id(tracking_id)
        except Exception as e:
            self.log.exception(e)
            return 'Malformed restore tracking ID.', http.client.BAD_REQUEST

        external_backup_path = tracking_request.get('externalBackupPath')
        external_backup_root = None
        if external_backup_path is not None:
            external_backup_root = backups.build_external_backup_root(external_backup_path)
        restore_status_file = backups.build_restore_status_file_path(backup_id, tracking_id, namespace,
                                                                     external_backup_root)
        if self.s3:
            try:
                status = self.s3.read_object(restore_status_file)
                logging.info(status)

            except:
                return "Backup in bucket is not found.", http.client.NOT_FOUND
            return json.loads(status), http.client.OK
        else:
            if not os.path.isfile(restore_status_file):
                return "Restore is not found.", http.client.NOT_FOUND

            with open(restore_status_file) as f:
                return json.load(f), http.client.OK


class GranularBackupDeleteEndpoint(flask_restful.Resource):

    def __init__(self):
        self.log = logging.getLogger('GranularBackupDeleteEndpoint')
        self.s3 = storage_s3.AwsS3Vault() if os.environ['STORAGE_TYPE'] == "s3" else None

    @auth.login_required
    @superuser_authorization
    def get(self, backup_id):
        return self.process_delete(backup_id)

    @auth.login_required
    @superuser_authorization
    def post(self, backup_id):
        return self.process_delete(backup_id)

    def process_delete(self, backup_id):
        self.log.info("Request to delete backup %s" % backup_id)
        if not backup_id:
            return self.response(backup_id,
                                 "Backup ID is not specified.",
                                 backups.BackupStatus.FAILED,
                                 http.client.BAD_REQUEST)

        # for gke full backup
        # if os.getenv("GOOGLE_APPLICATION_CREDENTIALS"):
        #     self.log.info('Perform GKE backup delete')
        #     client = utils.GkeBackupApiCaller()
        #     response = client.delete_backup(backup_id)
        #     return response

        namespace = flask.request.args.get('namespace') or configs.default_namespace()

        if not is_valid_namespace(namespace):
            return self.response(backup_id,
                                 "Invalid namespace name: %s." % namespace.encode('utf-8'),
                                 backups.BackupStatus.FAILED,
                                 http.client.BAD_REQUEST)

        backup_status_file = build_backup_status_file_path(backup_id, namespace)
        if self.s3:
            try:
                self.s3.read_object(backup_status_file)
            except:
                return "Backup in bucket is not found.", http.client.NOT_FOUND

        elif not os.path.isfile(backup_status_file):
            return self.response(backup_id,
                                 "Backup is not found.",
                                 backups.BackupStatus.FAILED,
                                 http.client.NOT_FOUND)

        try:
            dir = build_backup_path(backup_id, namespace)
            if self.s3:
                self.s3.delete_objects(dir)
            else:
                terminate = TerminateBackupEndpoint()
                terminate.post(backup_id)
                shutil.rmtree(dir)

                # remove namespace dir if no more backups in namespace
                backup_list = os.listdir(build_namespace_path(namespace))
                if len(backup_list) == 0 and namespace != 'default':
                    shutil.rmtree(build_namespace_path(namespace))

        except Exception as e:
            self.log.exception(e)
            return self.response(backup_id,
                                 'An error occurred while deleting backup {} : {}.'.format(backup_id, e),
                                 backups.BackupStatus.FAILED,
                                 http.client.INTERNAL_SERVER_ERROR)

        return self.response(backup_id, "Backup deleted successfully.", backups.BackupStatus.SUCCESSFUL, http.client.OK)

    def response(self, backup_id, message, status, code):
        return {
                   'backupId': backup_id,
                   'message': message,
                   'status': status
               }, code


class GranularBackupHealthEndpoint(flask_restful.Resource):

    def __init__(self):
        self.log = logging.getLogger('GranularBackupHealthEndpoint')
        self.s3 = storage_s3.AwsS3Vault() if os.environ['STORAGE_TYPE'] == "s3" else None

    def get(self):

        status = {}
        namespace = "schedule"
        status[namespace] = {}

        namespace_path = backups.build_namespace_path(namespace)
        if not os.path.exists(namespace_path):
            return status, http.client.OK

        sorted_backups = sorted(os.listdir(namespace_path), reverse=True)
        dump_count = len(sorted_backups)
        space = os.statvfs(namespace_path)
        free_space, total_space = space.f_bfree * space.f_bsize, space.f_blocks * space.f_bsize
        status[namespace]['dump_count'] = dump_count
        status[namespace]['total_space'] = total_space
        status[namespace]['free_space'] = free_space

        if len(sorted_backups) > 0:
            status[namespace]['backup'] = {
                'count': len(sorted_backups)
            }
            last_backup = sorted_backups[-1]
            status_file = backups.build_backup_status_file_path(last_backup, namespace)
            if os.path.isfile(status_file):
                with open(status_file, 'r') as f:
                    try:
                        backup_status = json.load(f)
                        status[namespace]['last'] = {
                            'id': last_backup,
                            'status': backup_status.get('status'),
                            'status_id': backups.get_backup_status_id(backup_status.get('status')),
                            'created': backup_status.get('created'),
                            'expires': backup_status.get('expires'),
                            'expirationDate': backup_status.get('expirationDate')
                        }
                        return status, http.client.OK
                    except ValueError:
                        self.log.exception("Cannot read status file")
                        status[namespace]['last'] = {
                            'id': last_backup,
                            'status': backups.BackupStatus.UNKNOWN,
                            'status_id': backups.get_backup_status_id(backups.BackupStatus.UNKNOWN)
                        }
            else:
                status[namespace]['last'] = {
                    'id': last_backup,
                    'status': backups.BackupStatus.UNKNOWN,
                    'status_id': backups.get_backup_status_id(backups.BackupStatus.UNKNOWN)
                }

        return status, http.client.OK


class GranularBackupDownloadEndpoint(flask_restful.Resource):

    def __init__(self):
        self.log = logging.getLogger("GranularBackupDownloadEndpoint")
        self.s3 = storage_s3.AwsS3Vault() if os.environ['STORAGE_TYPE'] == "Zs3" else None

    @auth.login_required
    def get(self, backup_id):
        self.log.info("Download request accepted ")

        def generate(stream_path):
            stream = io.FileIO(stream_path, "r", closefd=True)
            with stream as f:
                chunk_size = 4096
                while True:
                    data = f.read(chunk_size)
                    if len(data) == 0:
                        f.close()
                        os.remove(stream_path)
                        self.log.info("Download ends ")
                        return
                    yield data

        namespace = flask.request.args.get('namespace') or configs.default_namespace()
        path_for_streaming = utils.get_backup_tar_file_path(backup_id, namespace)
        if path_for_streaming:
            return Response(stream_with_context(
                generate(path_for_streaming)),
                mimetype='application/octet-stream',
                headers=[
                    ('Content-Type', 'application/octet-stream'),
                    ('Content-Disposition',
                     "pg_granular_backup_{}.tar.gz".format(
                         backup_id))
                ])
        else:
            return Response("Cannot find backup ", status=404)



class DiffBackupRequestEndpoint(flask_restful.Resource):

    def __init__(self):
        self.log = logging.getLogger('DifferentialBackup')
        self.allowed_fields = ['timestamp']

    def perform_diff_backup(self):

        self.log.info('Perform diff backup')

        backup_id = backups.generate_short_id()
        payload = {'timestamp':backup_id}

        try:
            pgbackrest_service = get_pgbackrest_service()
            self.log.info(f"Using pgbackrest service: {pgbackrest_service}")
        except Exception as e:
            self.log.error(f"Failed to get pgbackrest service: {str(e)}")
            return http.client.INTERNAL_SERVER_ERROR

        r = requests.post(f"http://{pgbackrest_service}:3000/backup/diff", payload)
        if r.status_code == 200:
            return {
                'backupId': backup_id
            }, http.client.ACCEPTED
        else:
            return r.status_code


    def post(self):
        content_type = request.headers.get('Content-Type')

        # if content_type and content_type.split(";")[0] != 'application/json' \
        #         and request.headers.get('Content-Length'):
        #     return "Invalid request body: Content Type is not json", http.client.BAD_REQUEST


        return self.perform_diff_backup()

class IncrBackupRequestEndpoint(flask_restful.Resource):

    def __init__(self):
        self.log = logging.getLogger('IncrementalBackup')
        self.allowed_fields = ['timestamp']

    def perform_incr_backup(self):

        self.log.info('Perform incremental backup')

        backup_id = backups.generate_short_id()
        payload = {'timestamp':backup_id}

        try:
            pgbackrest_service = get_pgbackrest_service()
            self.log.info(f"Using pgbackrest service: {pgbackrest_service}")
        except Exception as e:
            self.log.error(f"Failed to get pgbackrest service: {str(e)}")
            return http.client.INTERNAL_SERVER_ERROR

        r = requests.post(f"http://{pgbackrest_service}:3000/backup/incr", payload)
        if r.status_code == 200:
            return {
                'backupId': backup_id
            }, http.client.ACCEPTED
        else:
            return r.status_code


    def post(self):
        return self.perform_incr_backup()

class GranularBackupStatusInfoEndpoint(flask_restful.Resource):

    def __init__(self):
        self.log = logging.getLogger('GranularBackupStatusMetricEndpoint')
        self.s3 = storage_s3.AwsS3Vault() if os.environ['STORAGE_TYPE'] == "s3" else None

    @auth.login_required
    def get(self):
        self.log.info("Backups metric gathering")
        storage = configs.backups_storage()
        s3 = None
        if os.environ['STORAGE_TYPE'] == "s3":
            s3 = backups.get_s3_client()
            namespaces = s3.get_granular_namespaces(storage)

        all_backups = []

        for namespace in os.listdir(storage) if not s3 else namespaces:
            if s3:
                backup_ids = s3.get_backup_ids(storage, namespace)

            for backup_id in os.listdir(build_namespace_path(namespace)) if not s3 else backup_ids:
                status_file = build_backup_status_file_path(backup_id, namespace)
                if s3:
                    try:
                        if s3.is_file_exists(status_file):
                            status_file = s3.read_object(status_file)
                            backup_details = json.loads(status_file)
                            all_backups.append(self.build_backup_info(backup_details))
                            continue
                        else:
                            self.log.error("Cannot find status file in bucket with backup id {}".format(backup_id))
                            failed_backup = {"backupId": backup_id, "namespace": namespace,  "status": backups.BackupStatus.FAILED}
                            all_backups.append(failed_backup)
                            continue
                    except ValueError:
                        self.log.exception("Cannot read status file")

                if not os.path.isfile(status_file):
                    failed_backup = {"backupId": backup_id, "namespace": namespace, "status": backups.BackupStatus.FAILED}
                    all_backups.append(failed_backup)
                    continue
                else:
                    backup_details = utils.get_json_by_path(status_file)
                    all_backups.append(self.build_backup_info(backup_details))
        response = {"granular": all_backups}

        return response, http.client.OK

    def build_backup_info(self, backup):

        backupInfo = {
            "backupId": backup.get("backupId", "UNDEFINED"),
            "namespace": backup.get("namespace", "UNDEFINED"),
            "status": backup.get("status", "UNDEFINED"),
            "expirationDate": backup.get("expirationDate", "UNDEFINED"),
            "created": backup.get("created", "UNDEFINED"),
        }

        return backupInfo

class NewBackup(flask_restful.Resource):
    __endpoints = ["/api/v1/backup"]

    def __init__(self):
        self.log = logging.getLogger("NewBackup")
        self.allowed_fields = ["storageName", "blobPath", "databases"]
        self.s3 = storage_s3.AwsS3Vault(prefix="")

    @staticmethod
    def get_endpoints():
        return NewBackup.__endpoints

    @auth.login_required
    def post(self):
        if not self.s3:
            return "S3 is not configured for backup daemon", http.client.FORBIDDEN

        body = request.get_json(silent=True) or {}
        storage_name = body.get("storageName")
        blob_path = body.get("blobPath")
        databases = body.get("databases") or []

        if not blob_path:
            return {"message": "blobPath is required"}, http.client.BAD_REQUEST
        if databases and not isinstance(databases, (list, tuple)):
            return {"message": "databases must be an array"}, http.client.BAD_REQUEST

        blob_path = normalize_blobPath(blob_path)

        # Reuse old logic directly
        backup_request = {
            "databases": list(databases),
            "blobPath": blob_path,
            "storageName": storage_name
        }
        resp = GranularBackupRequestEndpoint().perform_granular_backup(backup_request)
        body = None
        code = None
        if isinstance(resp, tuple) and len(resp) >= 2:
            body, code = resp[0], resp[1]
        elif isinstance(resp, dict):
            body, code = resp, http.client.ACCEPTED
        else:
            if code == http.client.BAD_REQUEST:
                return resp, http.client.NOT_FOUND
            if code == http.client.BAD_REQUEST:
                return resp, http.client.NOT_FOUND
            return resp

        try:
            import datetime
            created_iso = datetime.datetime.utcnow().replace(microsecond=0).isoformat() + "Z"
        except Exception:
            created_iso = ""

        dbs_out = []
        for d in (databases or []):
            if isinstance(d, dict):
                name = d.get("databaseName") or d.get("name") or ""
            else:
                name = str(d or "")
            dbs_out.append({
                "databaseName": name,
                "status": "notStarted" if name else "",
                "creationTime": created_iso
            })

        enriched = {
            "status": "notStarted",
            "backupId": body.get("backupId") if isinstance(body, dict) else None,
            "creationTime": created_iso,
            "storageName": storage_name or "",
            "blobPath": blob_path or "",
            "databases": dbs_out
        }

        return enriched, code


class NewBackupStatus(flask_restful.Resource):
    __endpoints = ["/api/v1/backup/<backup_id>"]

    def __init__(self):
        self.log = logging.getLogger("NewBackupStatus")
        self.s3 = storage_s3.AwsS3Vault(prefix="")

    @staticmethod
    def get_endpoints():
        return NewBackupStatus.__endpoints

    @auth.login_required
    def get(self, backup_id):
        if not self.s3:
            return "S3 is not configured for backup daemon", http.client.FORBIDDEN

        if not backup_id:
            return "Backup ID is not specified.", http.client.BAD_REQUEST

        namespace = request.args.get("namespace") or configs.default_namespace()
        if not backups.is_valid_namespace(namespace):
            return "Invalid namespace name: %s." % namespace.encode("utf-8"), http.client.BAD_REQUEST

        blob_path = normalize_blobPath(request.args.get("blobPath"))
        status_path = backups.build_backup_status_file_path(backup_id, blob_path=blob_path)

        
        try:
            raw = json.loads(self.s3.read_object(status_path))
        except Exception:
            return "Backup in bucket is not found.", http.client.NOT_FOUND

        if blob_path and not raw.get("blobPath"):
            raw["blobPath"] = blob_path

        return backups.transform_backup_status_v1(raw), http.client.OK
    
    @auth.login_required
    @superuser_authorization
    def delete(self, backup_id):
        if not self.s3:
            return "S3 is not configured for backup daemon", http.client.FORBIDDEN

        if not backup_id:
            return {"backupId": backup_id, "message": "Backup ID is not specified", "status": "Failed"}, http.client.BAD_REQUEST

        req_ns = request.args.get("namespace")
        blob_path = request.args.get("blobPath")
        if not blob_path:
            return {"backupId": backup_id,
                    "message": "blobPath query parameter is required (e.g. ?blobPath=tmp/a/b/c).",
                    "status": "Failed"}, http.client.BAD_REQUEST
        blob_path = normalize_blobPath(blob_path)

        def _exists(p: str) -> bool:            
            return self.s3.is_file_exists(p)

        namespace = req_ns
        if not namespace:
            candidates = []
            try:
                candidates.append(configs.default_namespace())
            except Exception:
                pass
            if "default" not in candidates:
                candidates.append("default")
            for cand in candidates:
                status_try = backups.build_backup_status_file_path(backup_id, cand, blob_path=blob_path)
                if _exists(status_try):
                    namespace = cand
                    break
        if not namespace:
            namespace = configs.default_namespace()

        status_path = backups.build_backup_status_file_path(backup_id, namespace, blob_path=blob_path)
        existed_before = _exists(status_path)

        resp = TerminateBackupEndpoint().post(backup_id)
        term_body, term_code = None, None
        try:
            if isinstance(resp, Response):
                term_body = resp.get_data(as_text=True)
                term_code = getattr(resp, "status_code", None)
            elif isinstance(resp, tuple) and len(resp) >= 2:
                term_body = resp[0]
                try:
                    term_code = int(resp[1])
                except Exception:
                    term_code = None
            elif isinstance(resp, dict):
                term_body = json.dumps(resp)
                term_code = http.client.OK
            elif isinstance(resp, (bytes, bytearray)):
                term_body = resp.decode("utf-8", "replace")
            elif isinstance(resp, str):
                term_body = resp
            else:
                term_body = repr(resp)
        except Exception:
            term_body = repr(resp)

        self.log.info("Terminate response for %s: code=%s body=%s", backup_id, term_code, term_body)

        try:
            target_dir = backups.build_backup_path(backup_id, blob_path=blob_path)
            self.s3.delete_objects(target_dir)
        except Exception as e:
            if not existed_before and term_code == http.client.NOT_FOUND:
                return {"backupId": backup_id, "message": "Backup is not found.", "status": "Failed"}, http.client.NOT_FOUND
            self.log.exception("Delete failed for %s: %s", backup_id, e)
            return {"backupId": backup_id,
                    "message": f"An error occurred while deleting backup: {e}",
                    "status": "Failed"}, http.client.INTERNAL_SERVER_ERROR

        if not existed_before and term_code == http.client.NOT_FOUND:
            return {"backupId": backup_id, "message": "Backup is not found.", "status": "Failed"}, http.client.NOT_FOUND

        if term_code and 200 <= term_code < 300:
            msg = "Backup terminated successfully. Cleanup completed."
        elif term_code == http.client.NOT_FOUND:
            msg = "No active backup. Cleanup completed."
        else:
            msg = "Termination attempted. Cleanup completed."

        return {
            "backupId": backup_id,
            "message": msg,
            "status": "Successful",
            "termination": {"code": term_code, "body": term_body}
        }, http.client.OK

class NewRestore(flask_restful.Resource):
    __endpoints = ["/api/v1/restore/<backup_id>"]

    def __init__(self):
        self.log = logging.getLogger("NewRestore")
        self.s3 = storage_s3.AwsS3Vault(prefix="")

    @staticmethod
    def get_endpoints():
        return NewRestore.__endpoints

    @auth.login_required
    @superuser_authorization
    def post(self, backup_id):
        if not self.s3:
            return "S3 is not configured for backup daemon", http.client.FORBIDDEN

        body = request.get_json(silent=True) or {}
        blob_path = body.get("blobPath")
        pairs = body.get("databases") or []
        
        dry_run = body.get("dryRun")
        if dry_run:
            self.log.info(f"Dry run requested for restore with backup ID: {backup_id}")

        if not blob_path:
            return {"message": "blobPath is required"}, http.client.BAD_REQUEST
        if not isinstance(pairs, (list, tuple)):
            return {"message": "databases must be an array of objects"}, http.client.BAD_REQUEST
        blob_path = normalize_blobPath(blob_path)

        databases = []
        databases_mapping = {}
        for item in pairs:
            item = item or {}
            prev_name = item.get("previousDatabaseName")
            curr_name = item.get("databaseName")
            if not prev_name or not curr_name:
                return {"message": "each databases item must have previousDatabaseName and databaseName"}, http.client.BAD_REQUEST
            databases.append(prev_name)
            databases_mapping[prev_name] = curr_name

        namespace = configs.default_namespace()
        if not backups.is_valid_namespace(namespace):
            return "Invalid namespace name: %s." % namespace.encode("utf-8"), http.client.BAD_REQUEST

        backup_details_file = backups.build_backup_status_file_path(backup_id, blob_path=blob_path)

        try:
            status = self.s3.read_object(backup_details_file)
            backup_details = json.loads(status)
        except Exception:
            return "Backup in bucket is not found.", http.client.NOT_FOUND

        backup_status = backup_details["status"]
        if backup_status != backups.BackupStatus.SUCCESSFUL:
            return "Backup status '%s' is unsuitable status for restore." % backup_status, http.client.FORBIDDEN

        for database in list(backup_details.get("databases", {}).keys()):
            if not self.s3.is_file_exists(backups.build_database_backup_path(backup_id, database, blob_path=blob_path)):
                return "Backup in bucket is not found.", http.client.NOT_FOUND

        ghost_databases = []
        uncompleted_backups = []
        requested = databases or list(backup_details.get("databases", {}).keys())
        owners_mapping = {}

        for database in requested:
            database_details = backup_details["databases"].get(database)
            if not database_details:
                ghost_databases.append(database)
                continue
            if database_details["status"] != backups.BackupStatus.SUCCESSFUL:
                uncompleted_backups.append((database, database_details["status"]))
                continue
            owners_mapping[database] = database_details.get("owner", "postgres")

        if ghost_databases:
            return "Databases are not found: %s." % ", ".join(ghost_databases), http.client.NOT_FOUND
        if uncompleted_backups:
            return (
                "Database backup is in unsuitable status for restore: %s."
                % ", ".join(["%s: %s" % (i[0], i[1]) for i in uncompleted_backups]),
                http.client.BAD_REQUEST,
            )

        tracking_id = backups.generate_restore_id(backup_id, namespace)

        # Defaults preserved from old endpoint
        force = False
        restore_roles = True
        single_transaction = True

        # Start worker (same as old)
        if not dry_run:
            worker = pg_restore.PostgreSQLRestoreWorker(
                requested, force,
                {"backupId": backup_id, "namespace": namespace, "trackingId": tracking_id},
                databases_mapping, owners_mapping, restore_roles, single_transaction, body.get("dbaasClone"), blob_path
            )
            worker.start()

        try:
            import datetime
            created_iso = datetime.datetime.utcnow().replace(microsecond=0).isoformat() + "Z"
        except Exception:
            created_iso = ""

        storage_name = body.get("storageName") or ""

        dbs_out = []
        for prev in (requested or []):
            prev_name = prev or ""
            restored_as = databases_mapping.get(prev_name) if isinstance(databases_mapping, dict) else None
            dbs_out.append({
                "previousDatabaseName": prev_name,
                "databaseName": restored_as or prev_name,
                "status": "notStarted" if prev_name else "",
                "creationTime": created_iso
            })

        enriched = {
            "status": "notStarted",
            "restoreId": tracking_id,
            "creationTime": created_iso,
            "storageName": storage_name,
            "blobPath": blob_path,
            "databases": dbs_out
        }

        try:
            restore_status = {
                "trackingId": tracking_id,
                "restoreId": tracking_id,
                "status": "notStarted",
                "errorMessage": None,
                "created": created_iso,
                "creationTime": created_iso,
                "completionTime": None,
                "databases": { prev: {"newDatabaseName": (databases_mapping.get(prev) if isinstance(databases_mapping, dict) else prev) or prev,
                                      "status": "notStarted",
                                      "creationTime": created_iso} for prev in (requested or []) },
                "storageName": storage_name,
                "blobPath": blob_path,
                "sourceBackupId": backup_id
            }

            if dry_run:
                return enriched, http.client.OK

            status_path = backups.build_restore_status_file_path(backup_id, tracking_id, namespace)

            try:
                if hasattr(utils, "write_in_json"):
                    utils.write_in_json(status_path, restore_status)
                else:
                    os.makedirs(os.path.dirname(status_path), exist_ok=True)
                    with open(status_path, "w") as sf:
                        json.dump(restore_status, sf)
            except Exception:
                self.log.exception("Failed to persist initial restore status for %s", tracking_id)
        except Exception:
            self.log.debug("Skipping initial restore status persist for %s", tracking_id)

        return enriched, http.client.OK


class NewRestoreStatus(flask_restful.Resource):
    __endpoints = ["/api/v1/restore/<restore_id>"]

    def __init__(self):
        self.log = logging.getLogger("NewRestoreStatus")
        self.s3 = storage_s3.AwsS3Vault(prefix="")

    @staticmethod
    def get_endpoints():
        return NewRestoreStatus.__endpoints

    @auth.login_required
    @superuser_authorization
    def get(self, restore_id):
        if not self.s3:
            return "S3 is not configured for backup daemon", http.client.FORBIDDEN

        if not restore_id:
            return "Restore tracking ID is not specified.", http.client.BAD_REQUEST

        try:
            backup_id, namespace = backups.extract_backup_id_from_tracking_id(restore_id)
        except Exception as e:
            self.log.exception(e)
            return "Malformed restore tracking ID.", http.client.BAD_REQUEST

        if not backups.is_valid_namespace(namespace):
            return "Invalid namespace name: %s." % namespace.encode("utf-8"), http.client.BAD_REQUEST

        blob_path = normalize_blobPath(request.args.get("blobPath"))
        storage_name = request.args.get("storageName") or os.environ.get("STORAGE_NAME")
        status_path = backups.build_restore_status_file_path(backup_id, restore_id, blob_path=blob_path)

        try:
            raw = json.loads(self.s3.read_object(status_path))
        except Exception:
            return "Backup in bucket is not found.", http.client.NOT_FOUND

        if blob_path and not raw.get("blobPath"):
            raw["blobPath"] = blob_path
        if storage_name and not raw.get("storageName"):
            raw["storageName"] = storage_name

        return backups.transform_restore_status_v1(raw), http.client.OK
    
    @auth.login_required
    @superuser_authorization
    def delete(self, restore_id):
        if not self.s3:
            return "S3 is not configured for backup daemon", http.client.FORBIDDEN

        if not restore_id:
            return {"restoreId": restore_id, "message": "Restore ID is not specified", "status": "Failed"}, http.client.BAD_REQUEST

        try:
            backup_id, namespace = backups.extract_backup_id_from_tracking_id(restore_id)
        except Exception as e:
            self.log.exception(e)
            resp = TerminateRestoreEndpoint().post(restore_id)
            term_code, term_body = None, None
            try:
                if isinstance(resp, Response):
                    term_body = resp.get_data(as_text=True)
                    term_code = getattr(resp, "status_code", None)
                elif isinstance(resp, tuple) and len(resp) >= 2:
                    term_body = resp[0]
                    try: term_code = int(resp[1])
                    except Exception: term_code = None
                elif isinstance(resp, dict):
                    term_body = json.dumps(resp)
                    term_code = http.client.OK
                elif isinstance(resp, str):
                    term_body = resp
                else:
                    term_body = repr(resp)
            except Exception:
                term_body = repr(resp)

            return {
                "restoreId": restore_id,
                "message": "Malformed restore ID; termination attempted but cleanup skipped.",
                "status": "Successful",
                "termination": {"code": term_code, "body": term_body}
            }, http.client.OK

        blob_path = request.args.get("blobPath")
        if not blob_path:
            return {
                "restoreId": restore_id,
                "message": "blobPath query parameter is required for cleanup (e.g. ?blobPath=tmp/a/b/c).",
                "status": "Failed"
            }, http.client.BAD_REQUEST
        blob_path = normalize_blobPath(blob_path)

        status_path = backups.build_restore_status_file_path(backup_id, restore_id, blob_path=blob_path)
        backup_base = backups.build_backup_path(backup_id, blob_path=blob_path)
        pattern_name = f"{restore_id}"
        pattern_glob = os.path.join(backup_base, pattern_name + "*")

        def _exists_file(p: str) -> bool:
            try:
                return self.s3.is_file_exists(p)
            except Exception:
                return False

        def _prefix_exists() -> bool:
            if hasattr(self.s3, "is_prefix_exists"):
                try:
                    return self.s3.is_prefix_exists(os.path.join(backup_base, pattern_name))
                except Exception:
                    return False
            return False

        existed_status = _exists_file(status_path)
        existed_prefix = _prefix_exists()
        resource_exists = existed_status or existed_prefix

        if not resource_exists:
            try:
                TerminateRestoreEndpoint().post(restore_id)
            except Exception:
                pass
            return {"restoreId": restore_id, "message": "Restore is not found.", "status": "Failed"}, http.client.NOT_FOUND

        resp = TerminateRestoreEndpoint().post(restore_id)
        term_body, term_code = None, None
        try:
            if isinstance(resp, Response):
                term_body = resp.get_data(as_text=True)
                term_code = getattr(resp, "status_code", None)
            elif isinstance(resp, tuple) and len(resp) >= 2:
                term_body = resp[0]
                try:
                    term_code = int(resp[1])
                except Exception:
                    term_code = None
            elif isinstance(resp, dict):
                term_body = json.dumps(resp)   
                term_code = http.client.OK
            elif isinstance(resp, (bytes, bytearray)):
                term_body = resp.decode("utf-8", "replace")
            elif isinstance(resp, str):
                term_body = resp
            else:
                term_body = repr(resp)
        except Exception:
            term_body = repr(resp)

        self.log.info("Terminate response for restore %s: code=%s body=%s", restore_id, term_code, term_body)

        try:
            prefix = os.path.join(backup_base, pattern_name).rstrip("/")
            self.s3.delete_objects(prefix if prefix.endswith("/") else prefix)
        except Exception as e:
            self.log.exception("Restore cleanup failed for %s: %s", restore_id, e)
            return {
                "restoreId": restore_id,
                "message": f"Termination attempted; cleanup encountered an error: {e}",
                "status": "Failed",
                "termination": {"code": term_code, "body": term_body}
            }, http.client.INTERNAL_SERVER_ERROR

        if term_code and 200 <= term_code < 300:
            msg = "Restore terminated successfully. Cleanup completed."
        else:
            msg = "Termination attempted. Cleanup completed."

        return {"restoreId": restore_id, "message": msg, "status": "Successful",
                "termination": {"code": term_code, "body": term_body}}, http.client.OK

def normalize_blobPath(blob_path):
    # Normalize blob_path by removing a single leading and trailing slash
    if isinstance(blob_path, str):
        if blob_path.startswith("/"):
            if not os.getenv("AWS_S3_PREFIX", ""):
                blob_path = blob_path[1:]
        else:
            if os.getenv("AWS_S3_PREFIX", ""):
                blob_path = "/" + blob_path
        if blob_path.endswith("/"):
            blob_path = blob_path[:-1]
    return blob_path

def get_pgbackrest_service():
    if os.getenv("BACKUP_FROM_STANDBY") == "true":
        try:
            # Query Patroni API
            patroni_response = requests.get("http://pg-patroni:8008/cluster").json()
            
            # Look for healthy streaming replicas
            streaming_replicas = [member for member in patroni_response.get('members', []) 
                                if member.get('role') == 'replica' and member.get('state') == 'streaming']
            
            if streaming_replicas:
                logging.info("Found healthy streaming replica(s), using pgbackrest-standby")
                return "backrest-standby"
            else:
                logging.info("No healthy streaming replicas found, using leader")
        except Exception as e:
            logging.error(f"Failed to query Patroni API: {str(e)}")
            raise e
    
    return "backrest"

app = Flask("GranularREST")
collector_endpoint = os.getenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
if collector_endpoint != "":
    collector_endpoint = "http://" + collector_endpoint
    NAMESPACE_PATH = '/var/run/secrets/kubernetes.io/serviceaccount/namespace'
    ns = open(NAMESPACE_PATH).read()
    resource = Resource(attributes={
        SERVICE_NAME: "postgresql-backup-daemon-" + ns
    })
    provider = TracerProvider(resource=resource)
    processor = BatchSpanProcessor(OTLPSpanExporter(endpoint=collector_endpoint, insecure=True))
    provider.add_span_processor(processor)
    FlaskInstrumentor().instrument_app(app=app, tracer_provider=provider, excluded_urls="health,/health,v2/health,/v2/health")
api = flask_restful.Api(app)

api.add_resource(GranularBackupsListEndpoint, '/backups')
api.add_resource(GranularBackupRequestEndpoint, '/backup/request')
api.add_resource(GranularBackupStatusEndpoint, '/backup/status/<backup_id>')
api.add_resource(GranularBackupStatusJSONEndpoint, '/backup/status')
api.add_resource(GranularRestoreRequestEndpoint, '/restore/request')
api.add_resource(TerminateBackupEndpoint, '/terminate/<backup_id>')
api.add_resource(TerminateRestoreEndpoint, '/restore/terminate/<tracking_id>')
api.add_resource(GranularRestoreStatusEndpoint, '/restore/status/<tracking_id>')
api.add_resource(GranularRestoreStatusJSONEndpoint, '/restore/status')
api.add_resource(GranularBackupDeleteEndpoint, '/delete/<backup_id>')
api.add_resource(GranularBackupHealthEndpoint, '/health')
api.add_resource(GranularBackupDownloadEndpoint, '/backup/download/<backup_id>')
api.add_resource(DiffBackupRequestEndpoint, '/backup/diff')
api.add_resource(IncrBackupRequestEndpoint, '/backup/incr')
api.add_resource(GranularBackupStatusInfoEndpoint, '/backup/info')
api.add_resource(NewBackup, *NewBackup.get_endpoints())
api.add_resource(NewBackupStatus, *NewBackupStatus.get_endpoints())
api.add_resource(NewRestore, *NewRestore.get_endpoints())
api.add_resource(NewRestoreStatus, *NewRestoreStatus.get_endpoints())

scheduler = BackgroundScheduler()
scheduler.start()
if os.environ['STORAGE_TYPE'] == "pgbackrest":
    schedule_diff_backup(scheduler)
    schedule_incr_backup(scheduler)
else:
    scheduler.add_job(backups.sweep_manager, 'interval', seconds=configs.eviction_interval())
    schedule_granular_backup(scheduler)