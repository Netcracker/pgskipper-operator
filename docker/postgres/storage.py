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

import abc
import binascii
import hashlib
import io
import json
import logging
import re
import os
import time
import locks
import utils
import errno
import subprocess
import configs
from retrying import retry
from traceback import format_exception

try:
    from io import StringIO
except ImportError:
    from io import StringIO


class StorageLocationAlreadyExistsException(Exception):
    pass


VAULT_NAME_FORMAT = "%Y%m%dT%H%M"
VAULT_DIRNAME_MATCHER = re.compile("\\d{8}T\\d{4}", re.IGNORECASE)
# pg_{}_archive_{}
ARCHIVE_NAME_MATCHER = re.compile("pg_(.*)_archive_(?P<name>[\da-f]+(\.[\da-f]+\.backup)?(\.partial)?(\.bk)?)$", re.IGNORECASE)
# print(ARCHIVE_NAME_MATCHER.match("pg_common_archive_000000320000004600000066").group("name"))
# print(ARCHIVE_NAME_MATCHER.match("pg_common_archive_0000001B000000100000003E.bk").group("name"))
# print(ARCHIVE_NAME_MATCHER.match("pg_common_archive_0000003200000046000000A3.00000028.backup").group("name"))
# print(ARCHIVE_NAME_MATCHER.match("pg_common_archive_000000310000004400000020.partial").group("name"))
# print(ARCHIVE_NAME_MATCHER.match("pg_common_archive_0000001B0000001B00000015.partial.bk").group("name"))

PG_CLUSTER_NAME = os.getenv("PG_CLUSTER_NAME")
ARCHIVE_EVICT_POLICY = os.getenv("ARCHIVE_EVICT_POLICY")
unit_pattern = re.compile("(?P<mult>\d*)(?P<unit>.+)")
memory_pattern = re.compile("\d+(\.\d+)?(?P<unit>[kmgt][bB]?)?")
memory_units = {
    "ki": 1, "mi": 1024, "gi": 1048576,
    "k": 1, "m": 1000, "g": 1000000,
    "kb": 1, "mb": 1024, "gb": 1048576, "tb": 1073741824
}
time_pattern = re.compile("\d+(\.\d+)?(?P<unit>(ms|s|min|h|d))?")
time_utins = {
    "ms": 1, "s": 1000, "min": 60000, "h": 3600000, "d": 86400000
}
log = logging.getLogger("Storage")


class StorageException(Exception):
    def __init__(self, msg):
        super(Exception, self).__init__(msg)

#@retry(retry_on_exception=utils.retry_if_storage_error, wait_fixed=1000)
def init_storage(storageRoot):
    if configs.is_external_pg():
        version_postfix = "external"
        log.info("External Postgres is used, storage folder is \"external\"")
    else:
        version_postfix = utils.get_version_of_pgsql_server()
        if version_postfix is None:
            import fnmatch
            pg_dirs = fnmatch.filter(os.listdir(storageRoot), 'pg*')
            if not pg_dirs:
                raise StorageException("No suitable directories are found, retrying")
            log.info(f"Possible directories for backup store {pg_dirs}")
            versions = sorted([int(re.search(r'\d+', x).group()) for x in pg_dirs], reverse=True)
            version_postfix = "pg" + str(versions[0])

    log.info(f"PostgreSQL server version is equal to {version_postfix}, "
             f"so will save all backups in {version_postfix} dir")
    storageRoot = os.path.join(storageRoot, version_postfix)

    # folders
    for folder in ["archive", "granular"]:
        dirs_to_create = os.path.join(storageRoot, folder)
        if not os.path.exists(dirs_to_create):
            try:
                os.makedirs(dirs_to_create)
            except OSError as exc:
                if exc.errno != errno.EEXIST:
                    raise
    if os.environ['STORAGE_TYPE'] not in ["s3", "swift"]:
        storage = os.path.join(storageRoot, "granular")
        for namespace in os.listdir(storage):
            for backup_id in os.listdir(os.path.join(storage, namespace)):
                status_file = os.path.join(storage, namespace, backup_id, "status.json")
                if os.path.isfile(status_file):
                    with open(status_file , "r+") as f:
                        try:
                            status_json = json.load(f)
                        except ValueError as e:
                            log.error("Failed to read the status file {} Error: {}".format(status_file,e))
                        else:
                            if status_json['status'] == 'In progress':
                                log.info("Attempt to change the status Path: {} ".format(status_file))
                                status_json['status'] = 'Failed'
                                f.seek(0)
                                f.write(json.dumps(status_json))
                                f.truncate()

    if os.environ['STORAGE_TYPE'] == "swift":
        import storage_swift
        return storage_swift.SwiftStorage(storageRoot)
    if os.environ['STORAGE_TYPE'] == "s3":
        import storage_s3
        return storage_s3.AwsS3Storage(storageRoot)
    if os.environ['STORAGE_TYPE'] == 'pgbackrest':
        import storage_pgbackrest
        print("Backrest storage init")
        return storage_pgbackrest.BackRestStorage(storageRoot)
    import storage_fs
    return storage_fs.FSStorage(storageRoot)


