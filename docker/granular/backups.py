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

import datetime
import json
import logging
import shutil
import os
import re
import time

import storage_s3
import configs
import utils
from itertools import groupby


class BackupNotFoundException(Exception):

    def __init__(self, backup_id, namespace, database=None):
        super(Exception, self).__init__("Backup of database '%s' is not found in backup '%s' in namespace '%s'."
                                        % (database, backup_id, namespace))


class BackupBadStatusException(Exception):

    def __init__(self, backup_id, status=None, database=None):
        super(Exception, self).__init__("Backup of database '%s' in backup '%s' has bad status for restore: %s"
                                        % (database, backup_id, status))


class BackupFailedException(Exception):

    def __init__(self, database, reason=None):
        super(Exception, self).__init__("Backup of database '%s' has failed: %s." % (database, reason))


class RestoreFailedException(Exception):

    def __init__(self, database, reason=None):
        super(Exception, self).__init__("Restore of database '%s' has failed: %s" % (database, reason))


class BackupStatus:
    SUCCESSFUL = 'Successful'
    FAILED = 'Failed'
    IN_PROGRESS = 'In progress'
    PLANNED = 'Planned'
    UNKNOWN = 'Unknown'
    CANCELED = 'Canceled'


def get_backup_status_id(status):
    statuses = {
        BackupStatus.SUCCESSFUL: 0,
        BackupStatus.CANCELED: 7,
        BackupStatus.FAILED: 6,
        BackupStatus.IN_PROGRESS: 5,
        BackupStatus.PLANNED: 4,
        BackupStatus.UNKNOWN: -1
    }
    return statuses[status]

def is_valid_namespace(namespace):
    return re.match("^[a-zA-z0-9_]+$", namespace) is not None


def backup_exists(backup_id, namespace=configs.default_namespace(), external_backup_storage=None):
    return os.path.exists(build_backup_path(backup_id, namespace, external_backup_storage))


def database_backup_exists(backup_id, database, namespace=configs.default_namespace(), external_backup_storage=None):
    return os.path.exists(build_database_backup_path(backup_id, database, namespace, external_backup_storage))


def build_namespace_path(namespace=configs.default_namespace()):
    return '%s/%s' % (configs.backups_storage(), namespace)


def build_database_backup_path(backup_id, database,
                               namespace=configs.default_namespace(), external_backup_storage=None):
    if configs.get_encryption():
        return '%s/%s_enc.dump' % (
            build_backup_path(backup_id, namespace, external_backup_storage), database)
    else:
        return '%s/%s.dump' % (
            build_backup_path(backup_id, namespace, external_backup_storage), database)


def build_roles_backup_path(backup_id, database,
                            namespace=configs.default_namespace(), external_backup_storage=None):
    if configs.get_encryption():
        return "%s/%s.roles_enc.sql" % (
            build_backup_path(backup_id, namespace, external_backup_storage), database)
    else:
        return "%s/%s.roles.sql" % (
            build_backup_path(backup_id, namespace, external_backup_storage), database)


def build_database_backup_full_path(backup_id, database, storage_root,
                                        namespace=configs.default_namespace(),
                                        ):
    if configs.get_encryption():
        return '%s/%s/%s/%s_enc.dump' % (
        storage_root, namespace, backup_id, database)
    else:
        return '%s/%s/%s/%s.dump' % (
        storage_root, namespace, backup_id, database)


def build_database_restore_report_path(backup_id, database, restore_tracking_id, namespace=configs.default_namespace()):
    return '%s/%s.%s.report' % (build_backup_path(backup_id, namespace), database, restore_tracking_id)


def build_backup_path(backup_id, namespace=configs.default_namespace(), external_backup_storage=None):
    return '%s/%s/%s' % (configs.backups_storage() if external_backup_storage is None else external_backup_storage,
                         namespace, backup_id)


def build_external_backup_root(external_backup_path):
    return '%s/%s' % (os.getenv("EXTERNAL_STORAGE_ROOT"), external_backup_path)


def build_backup_status_file_path(backup_id, namespace=configs.default_namespace(), external_backup_storage=None):
    return '%s/status.json' % build_backup_path(backup_id, namespace, external_backup_storage)


