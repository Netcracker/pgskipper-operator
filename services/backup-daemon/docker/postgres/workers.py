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

import eviction
import locks
import utils
import logging
import time
import encryption

from multiprocessing import Process


class BackupProcessException(Exception):
    pass


class PostgreSQLBackupWorker(Process):
    def __init__(self, storage, backup_command, eviction_rule, backup_id):
        Process.__init__(self)
        self.__log = logging.getLogger("PostgreSQLBackupWorker")

        self.__storage = storage
        self.__backup_command = backup_command
        self.__eviction_rule = eviction_rule
        self.__backup_id = backup_id

        if utils.get_encryption():
            self.encryption = True
            self.key = encryption.KeyManagement.get_object().get_password()
            self.key_name = encryption.KeyManagement.get_key_name()
            self.key_source = encryption.KeyManagement.get_key_source()
            self.__backup_command = backup_command + " %(key)s"
        else:
            self.encryption = False

    def run(self):
        self.__perform_database_backup()
        self.__cleanup_storage()

    def __perform_database_backup(self):
        with self.__storage.open_vault(self.__backup_id) as (vault_folder, metrics, console):
            backup_id = self.__backup_id.split('/')[-1]

            self.__log.info("[backup-id=%s] Start new backup streaming." % backup_id)
            locks.update_lock_file(backup_id=backup_id)

            cmd_options = {"data_folder": vault_folder}
            if self.encryption:
                cmd_options["key"] = self.key

            cmd_processed = self.__split_command_line(self.__backup_command % cmd_options)
            if not self.encryption:
                self.__log.info("Run cmd template: %s\n\toptions: %s\n\tcmd: [%s]" % (
                    self.__backup_command, str(cmd_options), ", ".join(cmd_processed)))
            else:
                self.__log.info("Run cmd template %s\n" % (self.__backup_command))
            import subprocess
            process = subprocess.Popen(cmd_processed, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
            while True:
                output = process.stdout.readline()
                if output.decode() == '':
                    if process.poll() is not None:
                        break
                    else:
                        time.sleep(0.1)
                if output:
                    print((output.decode().strip()))
                    console.write(output.decode().strip())
            exit_code = process.poll()
            metrics["exit_code"] = exit_code
            if self.encryption:
                metrics["key_name"] = self.key_name
                metrics["key_source"] = self.key_source

            if exit_code != 0:
                msg = "[backup-id=%s] Execution of '%s' was finished with non zero exit code: %d" % (backup_id, cmd_processed, exit_code)
                self.__log.error(msg)
                raise BackupProcessException(msg)
            else:
                self.__log.info("[backup-id=%s] Backup streaming has been successfully finished." % backup_id)

    def __cleanup_storage(self):
        self.__log.info("Start eviction process by policy: %s" % self.__eviction_rule)
        outdated_vaults = eviction.evict(self.__storage.list(), self.__eviction_rule,
                                         accessor=lambda x: x.create_time())
        if len(outdated_vaults) > 0:
            for vault in outdated_vaults:
                self.__storage.evict(vault)
        else:
            self.__log.info("No obsolete vaults to evict")

    def fail(self):
        self.__log.warning("Set failed status for backup")
        self.__storage.fail(self.__backup_id)

    @staticmethod
    def __split_command_line(cmd_line):
        import shlex
        lex = shlex.shlex(cmd_line)
        lex.quotes = '"'
        lex.whitespace_split = True
        lex.commenters = ''
        return list(lex)


def spawn_backup_worker(storage, backup_command, eviction_rule, backup_id):
    process = PostgreSQLBackupWorker(storage, backup_command, eviction_rule, backup_id)
    process.start()
    return process


def spawn_worker(runnable, *args):
    process = Process(target=runnable, args=args)
    process.start()
    return process
