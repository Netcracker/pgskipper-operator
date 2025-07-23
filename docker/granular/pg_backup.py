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
import datetime
import glob
import logging
import os
import subprocess
import time
from threading import Thread, Event
from subprocess import Popen, PIPE
import psycopg2

import utils
import backups
import configs
import encryption
import storage_s3


class PostgreSQLDumpWorker(Thread):

    def __init__(self, databases, backup_request):
        Thread.__init__(self)

        self.log = logging.getLogger("PostgreSQLDumpWorker")

        self.backup_id = backup_request.get('backupId') or backups.generate_backup_id()
        self.name = self.backup_id
        self.namespace = backup_request.get('namespace') or configs.default_namespace()
        self.compression_level = backup_request.get('compressionLevel', 9)
        self.keep = backup_request.get('keep') or configs.default_backup_expiration_period()
        self.postgres_version = utils.get_version_of_pgsql_server()
        self.is_standard_storage = True if backup_request.get('externalBackupPath') is None else False
        self.location = configs.backups_storage(self.postgres_version) if self.is_standard_storage \
            else backups.build_external_backup_root(backup_request.get('externalBackupPath'))
        self.external_backup_root = None if self.is_standard_storage else self.location
        self.bin_path = configs.get_pgsql_bin_path(self.postgres_version)
        self.parallel_jobs = configs.get_parallel_jobs()
        self.databases = databases if databases else []
        self.backup_dir = backups.build_backup_path(self.backup_id, self.namespace, self.external_backup_root)
        self.create_backup_dir()
        self.s3 = storage_s3.AwsS3Vault() if os.environ['STORAGE_TYPE'] == "s3" else None
        self._cancel_event = Event()
        if configs.get_encryption():
            self.encryption = True
            self.key = encryption.KeyManagement.get_object().get_password()
            self.key_name = encryption.KeyManagement.get_key_name()
            self.key_source = encryption.KeyManagement.get_key_source()
        else:
            self.encryption = False
        self.status = {
            'backupId': self.backup_id,
            'namespace': self.namespace,
            'status': backups.BackupStatus.PLANNED
        }
        self.pg_dump_proc = None

        self.flush_status(self.external_backup_root)

    def cancel(self):
        self.kill_processes()
        self._cancel_event.set()
        self.log.info(self.log_msg("Worker stopped"))

    def is_cancelled(self):
        return self._cancel_event.is_set()

    def log_msg(self, msg):
        return "[backupId={}] {}".format(self.backup_id, msg)

    def kill_processes(self):
        if self.pg_dump_proc:
            self.log.info("kill backup process with pid: {}".format(self.pg_dump_proc.pid))
            self.pg_dump_proc.kill()

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

    def flush_status(self, external_backup_storage=None):
        path = backups.build_backup_status_file_path(self.backup_id, self.namespace, external_backup_storage)
        utils.write_in_json(path, self.status)
        if self.s3:
            try:
                # upload dumpfile
                self.s3.upload_file(path)
            except Exception as e:
                raise e

    def stderr_file(self, database):
        return '{}/{}.error'.format(self.backup_dir, database)

    def populate_databases_list(self):
        connection_properties = configs.connection_properties()
        conn = None
        try:
            conn = psycopg2.connect(**connection_properties)
            with conn.cursor() as cur:
                cur.execute("SELECT datname "
                            "FROM pg_database "
                            "WHERE datallowconn = 't' and "
                            "      datistemplate = 'f' and "
                            "      datname not in ({0})".format(",".join("'{0}'".format(x) for x in
                                                                         configs.protected_databases())))
                self.databases = [db[0] for db in cur]
        finally:
            if conn:
                conn.close()

    def get_included_extensions(self, database):
        connection_properties = configs.connection_properties()
        connection_properties['database'] = database
        conn = None
        try:
            conn = psycopg2.connect(**connection_properties)
            with conn.cursor() as cur:
                excluded_env = os.getenv("EXCLUDED_EXTENSIONS", "")
                excluded_extensions = [e.strip() for e in excluded_env.split(',') if e.strip()]

                # Always exclude pg_hint_plan
                if "pg_hint_plan" not in excluded_extensions:
                    excluded_extensions.append("pg_hint_plan")

                self.log.info(self.log_msg(f"Excluded extensions: {excluded_extensions}"))

                if not excluded_extensions:
                    self.log.warning(self.log_msg("No excluded extensions configured; all extensions will be included."))

                placeholders = ','.join(['%s'] * len(excluded_extensions))
                query = f"SELECT extname FROM pg_extension WHERE extname NOT IN ({placeholders})"
                cur.execute(query, excluded_extensions)

                included_extensions = [row[0] for row in cur]
                self.log.info(self.log_msg(f"Fetched included extensions for '{database}': {included_extensions}"))
                return included_extensions
        except Exception as e:
            raise backups.BackupFailedException(database, f"Failed to fetch included extensions: {e}")
        finally:
            if conn:
                conn.close()

    def backup_single_database(self, database):
        self.log.info(self.log_msg("Start processing database '{}'.".format(database)))
        self.log.info(self.log_msg("Will use binaries: '{}' for backup.".format(self.bin_path)))

        if database == 'postgres':
            raise backups.BackupFailedException(
                database, "Database 'postgres' is not suitable for "
                          "backup/restore since Patroni always keeps "
                          "connection to the database.")

        self.update_status('status', backups.BackupStatus.IN_PROGRESS, database)
        self.update_status('timestamp', int(time.time()), database)
        iso_date = datetime.datetime.fromtimestamp(self.status['timestamp']).isoformat()
        self.update_status('created', str(iso_date), database, flush=True)

        pg_dump_backup_path = backups.build_backup_path(self.backup_id, self.namespace, self.external_backup_root)

        # Some databases may contain special symbols like '=',
        # '!' and others, so use this WA.
        os.environ['PGDATABASE'] = database
        if configs.postgres_password():
         os.environ['PGPASSWORD'] = configs.postgres_password()

        #fix to exclude pg_hint_plan for azure pg
        #for pg17 use --exclude-extension with pg_dump
        extension_flags = []
        if configs.is_external_pg():
            included_exts = self.get_included_extensions(database)
            if included_exts:
            # Add each extension as a separate --extension flag
                for ext in included_exts:
                    extension_flags.extend(['--extension', ext])

        if int(self.parallel_jobs) > 1:
            command = ['{}/pg_dump'.format(self.bin_path),
                    '--format=directory',
                    '--file', os.path.join(pg_dump_backup_path, database),
                    '--user', configs.postgresql_user(),
                    '--host', configs.postgresql_host(),
                    '--port', configs.postgresql_port(),
                    # '--clean',
                    # '--create',
                    # '--if-exists',
                    '--blobs']

            command.extend(['-j', self.parallel_jobs])

            if configs.is_external_pg():
                command.extend(extension_flags)
                if self.postgres_version[0] < 15:
                    command.extend(['-T','cron.*'])

            # Zero is corner-case in Python :(
            if self.compression_level or self.compression_level == 0:
                command.extend(['-Z', str(self.compression_level)])


            with open(self.stderr_file(database), "w+") as stderr:
                start = time.time()
                if self.encryption:
                    pg_dump_proc = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=stderr)
                    openssl_proc = subprocess.Popen(
                        "openssl enc -aes-256-cbc -nosalt -pass pass:%s" % self.key,
                        stdin=pg_dump_proc.stdout, shell=True, stderr=stderr)
                    self.pg_dump_proc = openssl_proc
                    exit_code = openssl_proc.wait()
                else:
                    pg_dump_proc = subprocess.Popen(command, stderr=stderr)
                    self.pg_dump_proc = pg_dump_proc
                    exit_code = pg_dump_proc.wait()

                if exit_code != 0:
                    with open(self.stderr_file(database)) as f:
                        raise backups.BackupFailedException(database, '\n'.join(f.readlines()))

                self.pg_dump_proc = None


        else:
            command = ['{}/pg_dump'.format(self.bin_path),
                '--format=custom',
                '--user', configs.postgresql_user(),
                '--host', configs.postgresql_host(),
                '--port', configs.postgresql_port(),
                # '--clean',
                # '--create',
                # '--if-exists',
                '--blobs']

            if self.compression_level or self.compression_level == 0:
                command.extend(['-Z', str(self.compression_level)])

            if configs.is_external_pg():
                command.extend(extension_flags)
                if self.postgres_version[0] < 15:
                    command.extend(['-T','cron.*'])

            database_backup_path = backups.build_database_backup_path(self.backup_id, database,
                                                                  self.namespace, self.external_backup_root)

            with open(database_backup_path, 'w+') as dump, \
                    open(self.stderr_file(database), "w+") as stderr:
                start = time.time()
                # in case of encryption lets redirect output of pg_dump to openssl
                if self.encryption:
                    pg_dump_proc = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=stderr)
                    openssl_proc = subprocess.Popen(
                        "openssl enc -aes-256-cbc -nosalt -pass pass:%s" % self.key,
                        stdin=pg_dump_proc.stdout, stdout=dump, shell=True, stderr=stderr)
                    self.pg_dump_proc = openssl_proc
                    exit_code = openssl_proc.wait()
                else:
                    pg_dump_proc = subprocess.Popen(command, stdout=dump, stderr=stderr)
                    self.pg_dump_proc = pg_dump_proc
                    exit_code = pg_dump_proc.wait()

                if exit_code != 0:
                    with open(self.stderr_file(database)) as f:
                        raise backups.BackupFailedException(database, '\n'.join(f.readlines()))

                self.pg_dump_proc = None
        if self.s3:
            try:
                # upload dumpfile
                self.s3.upload_file(database_backup_path)
                # upload errorfile
                self.s3.upload_file(self.stderr_file(database))
            except Exception as e:
                raise e

        self.update_status('duration', (int(time.time() - start)), database)
        owner = utils.get_owner_of_db(database)
        self.log.info(self.log_msg("owner of database '%s'." % owner))
        self.update_status('owner', owner, database)

        roles = self.fetch_roles(database)
        self.dump_roles_for_db(roles, database)
        self.update_status('duration', (int(time.time() - start)), database)
        self.log.info(self.log_msg("Finished processing of database '%s'." % database))
        if self.s3:
            os.remove(database_backup_path)

    def fetch_roles(self, database):
        connection_properties = configs.connection_properties()
        rolesList = []
        conn = None
        try:
            conn = psycopg2.connect(**connection_properties)
            conn.autocommit = True
            with conn.cursor() as cur:
                # check if there are any active connections to pgsql
                cur.execute("SELECT pid "
                            "FROM pg_stat_activity "
                            "WHERE pid <> pg_backend_pid() "
                            "      AND datname = %s LIMIT 1", (database,))
                pids = [p[0] for p in cur.fetchall()]

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
                    AND db.privilege_type = 'CONNECT' AND r2.rolname not in ('postgresadmin');
                """, (database,))
                rolesList = [p[0] for p in cur.fetchall()]
        finally:
            if conn:
                conn.close()

        self.log.debug(self.log_msg("Roles {} have been fetched for backup ".format(rolesList)))

        roles_backup_path = backups.build_roles_backup_path(self.backup_id, database,
                                                            self.namespace, self.external_backup_root)
        database_backup_path = backups.build_database_backup_path(self.backup_id, database,
                                                                  self.namespace, self.external_backup_root)

        pg_dump_backup_path = backups.build_backup_path(self.backup_id, self.namespace, self.external_backup_root)
        path_for_parallel_flag_backup = os.path.join(pg_dump_backup_path, database)

        self.log.debug("Will try to fetch users to %s" % roles_backup_path)
        with open(self.stderr_file(database), "w+") as stderr:
            fetch_command = \
                "| grep -P 'ALTER TABLE.*OWNER TO.*' | " \
                "awk 'NF>1{print  substr($NF, 1, length($NF)-1)}' | uniq"
            if self.encryption:
                encrypt = "openssl enc -aes-256-cbc -nosalt -d -pass " \
                    "pass:'%s' < '%s' | %s/pg_restore " % \
                          (self.key, database_backup_path, self.bin_path)
                fetch_command = encrypt + fetch_command
            else:
                if int(self.parallel_jobs) > 1:
                    dump_version = self.get_pg_version_from_dump(path_for_parallel_flag_backup)
                else:    
                    dump_version = self.get_pg_version_from_dump(database_backup_path)

                pg_restore_options = "-f - "
                if dump_version[0] == 9 and dump_version[1] == 4:
                    pg_restore_options = ""
                
                if int(self.parallel_jobs) > 1:
                    pg_restore_options += " -j {} ".format(self.parallel_jobs)
                    fetch_command = ("%s/pg_restore '%s' %s" + fetch_command) % \
                                    (self.bin_path, path_for_parallel_flag_backup, pg_restore_options)
                else:
                    fetch_command = ("%s/pg_restore '%s' %s" + fetch_command) % \
                                    (self.bin_path, database_backup_path, pg_restore_options)
            self.log.debug("Roles fetch command: %s." % fetch_command)
            p = Popen(fetch_command, shell=True, stdout=PIPE, stderr=stderr)
            output, err = p.communicate()
            exit_code = p.returncode
            self.log.info("Roles search result: {} type: {} . Exit code: {}".format(output, type(output), exit_code))
            if exit_code != 0:
                raise backups.BackupFailedException(database, '\n'.join(
                    stderr.readlines()))
            rolesFromRestore = [x for x in output.decode().split("\n") if x.strip()]
            rolesList = list(set(rolesList + rolesFromRestore))
            roles = "|".join(list(
                [x for x in rolesList if x not in configs.protected_roles()]))
            self.log.debug("Selected roles template: %s " % roles)
            return roles

    def dump_roles_for_db(self, roles, database):
        roles_backup_path = backups.build_roles_backup_path(self.backup_id, database,
                                                            self.namespace, self.external_backup_root)

        with open(roles_backup_path, 'w+') as dump, \
                open(self.stderr_file(database), "w+") as stderr:
            if roles:
                cmd = "{}/pg_dumpall --roles-only -U {} --host {} --port {} {}" \
                      "| grep -P '{}' ".format(self.bin_path,
                                           configs.postgresql_user(),
                                           configs.postgresql_host(),
                                           configs.postgresql_port(),
                                           configs.postgresql_no_role_password_flag(),
                                           roles
                                           )
                                                           
                if self.encryption:
                    encrypt_cmd = \
                        "| openssl enc -aes-256-cbc -nosalt -pass" \
                        " pass:'{}'".format(self.key)
                    cmd = cmd + encrypt_cmd
                p = Popen(cmd, shell=True, stdout=dump, stderr=stderr)
                output, err = p.communicate()
                self.log.debug("Fetch roles command: {}".format(cmd))
                exit_code = p.returncode
                if exit_code != 0:
                    # stderr.seek(0)
                    raise backups.BackupFailedException(
                        database, '\n'.join(stderr.readlines()))
            else:
                self.log.info("No roles to fetch")
            if self.s3:
                try:
                    logging.info("Streaming {} roles to AWS".format(database))
                    self.s3.upload_file(roles_backup_path)
                except Exception as e:
                    raise e
                finally:
                    os.remove(roles_backup_path)

    def cleanup(self, database):
        if self.s3:
            self.s3.delete_file(self.stderr_file(database))

        os.remove(self.stderr_file(database))

    def on_success(self, database):
        database_backup_path = backups.build_database_backup_path(self.backup_id, database, self.namespace, self.external_backup_root)

        pg_dump_backup_path = backups.build_backup_path(self.backup_id, self.namespace, self.external_backup_root)
        path_for_parallel_flag_backup = os.path.join(pg_dump_backup_path, database)
        
        if self.s3:
            if int(self.parallel_jobs) > 1:
                size_bytes = self.s3.get_file_size(path_for_parallel_flag_backup)
            else:
                size_bytes = self.s3.get_file_size(database_backup_path)
        else:
            if int(self.parallel_jobs) > 1:
                size_bytes = os.path.getsize(path_for_parallel_flag_backup)
            else:
                size_bytes = os.path.getsize(database_backup_path)
        self.update_status('path', backups.
                           build_database_backup_full_path(
            self.backup_id, database, self.location, self.namespace), database)
        self.update_status('sizeBytes', size_bytes, database)
        self.update_status('size', backups.sizeof_fmt(size_bytes), database)
        if self.encryption:
            self.update_status('key_name', self.key_name)
            self.update_status('key_source', self.key_source)
        self.update_status(
            'status', backups.BackupStatus.SUCCESSFUL, database, flush=True
        )

    def on_failure(self, database, e):
        self.log.exception("Failed to backup database {0} {1}.".format(database, str(e)))
        self.update_status('details', str(e), database)
        self.update_status('status', backups.BackupStatus.FAILED, database)

        for f in glob.glob(self.backup_dir + '/*.dump'):
            os.remove(f)

    def on_cancel(self, database=None):
        if database:
            self.log.exception("Backup got canceled for database {0}".format(database))
            self.update_status('details', "Backup got canceled for database {0}".format(database), database)
            self.update_status('status', backups.BackupStatus.CANCELED, database)
        self.update_status('details', "Backup got canceled for database")
        self.update_status("status", backups.BackupStatus.CANCELED, flush=True)

    def expire(self, start_timestamp=None, keep=configs.default_backup_expiration_period()):

        if not start_timestamp:
            start_timestamp = int(time.time())

        if keep.lower() == 'forever':
            self.update_status('expires', 'Never')
            self.update_status('expirationDate', 'Never', flush=True)
        else:
            expiration_timestamp = backups.calculate_expiration_timestamp(start_timestamp, keep)
            self.update_status('expires', expiration_timestamp)
            self.update_status('expirationDate', str(datetime.datetime.fromtimestamp(expiration_timestamp).isoformat()), flush=True)

    def process_backup_request(self):
        self.update_status('status', backups.BackupStatus.IN_PROGRESS, flush=True)
        self.log.info(self.log_msg("Backup request processing has been started. Databases to backup: '{}'.".format(self.databases)))

        start_timestamp = int(time.time())
        self.expire(keep=self.keep)
        self.update_status('timestamp', start_timestamp)
        self.update_status('created', str(datetime.datetime.fromtimestamp(start_timestamp).isoformat()))

        for database in self.databases:
            if backups.is_database_protected(database):
                return "Database '{}' is not suitable for backup/restore.".format(database, http.client.FORBIDDEN)
            #if self.should_be_skipped(database):
            #    self.log.info("Skipping Logical Database: {}, because it's not suitable for the backup.".format(database))
            try:
                self.backup_single_database(database)
                self.on_success(database)
            except Exception as e:  # Call on_failure here to mark database backup failed on any exception.
                self.on_failure(database, e)
                raise e
            finally:
                self.cleanup(database)

    def should_be_skipped(self, database):
        # if not external, shouldn't skip
        if not configs.is_external_pg():
            return False

        # if external check for _DBAAS_METADATA table presence
        # establish connect to logical db and check if metadata presented
        connection_properties = configs.connection_properties(database=database)
        conn = None
        try:
            conn = psycopg2.connect(**connection_properties)
            with conn.cursor() as cur:
                cur.execute("select 1 from pg_tables where upper(tablename) = '_DBAAS_METADATA'")
                return cur.fetchone() == None
        finally:
            if conn:
                conn.close()

    def create_backup_dir(self):
        if not os.path.exists(self.backup_dir):
            os.makedirs(self.backup_dir)

    def run(self):
        try:
            if not self.databases:
                self.log.info(self.log_msg("No databases specified for backup. "
                                           "According to the contract, all databases will be backup'ed."))
                self.populate_databases_list()

            self.process_backup_request()
            self.update_status('status', backups.BackupStatus.SUCCESSFUL, flush=True)
            self.log.info(self.log_msg("Backup request processing has been completed."))
        except Exception as e:
            self.log.exception(self.log_msg("Backup request processing has failed."))
            self.update_status('details', str(e))
            self.update_status('status', backups.BackupStatus.FAILED, flush=True)
            self.expire()
            raise e
        finally:
            if self.is_cancelled():
                self.on_cancel()

    def get_pg_version_from_dump(self, database_backup_path):
        return utils.get_pg_version_from_dump(database_backup_path, self.key_name if self.encryption else None, self.bin_path)

