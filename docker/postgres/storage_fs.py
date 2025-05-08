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

import io

import errno
import os
from datetime import datetime
import logging
from traceback import format_exception
import time
import json
import fsutil
import storage
from storage import VAULT_NAME_FORMAT, VAULT_DIRNAME_MATCHER, ARCHIVE_NAME_MATCHER, \
    StorageLocationAlreadyExistsException
import utils
import encryption

try:
    from io import StringIO
except ImportError:
    from io import StringIO

PG_CLUSTER_NAME = os.getenv("PG_CLUSTER_NAME")
STORAGE_ROOT = os.getenv("STORAGE_ROOT")


class FSStorage(storage.Storage):
    __log = logging.getLogger("FSStorage")

    def __failed_filepath(self):
        return ".failed"

    def __init__(self, root):
        self.__log.info("Init storage object with storage root: %s" % root)
        self.root = root
        if utils.get_encryption():
            self.encryption = True
        else:
            self.encryption = False

    def fail(self, backup_id):
        self.__log.info("create .failed file: {}".format(os.path.join(self.root, backup_id, self.__failed_filepath())))
        open(os.path.join(self.root, backup_id, self.__failed_filepath()), "w").close()

    def get_encryption(self):
        return self.encryption

    def list(self):
        if os.path.exists(self.root):
            vaults = [FSVault(self.root + "/" + dirname)
                      for dirname in os.listdir(self.root)
                      if VAULT_DIRNAME_MATCHER.fullmatch(dirname) is not None]
            vaults.sort(key=lambda v: v.create_time())
            return vaults
        else:
            return []

    def size(self):
        """ Returns whole storage size in bytes """
        return fsutil.get_folder_size(self.root)

    def archive_size(self):
        """ Returns whole storage size in bytes """
        return fsutil.get_folder_size(os.path.join(self.root, "archive"))

    def fs_space(self):
        """ Returns tuple (free, total) space on mount point where is root folder located """
        return fsutil.get_fs_space(self.root)

    def open_vault(self, backup_id):
        """
        :return:
        :rtype: (str, dict, StringIO)
        """
        return FSVault("%s/%s" % (self.root, backup_id))

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

    def prot_is_file_exists(self, filename):
        self.__log.debug("Check for file: %s" % filename)
        return os.path.isfile("{}/{}".format(self.root, filename))

    def prot_delete(self, filename):
        self.__log.info("Delete file: %s" % filename)
        os.remove("{}/{}".format(self.root, filename))

    def prot_get_as_stream(self, filename):
        """
        :param filename: path to file from backup root.
        :type filename: string
        :return: stream with requested file
        :rtype: io.RawIOBase
        """
        self.__log.info("Get request for file: %s" % filename)
        full_file_path = "{}/{}".format(self.root, filename)

        return encryption.FileWrapper(full_file_path, self.encryption) \
            .get_file_stream()

    def prot_put_as_stream(self, filename, stream):
        """
        :param filename: 
        :type filename: string
        :param stream:
        :type stream: io.RawIOBase
        :return: sha256 of processed stream
        :rtype: string
        """
        self.__log.info("Put request for file: %s" % filename)
        fs_filename = "{}/{}".format(self.root, filename)
        if not os.path.exists(os.path.dirname(fs_filename)):
            try:
                os.makedirs(os.path.dirname(fs_filename))
            except OSError as exc:
                if exc.errno != errno.EEXIST:
                    raise
        return encryption.FileWrapper(fs_filename, self.encryption) \
            .put_file_stream(stream)

    def prot_get_file_size(self, filename):
        # self.__log.info("Size request for file: %s" % filename)
        if os.path.exists("{}/{}".format(self.root, filename)):
            return os.path.getsize("{}/{}".format(self.root, filename))
        return 0

    def prot_list_archive(self):
        archive_root = os.path.join(self.root, "archive")
        if os.path.exists(archive_root):
            self.__log.debug("Archive directory listing {}".format(os.listdir(archive_root)))
            archives = [storage.Archive(
                ARCHIVE_NAME_MATCHER.match(filename).group("name"),
                os.path.getmtime(os.path.join(archive_root, filename)) * 1000)
                for filename in os.listdir(archive_root)
                if ARCHIVE_NAME_MATCHER.match(filename) is not None]
            return archives
        else:
            self.__log.debug("Archive directory does not exist")
            return []

    def get_type(self):
        return "Volume"

    def get_type_id(self):
        return 0


