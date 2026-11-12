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

import logging
import time
from datetime import datetime
from io import StringIO

import os

import storage
from storage import VAULT_NAME_FORMAT, StorageLocationAlreadyExistsException
import fsutil
import requests


class BackRestStorage(storage.Storage):
    __log = logging.getLogger("PgBackRestFS")

    def __init__(self, root):
        self.__log.info("Init storage object with storage root: %s" % root)
        self.root = root

    def list(self):

        response = requests.get("http://backrest-headless:3000/list").json()
        print(response)
        vault = []
        for backup in response:
            annotation = backup.get("annotation") or {}
            if annotation.get("timestamp"):
                vault.append(BackRestVault(annotation["timestamp"], backup.get("error")))
            else:
                self.__log.warning("Found backup without timestamp in annotation: %s", backup)
        return vault

    def archive_size(self):
        """Returns whole storage size in bytes"""
        return 0

    def fs_space(self):
        """Returns tuple (free, total) space on mount point where is root folder located"""
        return (1, 1)

    def get_type(self):
        return "PgBackRest"

    def get_type_id(self):
        return 2

    def open_vault(self, backup_id):
        """

        :return:
        :rtype: (str, dict, StringIO)
        """
        vault = BackRestVault("%s/%s" % (self.root, backup_id))
        print(f"RETURNING VAULT {vault}")
        return vault

    def prot_get_as_stream(self, filename):
        pass

    def prot_get_file_size(self, filename):
        pass

    def prot_is_file_exists(self, filename):
        pass

    def prot_list_archive(self):
        pass

    def prot_put_as_stream(self, filename, stream):
        pass

    def size(self):
        return 0

    def evict_vault(self, vault):
        self.__log.info("Evict vault: %s" % vault)
        self.__log.debug("Delete folder: %s" % vault.folder)
        for root, dirs, files in os.walk(vault.folder, topdown=False):
            for name in files:
                try:
                    os.remove(os.path.join(root, name))
                except OSError as e:  # passing possible problems with permission
                    self.__log.exception(e)
            for name in dirs:
                try:
                    os.rmdir(os.path.join(root, name))
                except OSError as e:
                    self.__log.exception(e)
        try:
            os.rmdir(vault.folder)
        except OSError as e:
            self.__log.exception(e)

    def get_encryption(self):
        pass

    def prot_delete(self, filename):
        pass


class BackRestVault(storage.Vault):
    __log = logging.getLogger("BackRestVault")

    def __init__(self, timestamp="", is_failed=False):
        super(BackRestVault, self).__init__()

        self.folder = timestamp
        self.metrics_filepath = self.folder + "/.metrics"
        self.console = StringIO()
        self._failed = bool(is_failed)

        print(f"INIT VAULT {self.folder, self.metrics_filepath, self.console, self._failed}")

    def get_id(self):
        return os.path.basename(self.folder)

    def get_folder(self):
        pass

    #
    #
    def __lock_filepath(self):
        return self.folder + ".lock"

    #
    # def __failed_filepath(self):
    #     return self.folder + ".failed"
    #
    # def __metrics_filepath(self):
    #     return self.folder + ".metrics"
    #
    # def __console_filepath(self):
    #     return self.folder + ".console"
    #
    # def __is_file_exists(self, path):
    #     return subprocess.call(["/opt/backup/scli", "get", path], env=scli_env) == "Object Not Found"
    #
    # def __backup_archive_file_path(self):
    #     return "{}/pg_{}_backup_{}.tar.gz".format(self.get_folder(), PG_CLUSTER_NAME, self.get_id())
    #
    # def is_locked(self):
    #     if self.cache_state:
    #         if not self.cached_state:
    #             self.__cache_current_state()
    #         return self.cached_state["is_locked"]
    #     return self.__is_file_exists("{}/{}".format(CONTAINER, self.__lock_filepath()))
    #

    def is_failed(self):
        return self._failed

    #

    def is_done(self):
        return not self._failed

    #

    def is_back_up_archive_exists(self):
        return True

    def is_locked(self):
        # TODO: work with root object from pgbackrest
        return False

    def load_metrics(self):
        return {}

    def __enter__(self):
        self.__log.info("Init next vault: %s" % self.folder)
        super(BackRestVault, self).__enter__()

        if not os.path.exists(self.folder):
            os.makedirs(self.folder)
        else:
            raise StorageLocationAlreadyExistsException("Destination backup folder already exists: %s" % self.folder)

        self.__log.info("Create .lock file in vault: %s" % self.folder)
        fsutil.touch(self.__lock_filepath())

        return self.folder, self.metrics, self.console

    def create_time(self):
        foldername = self.get_id()

        # If foldername has the 'backup-' prefix, return a default time
        if foldername.startswith("backup-"):
            return time.time()  # Returning the current timestamp as a fallback

        try:
            d = datetime.strptime(foldername, VAULT_NAME_FORMAT)
            return time.mktime(d.timetuple())
        except ValueError as e:
            self.__log.error(
                "Failed to parse datetime from foldername: %s, error: %s",
                foldername,
                str(e),
            )
            return time.time()  # Returning the current timestamp as a fallback
