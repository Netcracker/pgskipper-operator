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
import utils
import logging
from utils import get_postgres_version_by_path


_PROTECTED_DATABASES = ['template0', 'template1', 'postgres',
                        'rdsadmin',  # aws rds
                        'cloudsqladmin',  # cloudsql
                        'azure_maintenance', 'azure_sys', # azure postgresql
                        'powa'] # powa
_PROTECTED_GREENPLUM_DATABASES = ['gpadmin', 'gpperfmon']
_PROTECTED_ROLES = ['replicator', 'postgresadmin', 'psqladmin', 'azuresu', 'azure_pg_admin', 'replication']

log = logging.getLogger("configs")

def backups_storage(version=None):
    if is_external_pg():
        return '/backup-storage/external/granular'

    storageRoot = '/backup-storage'
    if not version:
        try:
            version = utils.get_version_of_pgsql_server()
        except Exception as e:
            version = get_postgres_version_by_path(storageRoot)
            log.info(f"version returned from exception block {version}")
            log.exception(e)
    storage_path = '/backup-storage/granular'
    # checking if version of pgsql server if above 10 => saving backups
    # in different folder
    if [10, 0] <= version < [11, 0]:
        storage_path = '/backup-storage/pg10/granular'
    elif [11, 0] <= version < [12, 0]:
        storage_path = '/backup-storage/pg11/granular'
    elif [12, 0] <= version < [13, 0]:
        storage_path = '/backup-storage/pg12/granular'
    elif [13, 0] <= version < [14, 0]:
        storage_path = '/backup-storage/pg13/granular'
    elif [14, 0] <= version < [15, 0]:
        storage_path = '/backup-storage/pg14/granular'
    elif [15, 0] <= version < [16, 0]:
        storage_path = '/backup-storage/pg15/granular'
    elif [16, 0] <= version < [17, 0]:
        storage_path = '/backup-storage/pg16/granular'
    elif [17, 0] <= version < [18, 0]:
        storage_path = '/backup-storage/pg17/granular'
    elif version >= [18, 0]:
        storage_path = '/backup-storage/pg18/granular'
    return storage_path


def default_namespace():
    return 'default'


def default_backup_type():
    return 'all'


def default_backup_expiration_period():
    return '2 weeks'


def postgresql_user():
    return os.getenv('POSTGRES_USER') or 'postgres'


def postgresql_host():
    return os.getenv('POSTGRES_HOST') or 'localhost'


def postgresql_port():
    return os.getenv('POSTGRES_PORT') or '5432'


def postgres_password():
    return os.getenv('POSTGRES_PASSWORD')


def postgresql_no_role_password_flag():
    if is_external_pg():
        return "--no-role-passwords"
    return ""


def protected_databases():
    return _PROTECTED_DATABASES


def protected_greenplum_databases():
    return _PROTECTED_GREENPLUM_DATABASES


def protected_roles():
    postgres_admin_user = os.getenv("POSTGRES_USER", "postgres")
    return _PROTECTED_ROLES + [postgres_admin_user]


def eviction_interval():
    interval = os.getenv("GRANULAR_EVICTION", "3600")
    if interval:
        interval = int(interval)
    return interval or 3600


def granular_cron_pattern():
    return os.getenv("GRANULAR_BACKUP_SCHEDULE", "none")

def diff_cron_pattern():
    return os.getenv("DIFF_SCHEDULE", "none")

def incr_cron_pattern():
    return os.getenv("INCR_SCHEDULE", "none")


def get_parallel_jobs():
    return os.getenv("JOB_FLAG" , "1")

def dbs_to_granular_backup():
    databases = os.getenv("DATABASES_TO_SCHEDULE")
    if databases:
        databases = databases.split(',')
    return databases or []

def connection_properties(username = postgresql_user(), password = postgres_password(), database = 'postgres'):
    return {
        'host': postgresql_host(),
        'port': postgresql_port(),
        'user': username,
        'password': password,
        'database': database,
        'connect_timeout': int(os.getenv("CONNECT_TIMEOUT", "5"))
    }


def get_encryption():
    encrypt_backups = os.getenv("KEY_SOURCE", 'false').lower()
    return encrypt_backups != 'false'


def get_pgsql_bin_path(version):
    major_version = version[0]
    minor_version = version[1]
    if major_version == 9:
        if minor_version != 4:
            return "/usr/pgsql-9.{}/bin".format(version[1])
        else:
            # GPDB uses Postgresql 9.4
            return "/usr/local/greenplum-db/bin/"
    elif major_version == 10:
        return "/usr/lib/postgresql/10/bin"
    elif major_version == 11:
        return "/usr/lib/postgresql/11/bin"
    elif major_version == 12:
        return "/usr/lib/postgresql/12/bin"
    elif major_version == 13:
        return "/usr/lib/postgresql/13/bin"
    elif major_version == 14:
        return "/usr/lib/postgresql/14/bin"
    elif major_version == 15:
        return "/usr/lib/postgresql/15/bin"
    elif major_version == 16:
        return "/usr/lib/postgresql/16/bin"
    elif major_version == 17:
        return "/usr/lib/postgresql/17/bin"
    elif major_version == 18:
        return "/usr/lib/postgresql/18/bin"


def is_external_pg():
    return os.getenv("EXTERNAL_POSTGRESQL", "") != ""