def build_restore_status_file_path(backup_id, tracking_id, namespace=configs.default_namespace(),
                                   external_backup_storage=None):
    return '%s/%s.json' % (build_backup_path(backup_id, namespace, external_backup_storage), tracking_id)


def get_key_name_by_backup_id(backup_id, namespace, external_backup_storage=None):
    status_path = build_backup_status_file_path(backup_id, namespace, external_backup_storage)
    with open(status_path) as f:
        data = json.load(f)
        return data.get("key_name")

def generate_short_id():
    return datetime.datetime.now().strftime("%Y%m%dT%H%M")

def generate_id():
    return datetime.datetime.now().strftime("%Y%m%dT%H%M%S%f")


def generate_backup_id():
    return 'backup-%s' % generate_id()


def generate_restore_id(backup_id, namespace=configs.default_namespace()):
    m = re.match("^backup-(?P<backupId>[a-zA-Z0-9]+)$", backup_id)
    backup_id = m.group('backupId')
    return 'restore-%s-%s-%s' % (namespace, backup_id, generate_id())


def extract_backup_id_from_tracking_id(tracking_id):
    m = re.match("^restore-(?P<namespace>[a-zA-Z0-9_]+)-(?P<backupId>[a-zA-Z0-9]+)-[a-zA-Z0-9]+$", tracking_id)
    return 'backup-%s' % m.group('backupId'), m.group('namespace')


# Kindly offered by https://stackoverflow.com/a/1094933/6519476
def sizeof_fmt(num, suffix='B'):
    for unit in ['', 'Ki', 'Mi', 'Gi', 'Ti', 'Pi', 'Ei', 'Zi']:
        if abs(num) < 1024.0:
            return "%3.1f%s%s" % (num, unit, suffix)
        num /= 1024.0
    return "%.1f%s%s" % (num, 'Yi', suffix)


def calculate_expiration_timestamp(start_timestamp, period):
    return start_timestamp + get_seconds(period)


def is_backup_completed(status):
    return status == BackupStatus.SUCCESSFUL or status == BackupStatus.FAILED


def get_seconds(delta):
    time_unit = delta.split()

    if len(time_unit) != 2 or not time_unit[0].isdigit():
        raise Exception("Malformed expiration period: %s." % delta)

    value = int(time_unit[0])
    unit = time_unit[1].lower()

    if unit == 'week' or unit == 'weeks':
        td = datetime.timedelta(weeks=value)
    elif unit == 'day' or unit == 'days':
        td = datetime.timedelta(days=value)
    elif unit == 'hour' or unit == 'hours':
        td = datetime.timedelta(hours=value)
    elif unit == 'minute' or unit == 'minutes':
        td = datetime.timedelta(minutes=value)
    elif unit == 'second' or unit == 'seconds':
        td = datetime.timedelta(seconds=value)
    else:
        raise Exception("Time unit '%s' is not supported" % unit)

    return int(td.total_seconds())


def is_database_protected(database):
    return database in configs.protected_databases() or database in configs.protected_greenplum_databases()


def get_backup_create_date(backup_id):
    return int(datetime.datetime.strptime(backup_id, "backup-%Y%m%dT%H%M%S%f").strftime('%s'))


def backup_expired(backup, expire_date):
    return 1 if get_backup_create_date(os.path.basename(backup)) < expire_date else 0


def get_s3_client():
    return storage_s3.AwsS3Vault()


def sweep_manager():
    """ Sweep procedure manager """
    if os.getenv("USE_EVICTION_POLICY_FIRST") is None or os.getenv("USE_EVICTION_POLICY_FIRST").lower() == 'false':
        sweep_by_keep()
    else:
        sweep_by_policy()


