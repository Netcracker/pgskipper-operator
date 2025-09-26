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
import shutil

from psycopg2.extensions import AsIs
import logging
from threading import Thread, Event
import os
import os.path
import subprocess
import time

import psycopg2

import utils
import backups
import configs
import encryption
import storage_s3


class PostgreSQLRestoreWorker(Thread):

    def __init__(self, databases, force, restore_request, databases_mapping, owners_mapping, restore_roles=True, single_transaction=False, dbaas_clone=False):
        Thread.__init__(self)

        self.log = logging.getLogger("PostgreSQLRestoreWorker")
        self.backup_id = restore_request.get('backupId')

        if not self.backup_id:
            raise Exception("Backup ID is not specified.")

        self.databases = databases or []
        self.force = force
        self.single_transaction = single_transaction
        self.namespace = restore_request.get(
            'namespace') or configs.default_namespace()
        self.tracking_id = restore_request.get(
            'trackingId') or backups.generate_restore_id(self.backup_id,
                                                         self.namespace)
        self.name = self.tracking_id
        self.is_standard_storage = True if restore_request.get('externalBackupPath') is None else False
        self.restore_roles = restore_roles
        self.postgres_version = utils.get_version_of_pgsql_server()
        self.location = configs.backups_storage(self.postgres_version) if self.is_standard_storage \
            else backups.build_external_backup_root(restore_request.get('externalBackupPath'))
        self.external_backup_root = None if self.is_standard_storage else self.location
        self.databases_mapping = databases_mapping
        self.owners_mapping = owners_mapping
        self.bin_path = configs.get_pgsql_bin_path(self.postgres_version)
        self.parallel_jobs = configs.get_parallel_jobs()
        self.s3 = storage_s3.AwsS3Vault() if os.environ['STORAGE_TYPE'] == "s3" else None
        if self.s3:
            self.backup_dir = backups.build_backup_path(self.backup_id, self.namespace, self.external_backup_root)
            self.create_backup_dir(self.backup_dir)
        if configs.get_encryption():
            self.encryption = True
            self.key_name = backups.get_key_name_by_backup_id(self.backup_id,
                                                              self.namespace, self.external_backup_root)
        else:
            self.encryption = False
        if databases_mapping:
            self.databases = list(databases_mapping.keys())
        self.status = {
            'trackingId': self.tracking_id,
            'namespace': self.namespace,
            'backupId': self.backup_id,
            'status': backups.BackupStatus.PLANNED
        }
        self._cancel_event = Event()
        self.pg_restore_proc = None
        self.flush_status(self.external_backup_root)
        self.dbaas_clone=dbaas_clone

    def create_backup_dir(self, backup_dir):
        if not os.path.exists(backup_dir):
            os.makedirs(backup_dir)

    def log_msg(self, msg):
        return "[trackingId=%s] %s" % (self.tracking_id, msg)

    def flush_status(self, external_backup_storage=None):
        path = backups.build_restore_status_file_path(self.backup_id, self.tracking_id, self.namespace,
                                                      external_backup_storage)
        utils.write_in_json(path, self.status)
        if self.s3:
            try:
                # upload status file
                self.s3.upload_file(path)
            except Exception as e:
                raise e

    def update_status(self, key, value, database=None, flush=False):
        if database:
            databases_section = self.status.get('databases')

            if not databases_section:
                databases_section = {}
                self.status['databases'] = databases_section

            database_details = databases_section.get(database)
            if not database_details:
                database_details = {}
                databases_section[database] = database_details

            database_details[key] = value
            databases_section[database] = database_details
            self.status['databases'] = databases_section
        else:
            self.status[key] = value

        if flush or self.s3:
            self.flush_status(self.external_backup_root)

    @staticmethod
    def db_exists(database):
        connection_properties = configs.connection_properties()
        conn = None
        try:
            conn = psycopg2.connect(**connection_properties)
            with conn.cursor() as cur:
                cur.execute("select 1 from pg_database where datname = %s", (database,))
                return bool(cur.fetchone())
        finally:
            if conn:
                conn.close()

    def kill_pids_and_revoke_rights(self, database, cur, roles):
        if not self.db_exists(database):
            self.log.info(self.log_msg("skipping revoke, for not existing '%s' db" % database))
            return
        for role in roles:
            self.log.info(self.log_msg("Revoking grants from: {} on: {}.".format(role, database)))
            cur.execute("REVOKE CONNECT ON DATABASE \"%(database)s\" from \"%(role)s\";", {"database": AsIs(database),
                                                                                           "role": AsIs(role)})
        # also, revoking connect rights from public
        cur.execute("REVOKE CONNECT ON DATABASE \"%(database)s\" from PUBLIC;", {"database": AsIs(database)})
        # selecting pids to kill after revoking rights
        cur.execute("SELECT pg_terminate_backend(pid) "
                    "FROM pg_stat_activity "
                    "WHERE pid <> pg_backend_pid() "
                    "      AND datname = %s ", (database,))

    def get_pg_version_from_dump(self, dump_path):
        return utils.get_pg_version_from_dump(dump_path, self.key_name if self.encryption else None, self.bin_path)

    def restore_single_database(self, database):
        self.log.info(self.log_msg("Start restoring database '%s'."
                                   % database))

        db_start = int(time.time())
        self.update_status('creationTime',
                       datetime.datetime.fromtimestamp(db_start).isoformat(),
                       database=database)
        self.update_status('status', backups.BackupStatus.IN_PROGRESS, database)
        self.update_status('source', backups.build_database_backup_full_path(
            self.backup_id, database, self.location,
            self.namespace), database, flush=True)

        if int(self.parallel_jobs) > 1:
            pg_dump_backup_path = backups.build_backup_path(self.backup_id, self.namespace, self.external_backup_root)
            dump_path = os.path.join(pg_dump_backup_path, database)
        else:
            dump_path = backups.build_database_backup_path(self.backup_id, database,
                                                           self.namespace, self.external_backup_root)
        roles_backup_path = backups.build_roles_backup_path(self.backup_id, database,
                                                            self.namespace, self.external_backup_root)
        stderr_path = backups.build_database_backup_path(self.backup_id, database,
                                                         self.namespace, self.external_backup_root)
        stderr_path = stderr_path + '.stderr'
        stdout_path = backups.build_database_backup_path(self.backup_id, database,
                                                         self.namespace, self.external_backup_root)
        sql_script_path = stdout_path + '.sql'
        stdout_path = stdout_path + '.stdout'
        if self.s3:
            try:
                self.s3.download_file(dump_path)
                if self.restore_roles:
                    self.s3.download_file(roles_backup_path)
            except Exception as e:
                raise e
        self.update_status('path',
                       backups.build_database_backup_full_path(
                           self.backup_id, database, self.location, self.namespace),
                       database=database,
                       flush=True)
        new_bd_name = self.databases_mapping.get(database) or database
        db_owner = self.owners_mapping.get(database, 'postgres')
        dump_version = self.get_pg_version_from_dump(dump_path)
        bin_path = configs.get_pgsql_bin_path(dump_version)
        self.log.info(self.log_msg("Will use binaries: '%s' for restore."
                                   % bin_path))
        os.environ['PGPASSWORD'] = configs.postgres_password()

        connection_properties = configs.connection_properties()
        # Restoring the pg_restore command with the -C flag to save the role rights. If we restore to the old database and the same version of PG and backup
        restore_without_psql = database == new_bd_name and self.postgres_version[0]==dump_version[0] and not configs.is_external_pg() and (not self.single_transaction)
        roles = []
        conn = None
        try:
            conn = psycopg2.connect(**connection_properties)
            conn.autocommit = True
            with conn.cursor() as cur:
                # check if there are any active connections to pgsql
                cur.execute("SELECT pid "
                            "FROM pg_stat_activity "
                            "WHERE pid <> pg_backend_pid() "
                            "      AND datname = %s LIMIT 1", (new_bd_name,))
                pids = [p[0] for p in cur.fetchall()]

                # get roles to revoke from
                pg_user = configs.postgresql_user()
                cur.execute("""
                SELECT r2.rolname grantee
                FROM
                  (SELECT datname AS objname,
                          datallowconn, (aclexplode(datacl)).grantor AS grantorI,
                          (aclexplode(datacl)).grantee AS granteeI,
                          (aclexplode(datacl)).privilege_type
                   FROM pg_database) AS db
                JOIN pg_roles r1 ON db.grantorI = r1.oid
                JOIN pg_roles r2 ON db.granteeI = r2.oid
                WHERE db.objname = %s
                    AND db.privilege_type = 'CONNECT' AND r2.rolname not in ('postgresadmin', %s);
                """, (new_bd_name, pg_user,))

                roles = [p[0] for p in cur.fetchall()]

                if pids and not self.force:
                    with open(stderr_path, 'a+') as f:
                        raise backups.RestoreFailedException("Not able to restore database {} with running connection".
                                                             format(new_bd_name), '\n'.join(f.readlines()))

                if pids and self.force:
                    # revoke grants for connection, to prevent new connection
                    self.kill_pids_and_revoke_rights(new_bd_name, cur, roles)
                if (not self.single_transaction):
                    self.log.debug(self.log_msg("DROP DROP DATABASE IF EXISTS".format(new_bd_name)))
                    cur.execute('DROP DATABASE IF EXISTS \"%(database)s\"', {"database": AsIs(new_bd_name)})

                if (not restore_without_psql) and (not self.single_transaction):
                    cur.execute('CREATE DATABASE \"%(database)s\"', {"database": AsIs(new_bd_name)})
                    self.drop_lookup_func_for_db(new_bd_name)

                if self.restore_roles and os.path.isfile(roles_backup_path):
                    self.log.info(self.log_msg("Will try to restore roles"))
                    command = "psql --dbname=postgres --username {} --host {}" \
                              " --port {} --echo-all --file {}" \
                        .format(configs.postgresql_user(), configs.postgresql_host(),
                                configs.postgresql_port(), roles_backup_path)
                    if self.encryption:
                        command = \
                            "openssl enc -aes-256-cbc -nosalt -d -pass " \
                            "pass:'%s' < '%s' | psql" \
                            " --username '%s' --dbname=postgres --host '%s' " \
                            "--port '%s' --echo-all" % (
                                encryption.KeyManagement.get_object().get_password_by_name(
                                    self.key_name),
                                roles_backup_path,
                                configs.postgresql_user(),
                                configs.postgresql_host(),
                                configs.postgresql_port())

                    with open(stderr_path, 'a') as stderr:
                        with open(stdout_path, 'a') as stdout:
                            p = subprocess.Popen(command, shell=True,
                                                 stdout=stdout, stderr=stderr)
                            self.pg_restore_proc = p
                            exit_code = p.wait()

                    if exit_code != 0:
                        with open(stderr_path, 'r') as f:
                            raise backups.RestoreFailedException(database, '\n'.join(f.readlines()))
                    else:
                        self.log.info(self.log_msg("Roles has been successfully restored"))
                    self.pg_restore_proc = None


                if self.single_transaction:
                    self.log.info(self.log_msg("Attempt to restore the {} database to a single transaction".format(new_bd_name)))
                    try:
                        os.remove(sql_script_path)
                    except OSError:
                        pass
                    con_properties2 = configs.connection_properties(database=new_bd_name)
                    connDb = None
                    try:
                        connDb = psycopg2.connect(**con_properties2)
                        connDb.autocommit = True
                        with connDb.cursor() as curDb:
                            with open(sql_script_path, 'w') as file:
                                curDb.execute("select schemaname from pg_catalog.pg_tables where schemaname !~ '^pg_' AND schemaname <> 'information_schema' AND schemaname <>'public';")
                                schemas = [r[0] for r in curDb.fetchall()]
                                for schema in schemas:
                                    self.log.debug(self.log_msg("Adding to the list for deletion: SCHEMA IF EXISTS {} CASCADE\n".format(schema)))
                                    file.write(("DROP SCHEMA IF EXISTS {} CASCADE;\n".format(schema)))
                                curDb.execute("SELECT tablename FROM pg_catalog.pg_tables where schemaname !~ '^pg_' AND schemaname <> 'information_schema';")
                                tablenames = [r[0] for r in curDb.fetchall()]
                                for table in tablenames:
                                    self.log.debug(self.log_msg("Adding to the list for deletion: TABLE IF EXISTS {} CASCADE".format(table)))
                                    file.write(("DROP TABLE IF EXISTS {} CASCADE;\n".format(table)))
                    except psycopg2.OperationalError as err:
                        self.log.error(self.log_msg(err))
                        if ('database "{}" does not exist'.format(new_bd_name)) in str(err):
                            self.log.info(self.log_msg("Try create database"))
                            cur.execute('CREATE DATABASE \"%(database)s\"', {"database": AsIs(new_bd_name)})
                            self.drop_lookup_func_for_db(new_bd_name)
                    except psycopg2.Error as err:
                        self.log.error(self.log_msg(err))
                        with open(stderr_path, 'r') as f:
                            raise backups.RestoreFailedException(database, '\n'.join(f.readlines()))
                    except Exception as err:
                        self.log.error(self.log_msg(err))
                        with open(stderr_path, 'r') as f:
                            raise backups.RestoreFailedException(database, '\n'.join(f.readlines()))
                    finally:
                        if connDb:
                            connDb.close()

                    pg_restore_options = "-f - "

                    if self.dbaas_clone:
                        pg_restore_options = pg_restore_options + "--no-owner  --no-acl"

                    if int(self.parallel_jobs) > 1:
                        commandWriteToFile = "{}/pg_restore {} {} -j {} >> {}".format(bin_path, dump_path, pg_restore_options, self.parallel_jobs, sql_script_path)
                    else:
                        commandWriteToFile = "{}/pg_restore {} {} >> {}".format(bin_path, dump_path, pg_restore_options, sql_script_path)
                    commandRest = ' (echo "BEGIN;"; cat {}; echo "COMMIT;") | psql -v --single-transaction ON_ERROR_STOP=1 --echo-errors --dbname={} ' \
                                  '--user {} --host {} --port {}' .format(sql_script_path,new_bd_name,
                                                                          configs.postgresql_user(),configs.postgresql_host(),
                                                                          configs.postgresql_port())
                    self.log.debug(self.log_msg("command WriteToFile: {} ".format(commandWriteToFile)))
                    self.log.debug(self.log_msg("command Restore: {} ".format( commandRest)))
                    with open(stderr_path, 'a') as stderr:
                        with open(stdout_path, 'a+') as stdout:
                            p = subprocess.Popen(commandWriteToFile, shell=True,
                                                 stdout=stdout, stderr=stderr)
                            self.pg_restore_proc = p
                            exit_code = p.wait()
                            self.log.debug(self.log_msg("exit_code: {}".format(exit_code)))
                            if exit_code != 0:
                                raise backups.RestoreFailedException(database, '\n'.join(
                                    stderr.readlines()))
                                return()
                            else:
                                self.log.debug(self.log_msg("Restore file {} is recorded. Starting the restore".format(sql_script_path)))
                                p2 = subprocess.Popen(commandRest, shell=True,
                                                      stdout=stdout, stderr=stderr)
                                self.pg_restore_proc = p2
                                exit_code2 = p2.wait()
                                if exit_code2 != 0:
                                    raise backups.RestoreFailedException(database, '\n'.join(
                                        stderr.readlines()))
                                else:
                                    # when resetting to a single transaction, ON_ERROR_STOP=1 is sometimes ignored and
                                    # exit_code==0, so we read the last 9 characters from stdout if there is a ROLLBACK,
                                    # then we generate an exception
                                    stdout.seek(stdout.tell()-9)
                                    line = stdout.read(9)
                                    self.log.debug(self.log_msg("the last 9 characters from stdout:{}".format(line)))
                                    if "ROLLBACK" in line:
                                        self.log.error("ROLLBACK")
                                        raise backups.RestoreFailedException(database, "ROLLBACK")
                                    self.log.info(self.log_msg("Successful restored"))
                            self.pg_restore_proc = None
                            try:
                                os.remove(sql_script_path)
                            except OSError:
                                pass
                    return()
                # setting owner back
                if self.restore_roles and not restore_without_psql:
                    cur.execute('ALTER DATABASE \"%(database)s\" OWNER TO \"%(owner)s\"',
                                {"database": AsIs(new_bd_name), "owner": AsIs(db_owner)})

                if pids and self.force:
                    self.kill_pids_and_revoke_rights(new_bd_name, cur, roles)
                self.log.info(self.log_msg("Target database {} created".format(new_bd_name)))
        finally:
            if conn:
                conn.close()
        # restoring database entities
        # add restore options "-f -" for all pg versions except greenplum
        pg_restore_options = "-f - "
        if dump_version[0] == 9 and dump_version[1] == 4:
            pg_restore_options = ""
        if configs.is_external_pg() or self.dbaas_clone:
            pg_restore_options = pg_restore_options + "--no-owner  --no-acl"

        if int(self.parallel_jobs) > 1:
            pg_restore_options += "-j {} ".format(self.parallel_jobs)

        command = "{}/pg_restore {} {}| psql -v ON_ERROR_STOP=1 --dbname={} " \
                  "--user {} --host {} --port {}" \
            .format(bin_path, dump_path,
                    pg_restore_options,
                    new_bd_name,
                    configs.postgresql_user(),
                    configs.postgresql_host(),
                    configs.postgresql_port()
                    )

        self.log.info(self.log_msg("DB version: {} dump version: {}, database: {}, new DB name: {} ".
                                   format(self.postgres_version[0], dump_version[0], database,new_bd_name)))
        if restore_without_psql:
            command = "{}/pg_restore -C {} " \
                      "--user {} --host {} --port {} --dbname=postgres --no-password" \
                .format(bin_path, dump_path,configs.postgresql_user(),
                        configs.postgresql_host(), configs.postgresql_port())

            # Add support for the -j flag
            if int(self.parallel_jobs) > 1:
                command += " -j {} ".format(self.parallel_jobs)

        if self.encryption:
            command = \
                "openssl enc -aes-256-cbc -d -nosalt -pass pass:{} < {}" \
                "| {}/pg_restore {}| psql -v ON_ERROR_STOP=1 --dbname={} " \
                "--user {} --host {} " \
                "--port {}".format(
                    encryption.KeyManagement.get_object().get_password(),
                    dump_path,
                    bin_path,
                    pg_restore_options,
                    new_bd_name, configs.postgresql_user(),
                    configs.postgresql_host(),
                    configs.postgresql_port())

            # Add support for the -j flag
            if int(self.parallel_jobs) > 1:
                command += " -j {} ".format(self.parallel_jobs)

        self.log.debug(self.log_msg("Restore Command: {}".format(command)))
        if database != new_bd_name:
            self.log.info(self.log_msg("New database name: {} specified for database: {}".
                                       format(new_bd_name, database)))

        with open(stderr_path, 'a') as stderr:
            with open(stdout_path, 'a') as stdout:
                pg_restore_proc = subprocess.Popen(command,shell=True, stdout=stdout, stderr=stderr)
                self.pg_restore_proc = pg_restore_proc
                exit_code = pg_restore_proc.wait()
                self.pg_restore_proc = None

        if pids and self.force:
            conn = None
            try:
                conn = psycopg2.connect(**connection_properties)
                conn.autocommit = True
                with conn.cursor() as cur:
                    for role in roles:
                        cur.execute("GRANT CONNECT ON DATABASE \"%(database)s\" to \"%(role)s\";",
                                    {"database": AsIs(new_bd_name), "role": AsIs(role)})
                        self.log.info(self.log_msg(
                            "Rights for connection granted for db {} to role {}".format(new_bd_name, role)))
            finally:
                if conn:
                    conn.close()

        if self.restore_roles:
            self.grant_connect_to_db_for_role(roles_backup_path, new_bd_name)

        if self.restore_roles and configs.is_external_pg():
            self.grant_connect_for_external_pg(roles_backup_path, new_bd_name)

        if exit_code != 0:
            with open(stderr_path, 'r') as f:
                raise backups.RestoreFailedException(database, '\n'.join(f.readlines()))
        else:
            self.update_status('duration', int(time.time() - db_start), database=database)

        if database != new_bd_name:
            self.log.info(self.log_msg("Database '%s' has been successfully restored with new name '%s'." %
                                       (database, new_bd_name)))
        else:
            self.log.info(self.log_msg("Database '%s' has been successfully restored." % database))

    def run(self):
        try:
            self.process_restore_request()
            self.update_status('completionTime',
                           datetime.datetime.utcnow().isoformat(),
                           flush=True)
            self.update_status('status', backups.BackupStatus.SUCCESSFUL, flush=True)
            self.log.info(self.log_msg("Backup has been successfully restored."))
            if self.s3:
                shutil.rmtree(self.backup_dir)
        except Exception as e:
            self.log.exception(self.log_msg("Restore request processing has failed."))
            self.update_status('details', str(e))
            self.update_status('completionTime',
                           datetime.datetime.utcnow().isoformat(),
                           flush=True)
            self.update_status('status', backups.BackupStatus.FAILED, flush=True)
            if self.s3:
                shutil.rmtree(self.backup_dir)
        finally:
            if self.is_cancelled():
                self.on_cancel()

    def process_restore_request(self):
        self.log.info(self.log_msg("Start restore procedure."))
        start_timestamp = int(time.time())
        self.update_status('status', backups.BackupStatus.IN_PROGRESS)
        self.update_status('timestamp', start_timestamp)
        self.update_status('started', str(datetime.datetime.fromtimestamp(start_timestamp).isoformat()), flush=True)

        backup_status_file = backups.build_backup_status_file_path(self.backup_id,
                                                                   self.namespace, self.external_backup_root)
        if self.s3:
            try:
                status = self.s3.read_object(backup_status_file)
            except Exception as e:
                raise e
            backup_details = json.loads(status)

        else:
            if not os.path.isfile(backup_status_file):
                raise backups.BackupNotFoundException(self.backup_id, self.namespace)

            backup_details = utils.get_json_by_path(backup_status_file)

        backup_status = backup_details['status']

        if backup_status != backups.BackupStatus.SUCCESSFUL:
            raise backups.BackupBadStatusException(self.backup_id, backup_status)

        if not self.databases:
            self.log.info(self.log_msg("Databases not specified -> restore all databases from backup."))
            self.databases = list(backup_details.get('databases', {}).keys())

        self.log.info(self.log_msg("Databases to restore: %s" % (self.databases,)))

        # Check physical dump files existence.
        for database in self.databases:
            if self.s3:
                is_backup_exist = self.s3.is_file_exists(backups.build_database_backup_path(self.backup_id, database,
                                                                                            self.namespace, self.external_backup_root))
            else:
                if int(self.parallel_jobs) > 1:
                    pg_dump_backup_path = backups.build_backup_path(self.backup_id, self.namespace, self.external_backup_root)
                    path_for_parallel_flag_backup = os.path.join(pg_dump_backup_path, database)
                    is_backup_exist = os.path.exists(path_for_parallel_flag_backup)
                else:
                    is_backup_exist = backups.database_backup_exists(self.backup_id, database,
                                                                     self.namespace, self.external_backup_root)
            if not is_backup_exist:
                raise backups.BackupNotFoundException(self.backup_id, database, self.namespace)

        for database in self.databases:
            try:
                self.restore_single_database(database)
                self.update_status('status', backups.BackupStatus.SUCCESSFUL, database)
                self.update_status('completed', str(datetime.datetime.fromtimestamp(int(time.time())).isoformat()),
                                   flush=True)
            except Exception as e:
                self.log.exception(self.log_msg("Restore has failed: %s " % str(e)))
                self.update_status('details', str(e), database)
                self.update_status('status', backups.BackupStatus.FAILED, database, flush=True)
                raise e
            finally:
                try:
                    if int(self.parallel_jobs) > 1:
                        pg_dump_backup_path = backups.build_backup_path(self.backup_id, self.namespace, self.external_backup_root)
                        backup_path = os.path.join(pg_dump_backup_path, database)
                    else:
                        backup_path = backups.build_database_backup_path(self.backup_id, database,
                                                                         self.namespace, self.external_backup_root)
                        os.remove(backup_path + '.stderr')
                except OSError as ex:
                    self.log.exception(self.log_msg("Unable to remove stderr log due to: %s " % str(ex)))

    def grant_connect_to_db_for_role(self, roles_backup_path, db_name):
        connection_properties = configs.connection_properties()
        conn = None
        try:
            conn = psycopg2.connect(**connection_properties)
            conn.autocommit = True
            with conn.cursor() as cur:
                with open(roles_backup_path, "r") as role_file:
                    for line in role_file:
                        if not line.startswith("CREATE ROLE"):
                            continue
                        try:
                            role_name = line.split('CREATE ROLE ')[1].split(';')[0]
                            cur.execute(
                                "GRANT CONNECT ON DATABASE \"%(database)s\" TO \"%(role)s\";",
                                {"database": AsIs(db_name), "role": AsIs(role_name)}
                            )
                            self.log.info(self.log_msg(
                                "Rights for connection granted for db {} to role {}".format(db_name, role_name)))
                        except Exception as e:
                            self.log.error(self.log_msg("error grant connect on database {} to role {}"
                                                        .format(db_name, role_name)), e)
        finally:
            if conn:
                conn.close()

    def cancel(self):
        self.kill_processes()
        self._cancel_event.set()
        self.log.info(self.log_msg("Worker stopped"))

    def is_cancelled(self):
        return self._cancel_event.is_set()

    def on_cancel(self, database=None):
        if database:
            self.log.exception("Restore got canceled for database {0}".format(database))
            self.update_status('details', "Restore got canceled for database {0}".format(database), database)
            self.update_status('status', backups.BackupStatus.CANCELED, database)
        self.update_status('details', "Backup got canceled for database")
        self.update_status("status", backups.BackupStatus.CANCELED, flush=True)

    def kill_processes(self):
        if self.pg_restore_proc:
            self.log.info("kill restore process with pid: {}".format(self.pg_restore_proc.pid))
            self.pg_restore_proc.kill()

    def grant_connect_for_external_pg(self, roles_backup_path, db_name):
        with open(roles_backup_path, "r") as role_file:
            for line in role_file:
                if not line.startswith("CREATE ROLE"):
                    continue
                role_name = line.split('CREATE ROLE ')[1].split(';')[0]
                self.log.debug(self.log_msg("Try restore TABLE, SEQUENCE, VIEW owners to '%s' without superuser"
                                            " privileges." % role_name))
                con_properties = configs.connection_properties(database=db_name)
                conn = None
                try:
                    conn = psycopg2.connect(**con_properties)
                    conn.autocommit = True
                    with conn.cursor() as cur:
                        cur.execute(
                            "SELECT 'ALTER TABLE '||schemaname||'.'||tablename||' OWNER TO \"%(role)s\";' FROM pg_tables "
                            "WHERE NOT schemaname IN ('pg_catalog', 'information_schema')", {"role": AsIs(role_name)})
                        alter_roles = [r[0] for r in cur.fetchall()]
                        for alter_role in alter_roles:
                            self.log.debug(self.log_msg("Try execute command: %s" % alter_role))
                            try:
                                cur.execute(alter_role)
                            except Exception as e:
                                self.log.info(self.log_msg("ERROR ALTER TABLE. Command: {} "
                                                           "ERROR: {}".format(alter_role, e)))

                        cur.execute("SELECT 'ALTER SEQUENCE '||sequence_schema||'.'||sequence_name||' OWNER TO "
                                    "\"%(role)s\";' FROM information_schema.sequences WHERE NOT sequence_schema "
                                    "IN ('pg_catalog', 'information_schema')", {"role": AsIs(role_name)})
                        alter_sequences = [r[0] for r in cur.fetchall()]
                        for alter_sequence in alter_sequences:
                            self.log.debug(self.log_msg("Try execute command: %s" % alter_sequence))
                            try:
                                cur.execute(alter_sequence)
                            except Exception as e:
                                self.log.info(self.log_msg("ERROR ALTER SEQUENCE. Command: {} "
                                                           "ERROR: {}".format(alter_sequence, e)))

                        cur.execute("SELECT 'ALTER VIEW '||table_schema||'.'||table_name ||' OWNER TO \"%(role)s\";' "
                                    "FROM information_schema.views WHERE NOT table_schema "
                                    "IN ('pg_catalog', 'information_schema')", {"role": AsIs(role_name)})
                        alter_views = [r[0] for r in cur.fetchall()]
                        for alter_view in alter_views:
                            self.log.debug(self.log_msg("Try execute command: %s" % alter_view))
                            try:
                                cur.execute(alter_view)
                            except Exception as e:
                                self.log.info(self.log_msg("ERROR ALTER VIEW. Command: {} "
                                                           "ERROR: {}".format(alter_view, e)))
                        cur.execute("GRANT \"%(role)s\" TO \"%(admin)s\";", {"role": AsIs(role_name),
                                                                             "admin": AsIs(configs.postgresql_user())})
                        try:
                            cur.execute(
                                "ALTER ROLE \"%(role)s\" WITH LOGIN;", {"role": AsIs(role_name)})
                        except Exception as e:
                            self.log.info(self.log_msg("ERROR ALTER ROLE {} WITH LOGIN "
                                                       "ERROR: {}".format(role_name, e)))
                finally:
                    if conn:
                        conn.close()
            else:
                self.log.info(self.log_msg("Username not found in the file %s. "
                                           "ALTER tables OWNER TO  was not executed" % roles_backup_path))

    @staticmethod
    def drop_lookup_func_for_db(db_name):
        connection_properties = configs.connection_properties(database=db_name)
        conn = None
        try:
            conn = psycopg2.connect(**connection_properties)
            conn.autocommit = True
            with conn.cursor() as cur:
                cur.execute("drop function if exists lookup(name);")
        finally:
            if conn:
                conn.close()