class Storage(metaclass=abc.ABCMeta):
    __log = logging.getLogger("Storage")

    @abc.abstractmethod
    def list(self):
        """
        :return: list of available except locked one.
        :rtype: list
        """
        raise NotImplementedError

    @abc.abstractmethod
    def size(self):
        """
        :return: occupied space size in bytes
        :rtype: int
        """
        raise NotImplementedError

    @abc.abstractmethod
    def archive_size(self):
        """
        :return: occupied space size in bytes
        :rtype: int
        """
        raise NotImplementedError

    @abc.abstractmethod
    def fs_space(self):
        """
        :return: tuple (free, total) space on mount point where is root folder located
        :rtype: (int, int)
        """
        raise NotImplementedError

    @abc.abstractmethod
    def open_vault(self):
        """        
        :return:
        :rtype: (str, dict, StringIO)
        """
        raise NotImplementedError

    @abc.abstractmethod
    def evict_vault(self, vault):
        """
        Removes vault and associated files from storage
        :param vault:
        :return:
        """
        raise NotImplementedError

    @abc.abstractmethod
    def prot_is_file_exists(self, filename):
        """
        Internal method. Should not use outside storage.
        :param filename: filename with path from entry point. For fs storage entrypoint is /backup-storage/. 
        :return: 
        """
        raise NotImplementedError

    @abc.abstractmethod
    def prot_delete(self, filename):
        """
        Internal method. Should not use outside storage.
        :param filename: filename with path from entry point. For fs storage entrypoint is /backup-storage/. 
        :return: 
        """
        raise NotImplementedError

    @abc.abstractmethod
    def prot_get_as_stream(self, filename):
        """
        Internal method. Should not use outside storage.
        :param filename: path to file from backup root.
        :type filename: string
        :return: stream with requested file
        :rtype: io.RawIOBase
        """
        raise NotImplementedError

    @abc.abstractmethod
    def prot_put_as_stream(self, filename, stream):
        """
        Internal method. Should not use outside storage.
        :param filename: 
        :type filename: string
        :param stream:
        :type stream: io.RawIOBase
        :return: sha256 of processed stream
        :rtype: string
        """
        raise NotImplementedError

    @abc.abstractmethod
    def prot_get_file_size(self, filename):
        """
        Internal method. Should not use outside storage.
        :param filename: path to file from backup root.
        :type filename: string
        :return: size of file in bytes
        :rtype: int
        """
        raise NotImplementedError

    @abc.abstractmethod
    def prot_list_archive(self):
        raise NotImplementedError

    @abc.abstractmethod
    def get_type(self):
        raise NotImplementedError

    @abc.abstractmethod
    def get_type_id(self):
        return -1

    @abc.abstractmethod
    def get_encryption(self):
        raise NotImplementedError

    def is_archive_evict_policy_set(self):
        return ARCHIVE_EVICT_POLICY is not None

    def evict(self, vault):
        """
        Removes vault and associated files from storage
        :param vault:
        :return:
        """
        self.evict_vault(vault)
        self.evict_archive()

    def get_oldest_backup(self):
        """
        :return:
        :rtype: Vault
        """
        vaults = reversed(self.list())
        for vault in vaults:
            if not vault.is_failed() and vault.is_back_up_archive_exists():
                return vault
        return None

    def evict_archive(self):
        archives = self.prot_list_archive()
        if archives:
            self.__log.info("Stored archive list {}".format(archives))
            if ARCHIVE_EVICT_POLICY:
                ups = unit_pattern.search(ARCHIVE_EVICT_POLICY)
                evict_rule_unit = ups.group("unit")
                evict_rule_value = int(ups.group("mult"))
                if evict_rule_unit.lower() in memory_units:
                    value_kib = evict_rule_value * memory_units[evict_rule_unit.lower()]
                    self.__evict_by_size(archives, 1024 * value_kib)
                elif evict_rule_unit.lower() in time_utins:
                    value_ms = evict_rule_value * time_utins[evict_rule_unit.lower()]
                    self.__evict_by_time(archives, int((time.time() * 1000 - value_ms)))
                else:
                    self.__log.error(
                        "Cannot parse eviction policy {}. "
                        "Will use oldest backup to perform eviction.".
                        format(ARCHIVE_EVICT_POLICY))
                    self.__evict_by_oldest_backup(archives)

            else:
                self.__evict_by_oldest_backup(archives)

    def __evict_by_oldest_backup(self, archives):
        oldest_backup = self.get_oldest_backup()
        self.__log.info("Oldest available backup {}".format(oldest_backup))
        if oldest_backup:
            bct = oldest_backup.create_timestamp()
        else:
            self.__log.warning(
                "Does not have oldest backup. "
                "Will use current time and remove all files in archive.")
            bct = int(time.time() * 1000)
        self.__evict_by_time(archives, bct)

    def __evict_by_time(self, archives, keep_time):
        self.__log.info("Eviction timestamp {}".format(keep_time))
        for archive in archives:
            self.__log.debug("Check WAL".format(archive))
            if keep_time > archive.timestamp:
                self.delete_archive(archive.filename)

    def __evict_by_size(self, archives, keep_bytes):
        self.__log.info("Eviction limit {} bytes".format(keep_bytes))
        sorted_archives = sorted(list(archives), key=lambda t: t.timestamp, reverse=True)
        occupied_space = 0
        freed_space = 0
        for archive in sorted_archives:
            self.__log.debug("Check WAL".format(archive))
            full_name = self.__get_fullfilename_for_archive(archive.filename)
            size = self.prot_get_file_size(full_name)
            occupied_space = occupied_space + size
            if occupied_space > keep_bytes:
                self.delete_archive(archive.filename)
                freed_space = freed_space + size
        self.__log.info("Occupied before: {} bytes. Freed: {} bytes.".format(occupied_space, freed_space))

    def get_backup_as_stream(self, vault):
        """
        :return: stream with requested backup
        :rtype: io.RawIOBase
        """
        self.__log.info("Get request for vault: %s" % vault)
        backup_name = "{}/pg_{}_backup_{}.tar.gz".format(vault.get_id(), PG_CLUSTER_NAME, vault.get_id())

        # for pg10 backups there is another naming convention `pg_backup_{timestamp}`
        if not self.prot_is_file_exists(backup_name):
            backup_name = "{}/pg_backup_{}.tar.gz".format(vault.get_id(), vault.get_id())

        # encrypted backups
        if not self.prot_is_file_exists(backup_name):
            backup_name = "{}/pg_backup_{}_enc.tar.gz".format(vault.get_id(),
                                                          vault.get_id())

        return self.prot_get_as_stream(backup_name)

    def __get_fullfilename_for_archive(self, filename):
        """
        Returns full filename from storage root in form like 'archive/pg_common_archive_00000000010000000001(_enc)?'
        :param filename:
        :return:
        """
        if self.get_encryption():
            filename = filename + "_enc"
        archive_full_name = "archive/pg_{}_archive_{}".format(PG_CLUSTER_NAME, filename)
        return archive_full_name

    def __get_fullfilename_for_archive_sha(self, filename):
        sha_name = "archive/pg_{}_archive_{}.sha".format(PG_CLUSTER_NAME, filename)
        return sha_name

    def is_archive_exists(self, filename):
        return self.prot_is_file_exists(self.__get_fullfilename_for_archive(filename))

    def get_archive_as_stream(self, filename):
        """
        :return: stream with requested archive
        :rtype: io.RawIOBase
        """
        self.__log.info("Get request for archive: %s" % filename)
        return self.prot_get_as_stream(self.__get_fullfilename_for_archive(filename))

    def put_archive_as_stream(self, filename, stream):
        """
        :return: stream with requested archive
        :rtype: io.RawIOBase
        """
        self.__log.info("Put request for archive: %s" % filename)
        return self.prot_put_as_stream(self.__get_fullfilename_for_archive(filename), stream)

    def store_archive_checksum(self, filename, sha256):
        sha_name = self.__get_fullfilename_for_archive_sha(filename)
        to_store = str(binascii.crc32(sha256.encode())) + "_" + sha256
        self.prot_put_as_stream(sha_name, io.BytesIO(to_store.encode()))

    def delete_archive(self, filename):
        self.__log.info("Delete request for archive: %s" % filename)
        self.prot_delete(self.__get_fullfilename_for_archive(filename))
        sha_filename = self.__get_fullfilename_for_archive_sha(filename)
        if self.prot_is_file_exists(sha_filename):
            self.prot_delete(sha_filename)

    def get_sha256sum_for_archive(self, filename):
        sha_name = self.__get_fullfilename_for_archive_sha(filename)
        if self.prot_is_file_exists(sha_name):
            stream = self.prot_get_as_stream(sha_name)
            with stream as f:
                result = f.read().strip()
            try:
                crc = int(result.split("_")[0])
                sha256 = result.split("_")[1]
                if (crc == binascii.crc32(sha256.encode())):
                    return sha256
                else:
                    self.__log.warning("CRC32 check failed for sha file {}.".format(filename))
                    return None
            except Exception as e:
                return None
        return None

    def calculate_sha256sum_for_archive(self, filename):
        sha256 = hashlib.sha256()
        chunk_size = 4096
        stream = self.get_archive_as_stream(filename)
        with stream as f:
            while True:
                data = f.read(chunk_size)
                if len(data) == 0:
                    stream.close()
                    self.__log.info(
                        "Calculated sha256 {} for local archive".format(
                            sha256.hexdigest()))
                    return sha256.hexdigest()
                sha256.update(data)

    def get_backup_in_progress_metrics(self):
        is_backup_in_progress = os.path.isfile(locks.get_backup_lock_file_path())
        in_progress_metrics = {'backup_is_in_progress': is_backup_in_progress}

        if is_backup_in_progress:
            with open(locks.get_backup_lock_file_path(), "r") as f:
                in_progress_metrics.update(json.load(f))

        return in_progress_metrics