def sweep_by_keep():
    log = logging.getLogger("Sweeper")
    log.info("Start backups sweeping by keep.")
    current_time = time.time()
    storage = configs.backups_storage()
    s3 = None
    if os.environ['STORAGE_TYPE'] == "s3":
        s3 = get_s3_client()
        namespaces = s3.get_granular_namespaces(storage)

    for namespace in os.listdir(storage) if not s3 else namespaces:
        log.info("Sweeping namespace: %s." % namespace)
        expired_backups = []
        failed_expired_backups = []

        healthy_backups = 0
        if s3:
            backup_ids = s3.get_backup_ids(storage, namespace)

        for backup_id in os.listdir(build_namespace_path(namespace)) if not s3 else backup_ids:
            status_file = build_backup_status_file_path(backup_id, namespace)
            if s3:
                try:
                    if s3.is_file_exists(status_file):
                        status_file = s3.read_object(status_file)
                        backup_details = json.loads(status_file)
                    else:
                        log.error("Cannot find status file in bucket with backup id {}".format(backup_id))
                        failed_expired_backups.append(backup_id)
                        continue
                except ValueError:
                    log.exception("Cannot read status file")

            elif not os.path.isfile(status_file):
                failed_expired_backups.append(backup_id)
                continue
            else:
                backup_details = utils.get_json_by_path(status_file)

            expires = backup_details.get('expires')
            backup_status = backup_details.get('status')
            timestamp = backup_details.get('timestamp')

            if backup_status == BackupStatus.SUCCESSFUL:
                # We may get unicode string here so check `basestring`, but not `str`
                if isinstance(expires, str) and expires.lower() == 'never':
                    continue
                else:
                    healthy_backups += 1

            if expires < current_time:
                if backup_status == BackupStatus.SUCCESSFUL:
                    expired_backups.append((backup_id, timestamp))  # Keep timestamp to sort later.
                else:
                    # We delete expired PLANNED and IN_PROGRESS backups as well
                    # since they probably hang if they have already expired, not not even finished.
                    failed_expired_backups.append(backup_id)

        if expired_backups and len(expired_backups) == healthy_backups:
            # If all healthy are expired then keep the freshest.
            # Sort by timestamp and take ID of the latest.
            expired_backups.sort(key=lambda backup: backup[1])
            saved_backup = expired_backups.pop()[0]
            log.info("All successful backups are expired. Keep backup '%s/%s' as last healthy."
                     % (saved_backup, namespace))

        for i in failed_expired_backups:
            log.info("Sweep out failed expired backup status of '%s/%s'." % (namespace, i))
            shutil.rmtree(build_backup_path(i, namespace)) if not s3 else s3.delete_objects(build_backup_path(i, namespace))

        for i in expired_backups:
            log.info("Sweep out expired backup '%s/%s'." % (namespace, i[0]))
            shutil.rmtree(build_backup_path(i[0], namespace)) if not s3 else s3.delete_objects(
                build_backup_path(i[0], namespace))

    log.info("Backups sweeping finished.")


