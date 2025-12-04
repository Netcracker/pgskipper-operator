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
import os
import fsutil

from retrying import retry


class BackupInProgressLock:
    __in_progress_lock_file_path = '/tmp/backup.progress'

    def __init__(self):
        self.__log = logging.getLogger(self.__class__.__name__)

    @retry(wait_fixed=300000)  # Wait for 300 seconds before retry.
    def acquire_lock(self):
        # could not lock the resource
        #if not self.__lock.acquire(False):
        #    self.__log.info("New backup can not be started, because last backup is still in progress.")
        #    raise Exception
        #else:
        #    result = self.__lock.acquire()
        self.__log.info("Backup lock has been acquired. File created")
        if not os.path.isfile(self.get_lock_file_path()):
            fsutil.touch(self.get_lock_file_path())
        with open(self.get_lock_file_path(), "w+") as lock_file:
            lock_details = {
                'lock_acquisition_time': datetime.datetime.now().isoformat()
            }
            lock_file.write(json.dumps(lock_details))

    def release_lock(self):
        os.remove(self.get_lock_file_path())
        self.__log.info("Backup lock has been released.")

    @staticmethod
    def get_lock_file_path():
        return BackupInProgressLock.__in_progress_lock_file_path


def backup_lock():
    return BackupInProgressLock()


def get_backup_lock_file_path():
    return BackupInProgressLock.get_lock_file_path()


def update_lock_file(**kwargs):
    with open(get_backup_lock_file_path(), "r") as f:
        details = json.load(f)

    details.update(kwargs)
    temp_lock = '%s.tmp' % get_backup_lock_file_path()
    with open(temp_lock, "w") as f:
        json.dump(details, f)

    os.rename(temp_lock, get_backup_lock_file_path())