class Archive:
    def __init__(self, filename, timestamp):
        self.filename = filename
        self.timestamp = timestamp

    def __repr__(self):
        return "Archive name: {}, timestamp: {}".format(self.filename, self.timestamp)

    def __eq__(self, other):
        if isinstance(other, Archive):
            return self.filename == other.filename
        return False

    def __ne__(self, other):
        return not self.__eq__(other)


class Vault(metaclass=abc.ABCMeta):
    def __init__(self):
        super(Vault, self).__init__()

        self.metrics = {}

    @abc.abstractmethod
    def get_id(self): raise NotImplementedError

    @abc.abstractmethod
    def get_folder(self): raise NotImplementedError

    @abc.abstractmethod
    def load_metrics(self): raise NotImplementedError

    @abc.abstractmethod
    def is_locked(self): raise NotImplementedError

    @abc.abstractmethod
    def is_failed(self): raise NotImplementedError

    @abc.abstractmethod
    def is_done(self): raise NotImplemented

    @abc.abstractmethod
    def create_time(self): raise NotImplementedError

    @abc.abstractmethod
    def is_back_up_archive_exists(self): raise NotImplementedError

    def create_timestamp(self):
        return int(self.create_time() * 1000)

    def __enter__(self):
        self.start_timestamp = create_timestamp_as_long()

    def __exit__(self, exc_type, exc_val, exc_tb):
        end_backup_timestamp = create_timestamp_as_long()

        self.metrics["end_backup_timestamp"] = end_backup_timestamp
        self.metrics["spent_time"] = end_backup_timestamp - self.start_timestamp

    def __lt__(self, other):
        return self.create_timestamp() < other.create_timestamp()

    def __gt__(self, other):
        return self.create_timestamp() > other.create_timestamp()

    def to_json(self):
        metrics = self.load_metrics()

        result = {
            "id": self.get_id(),
            "failed": self.is_failed(),
            "locked": self.is_locked(),
            "ts": self.create_timestamp(),
            "metrics": metrics
        }

        end_backup_timestamp = metrics.get("end_backup_timestamp")
        if end_backup_timestamp:
            result["end_timestamp_ago"] = create_timestamp_as_long() - end_backup_timestamp

        return result


def create_timestamp_as_long():
    return int(time.time() * 1000)