class FSVault(storage.Vault):
    __log = logging.getLogger("FSVaultLock")

    def __init__(self, folder):
        super(FSVault, self).__init__()

        self.folder = folder
        self.metrics_filepath = self.folder + "/.metrics"
        self.console = StringIO()

    def get_id(self):
        return os.path.basename(self.folder)

    def get_folder(self):
        return self.folder

    def load_metrics(self):
        self.__log.debug("Load metrics from: %s" % self.metrics_filepath)

        if not os.path.isfile(self.metrics_filepath):
            self.__log.warning("Metrics file: {} does not exists.".format(self.metrics_filepath))
            return {}

        with open(self.metrics_filepath, "r") as f:
            try:
                return json.load(f)
            except Exception as e:
                self.__log.exception(e)
                self.__log.warning("Cannot load metrics file: {}. Metrics file can be damaged.".format(self.metrics_filepath))
                return {}

    def __lock_filepath(self):
        return self.folder + "/.lock"

    def __failed_filepath(self):
        return self.folder + "/.failed"

    def __console_filepath(self):
        return self.folder + "/.console"

    def __metrics_filepath(self):
        return self.folder + "/.metrics"

    def is_locked(self):
        return os.path.exists(self.__lock_filepath())

    def is_failed(self):
        return os.path.exists(self.__failed_filepath())

    def is_done(self):
        if not os.path.isfile(self.__metrics_filepath()):
            return False
        with open(self.__metrics_filepath(), "r") as f:
            j = json.load(f)
        return j['exit_code'] == 0

    def is_back_up_archive_exists(self):
        # for pg10 backups there is another naming convention `pg_backup_{timestamp}`
        return os.path.exists(self.folder + "/pg_{}_backup_{}.tar.gz".format(PG_CLUSTER_NAME, self.get_id())) \
               or os.path.exists(self.folder + "/pg_backup_{}.tar.gz".format(self.get_id())) \
               or os.path.exists(self.folder + "/pg_{}_backup_{}_enc.tar.gz".format(PG_CLUSTER_NAME, self.get_id())) \
               or os.path.exists(self.folder + "/pg_backup_{}_enc.tar.gz".format(self.get_id()))

    def __enter__(self):
        self.__log.info("Init next vault: %s" % self.folder)
        super(FSVault, self).__enter__()

        if not os.path.exists(self.folder):
            os.makedirs(self.folder)
        else:
            raise StorageLocationAlreadyExistsException("Destination backup folder already exists: %s" % self.folder)

        self.__log.info("Create .lock file in vault: %s" % self.folder)
        fsutil.touch(self.__lock_filepath())

        return self.folder, self.metrics, self.console

    def create_time(self):
        foldername = os.path.basename(self.folder)
        d = datetime.strptime(foldername, VAULT_NAME_FORMAT)
        return time.mktime(d.timetuple())

    def __exit__(self, tpe, exception, tb):
        self.__log.info("Close vault")
        self.__log.info("Save metrics to: %s" % self.metrics_filepath)

        super(FSVault, self).__exit__(tpe, exception, tb)

        self.metrics["size"] = fsutil.get_folder_size(self.folder)

        if exception:
            fsutil.touch(self.__failed_filepath())
            e = "\n".join(format_exception(tpe, exception, tb))
            self.__log.info("Don't remove vault .lock due exception in nested code")
            self.__log.debug("Something wrong happened inside block uses vault: " + e)
            self.metrics["exception"] = e
            self.fail()

        with open(self.metrics_filepath, "w") as f:
            json.dump(self.metrics, f)

        console_logs = self.console.getvalue()
        self.console.close()
        with open(self.__console_filepath(), "w") as f:
            f.write(console_logs)

        self.__log.info("Remove lock for %s" % self.get_id())
        os.unlink(self.__lock_filepath())

    def __repr__(self):
        return "Vault(%s)" % os.path.basename(self.folder)

    def fail(self):
        open(self.__failed_filepath(), "w").close()