def sweep_by_policy():
    """ remove expired backups according EVICTION_POLICY """

    log = logging.getLogger("Sweeper (policy)")
    log.info("Start backups sweeping according eviction policy")
    current_time = time.time()
    storage = configs.backups_storage()
    s3 = None
    if os.environ['STORAGE_TYPE'] == "s3":
        s3 = get_s3_client()
        namespaces = s3.get_granular_namespaces(storage)

    start_point_time = time.time()
    for namespace in os.listdir(storage) if not s3 else namespaces:
        log.info("Sweeping namespace: %s." % namespace)
        expired_backups = []
        failed_expired_backups = []
        success_backups = []
        log.debug("namespace: {}".format(namespace))
        current_policy = os.getenv("EVICTION_POLICY_GRANULAR_" + namespace) or os.getenv("EVICTION_POLICY")
        log.debug("Current_policy: {}".format(current_policy))
        log.debug("policy for current namespace")

        for i in utils.parse(current_policy):
            log.debug('{}/{}'.format(i.start, i.interval))

        if s3:
            backup_ids = s3.get_backup_ids(storage, namespace)

        for backup_id in os.listdir(build_namespace_path(namespace)) if not s3 else backup_ids:
            status_file = build_backup_status_file_path(backup_id, namespace)
            if s3:
                try:
                    if s3.is_file_exists(status_file):
                        status_file = s3.read_object(status_file)
                        backup_details = json.loads(status_file)
                    else:
                        log.error("Cannot find status file in bucket with backup id {}".format(backup_id))
                        failed_expired_backups.append(os.path.join(build_backup_path(backup_id, namespace)))
                        continue
                except ValueError:
                    log.exception("Cannot read status file")
            elif not os.path.isfile(status_file):
                failed_expired_backups.append(os.path.join(build_backup_path(backup_id, namespace)))
                continue
            else:
                backup_details = utils.get_json_by_path(status_file)

            expires = backup_details.get('expires')
            backup_status = backup_details.get('status')

            if backup_status == BackupStatus.SUCCESSFUL or backup_status == BackupStatus.IN_PROGRESS:
                if isinstance(expires, str) and expires.lower() == 'never':
                    log.debug("Skip backup {} as marked Never delete".format(backup_id))
                    continue
                success_backups.append(os.path.join(build_backup_path(backup_id, namespace)))
            else:
                failed_expired_backups.append(os.path.join(build_backup_path(backup_id, namespace)))

        log.debug("success backups:")
        log.debug("\n".join(success_backups))
        log.debug("")

        log.debug("fail backups:")
        log.debug("\n".join(failed_expired_backups))
        log.debug("")

        for rule in utils.parse(current_policy):
            log.debug("current rule: {}/{}".format(rule.start, rule.interval))
            operateVersions = [t for t in success_backups if backup_expired(t, start_point_time - rule.start)]
            log.debug("stage1 selected:")
            log.debug("\n".join(operateVersions))
            if rule.interval == "delete":
                # all versions should be evicted catched by this interval
                expired_backups.extend(operateVersions)
            else:
                # group by interval and leave only first on each
                thursday = 3 * 24 * 60 * 60
                for _, versionsIt in groupby(operateVersions, lambda t: int(
                        (get_backup_create_date(os.path.basename(t)) - thursday) / rule.interval)):
                    grouped = sorted(list(versionsIt), key=lambda t: get_backup_create_date(os.path.basename(t)))
                    expired_backups.extend(grouped[:-1])
            log.debug("stage2 expired:")
            log.debug("\n".join(expired_backups))

        expired_backups = list(set(expired_backups))
        log.debug("stage3 expired unique backups:")
        log.debug("\n".join(expired_backups))

        log.debug("Remove expired backups:")
        for dir in expired_backups:
            log.info("remove backup: {}".format(dir))
            shutil.rmtree(dir) if not s3 else s3.delete_objects(dir)

        log.debug("Remove failed backups:")
        for dir in failed_expired_backups:
            log.info("remove backup: {}".format(dir))
            shutil.rmtree(dir) if not s3 else s3.delete_objects(dir)

    log.info("Backups sweeping finished.")

def transform_backup_status_v1(raw: dict) -> dict:
    raw = raw or {}
    dbs = raw.get("databases") or {}
    return {
        "status": raw.get("status"), 
        "errorMessage": raw.get("errorMessage") or raw.get("error"),
        "backupId": raw.get("backupId"),
        "creationTime": raw.get("created"),
        "completionTime": raw.get("completed") or raw.get("completionTime"),
        "databases": [
            {
                "databaseName": name,
                "status": (info or {}).get("status"),
                "size": (info or {}).get("size"),
                "duration": (info or {}).get("duration"),
                "path": (info or {}).get("path"),
                "errorMessage": (info or {}).get("errorMessage") or (info or {}).get("error"),
                "creationTime": (info or {}).get("created") or raw.get("created"),
            }
            for name, info in dbs.items()
        ],
    }

def transform_restore_status_v1(raw: dict) -> dict:
    raw = raw or {}
    dbs = raw.get("databases") or {}
    out = {
        "status": raw.get("status"),
        "errorMessage": raw.get("errorMessage") or raw.get("error"),
        "restoreId": raw.get("trackingId") or raw.get("restoreId"),
        "creationTime": raw.get("created"),
        "completionTime": raw.get("completed") or raw.get("completionTime"),
        "databases": [],
    }
    for prev_name, info in dbs.items():
        info = info or {}
        out["databases"].append({
            "previousDatabaseName": prev_name,
            "databaseName": info.get("databaseName") or info.get("restoredAs") or prev_name,
            "status": info.get("status"),
            "duration": info.get("duration"),
            "path": info.get("path"),
            "errorMessage": info.get("errorMessage") or info.get("error"),
            "creationTime": info.get("created") or raw.get("created"),
        })
    return out