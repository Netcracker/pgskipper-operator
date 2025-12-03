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

import hashlib
import io
import json
import logging
import time
from datetime import datetime
from traceback import format_exception

import os

import storage
from storage import VAULT_NAME_FORMAT, StorageLocationAlreadyExistsException

try:
    from io import StringIO
except ImportError:
    from io import StringIO

import subprocess

CONTAINER = os.getenv("CONTAINER")
CONTAINER_SEG = "{}_segments".format(CONTAINER)
PG_CLUSTER_NAME = os.getenv("PG_CLUSTER_NAME")
scli_env = os.environ.copy()
scli_env["ST_AUTH"] = os.getenv("SWIFT_AUTH_URL")
scli_env["ST_USER"] = os.getenv("SWIFT_USER")
scli_env["ST_KEY"] = os.getenv("SWIFT_PASSWORD")
scli_env["ST_TENANT"] = os.getenv("TENANT_NAME")


class SwiftStorage(storage.Storage):
    __log = logging.getLogger("SwiftStorage")

    def __init__(self, root):
        self.__log.info("Init storage object with storage root: %s" % root)
        self.root = root

    def list(self):
        swift_vault_listing = subprocess.check_output(["/opt/backup/scli", "ls", CONTAINER], env=scli_env)
        vaults = [SwiftVault(os.path.basename(backup_name[0:(len(backup_name) - len(os.path.basename(backup_name)) - 1)])
                             , cache_state=True
                             ,swift_vault_listing=swift_vault_listing)
                  for backup_name in swift_vault_listing.split("\n")
                  if backup_name.endswith(".tar.gz")]
        vaults.sort(key=lambda v: v.create_time())
        return vaults

    def size(self):
        """ Returns whole storage size in bytes """
        return 0

    def archive_size(self):
        """ Returns whole storage size in bytes """
        return 0

    def fs_space(self):
        """ Returns tuple (free, total) space on mount point where is root folder located """
        return (1, 1)

    def open_vault(self):
        """
        
        :return:
        :rtype: (str, dict, StringIO)
        """
        return SwiftVault("%s" % (datetime.now().strftime(VAULT_NAME_FORMAT)))

    def evict(self, vault):
        self.__log.info("Evict vault: %s" % vault)
        backup_name = "{}/pg_{}_backup_{}.tar.gz".format(vault.get_folder(), PG_CLUSTER_NAME, vault.get_id())
        subprocess.check_call(["/opt/backup/scli", "delete", "{}/{}".format(CONTAINER, backup_name)], env=scli_env)
        subprocess.call(["/opt/backup/scli", "delete", "{}/{}.lock".format(CONTAINER, vault.get_folder())], env=scli_env)
        subprocess.call(["/opt/backup/scli", "delete", "{}/{}.failed".format(CONTAINER, vault.get_folder())], env=scli_env)
        subprocess.call(["/opt/backup/scli", "delete", "{}/{}.console".format(CONTAINER, vault.get_folder())], env=scli_env)
        subprocess.check_call(["/opt/backup/scli", "delete", "{}/{}.metrics".format(CONTAINER, vault.get_folder())], env=scli_env)

    def prot_is_file_exists(self, filename):
        self.__log.info("File existence check for: %s" % filename)
        swift_vault_listing = subprocess.check_output(["/opt/backup/scli", "ls", CONTAINER], env=scli_env)
        return filename in swift_vault_listing

    def prot_delete(self, filename):
        self.__log.info("Delete file: %s" % filename)
        subprocess.check_call(["/opt/backup/scli", "delete", "{}/{}".format(CONTAINER, filename)], env=scli_env)

    def prot_get_as_stream(self, filename):
        """
        :param filename: path to file from backup root.
        :type filename: string
        :return: stream with requested file
        :rtype: io.RawIOBase
        """
        self.__log.info("Get request for file: %s" % filename)
        process = subprocess.Popen(["/opt/backup/scli", "get", "{}/{}".format(CONTAINER, filename)], env=scli_env, stdout=subprocess.PIPE)
        return process.stdout

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
        sha256 = hashlib.sha256()
        chunk_size = 4096
        process = subprocess.Popen(["/opt/backup/scli", "put", "{}/{}".format(CONTAINER, filename)], env=scli_env, stdout=subprocess.PIPE, stdin=subprocess.PIPE)

        while True:
            data = stream.read(chunk_size)
            if len(data) == 0:
                stream.close()
                self.__log.info("Processed stream with sha256 {}".format(sha256.hexdigest()))
                return sha256.hexdigest()
            sha256.update(data)
            process.stdin.write(data)

    def get_type(self):
        return "Swift"

    def get_type_id(self):
        return 2


class SwiftVault(storage.Vault):
    __log = logging.getLogger("SwiftVaultLock")

    def __init__(self, folder, cache_state=False, swift_vault_listing=None):
        super(SwiftVault, self).__init__()

        self.folder = folder
        self.console = None
        self.cache_state = cache_state
        self.cached_state = {}
        self.swift_vault_listing = swift_vault_listing

    def __cache_current_state(self):
        if self.cache_state:
            swift_vault_listing = self.swift_vault_listing if self.swift_vault_listing else subprocess.check_output(["/opt/backup/scli", "ls", CONTAINER], env=scli_env)
            self.cached_state["is_locked"] = self.__lock_filepath() in swift_vault_listing
            self.cached_state["is_failed"] = self.__failed_filepath() in swift_vault_listing

    def get_id(self):
        return os.path.basename(self.folder)

    def get_folder(self):
        return self.folder

    def __load_metrics_from_swift(self):
        try:
            return json.loads(subprocess.check_output([
                "/opt/backup/scli",
                "get",
                "{}/{}".format(CONTAINER, self.__metrics_filepath())], env=scli_env))
        except Exception:
            self.__log.warning("Cannot load metrics from swift. Backup can be damaged")
            return {}

    def load_metrics(self):
        self.__log.debug("Load metrics from: %s" % self.__metrics_filepath())
        if self.cache_state:
            if "metrics" not in self.cached_state:
                self.cached_state["metrics"] = self.__load_metrics_from_swift()
            return self.cached_state["metrics"]
        else:
            return self.__load_metrics_from_swift()


    def __lock_filepath(self):
        return self.folder + ".lock"

    def __failed_filepath(self):
        return self.folder + ".failed"

    def __metrics_filepath(self):
        return self.folder + ".metrics"

    def __console_filepath(self):
        return self.folder + ".console"

    def __is_file_exists(self, path):
        return subprocess.call(["/opt/backup/scli", "get", path], env=scli_env) == "Object Not Found"

    def __backup_archive_file_path(self):
        return "{}/pg_{}_backup_{}.tar.gz".format(self.get_folder(), PG_CLUSTER_NAME, self.get_id())

    def is_locked(self):
        if self.cache_state:
            if not self.cached_state:
                self.__cache_current_state()
            return self.cached_state["is_locked"]
        return self.__is_file_exists("{}/{}".format(CONTAINER, self.__lock_filepath()))

    def is_failed(self):
        if self.cache_state:
            if not self.cached_state:
                self.__cache_current_state()
            return self.cached_state["is_failed"]
        return self.__is_file_exists("{}/{}".format(CONTAINER, self.__failed_filepath()))

    def is_done(self):
        pass

    def is_back_up_archive_exists(self):
        return self.__is_file_exists("{}/{}".format(CONTAINER, self.__backup_archive_file_path()))

    def __enter__(self):
        self.__log.info("Init next vault: %s" % self.folder)
        super(SwiftVault, self).__enter__()

        if self.__is_file_exists(self.__metrics_filepath()):
            raise StorageLocationAlreadyExistsException("Destination backup folder already exists: %s" % self.folder)

        self.__log.info("Create .lock file in vault: %s" % self.folder)
        subprocess.call("echo .lock > /tmp/.lock", shell=True)
        subprocess.check_call(["/opt/backup/scli", "put", "/tmp/.lock", "{}/{}".format(CONTAINER, self.__lock_filepath())], env=scli_env)
        self.console = StringIO()
        return (self.folder, self.metrics, self.console)

    def create_time(self):
        foldername = self.get_id()
        d = datetime.strptime(foldername, VAULT_NAME_FORMAT)
        return time.mktime(d.timetuple())

    def __exit__(self, tpe, exception, tb):
        self.__log.info("Close vault")
        self.__log.info("Save metrics to: %s" % self.__metrics_filepath())

        super(SwiftVault, self).__exit__(tpe, exception, tb)

        backup_name = "{}/pg_{}_backup_{}.tar.gz".format(self.get_folder(), PG_CLUSTER_NAME, self.get_id())
        size_str = subprocess.check_output(
            ["/opt/backup/scli ls -l {} | grep {} | awk '{{A+=$2}} END{{print A}}'".format(CONTAINER_SEG, backup_name)]
            , shell=True
            , env=scli_env)
        try:
            self.metrics["size"] = int(size_str)
        except Exception as e:
            self.__log.error(e)
            self.metrics["size"] = -1

        if exception:
            subprocess.call("echo .failed > /tmp/.failed", shell=True)
            subprocess.check_call(["/opt/backup/scli", "put", "/tmp/.failed", "{}/{}".format(CONTAINER, self.__failed_filepath())], env=scli_env)

            e = "\n".join(format_exception(tpe, exception, tb))
            self.__log.info("Don't remove vault .lock due exception in nested code")
            self.__log.debug("Something wrong happened inside block uses vault: " + e)
            self.metrics["exception"] = e

        with open("/tmp/.metrics", "w") as f:
            json.dump(self.metrics, f)
        subprocess.check_call(["/opt/backup/scli", "put", "/tmp/.metrics", "{}/{}".format(CONTAINER, self.__metrics_filepath())], env=scli_env)

        console_logs = self.console.getvalue()
        self.console.close()
        with open("/tmp/.console", "w") as f:
            f.write(console_logs)
        subprocess.check_call(["/opt/backup/scli", "put", "/tmp/.console", "{}/{}".format(CONTAINER, self.__console_filepath())], env=scli_env)

        self.__log.info("Remove lock for %s" % self.get_id())
        subprocess.check_call(["/opt/backup/scli", "delete", "{}/{}".format(CONTAINER, self.__lock_filepath())], env=scli_env)

    def __repr__(self):
        return "Vault(%s)" % self.get_id()