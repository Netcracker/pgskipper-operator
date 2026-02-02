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

import subprocess

import boto3
import botocore
import hashlib
import io

import urllib3

import botocore.exceptions
import errno
import os
from datetime import datetime
import logging
from traceback import format_exception
import time
import json
import storage
from storage import VAULT_NAME_FORMAT, VAULT_DIRNAME_MATCHER, StorageLocationAlreadyExistsException, ARCHIVE_NAME_MATCHER
from retrying import retry

try:
    from io import StringIO
except ImportError:
    from io import StringIO

CONTAINER = os.getenv("CONTAINER")
CONTAINER_SEG = "{}_segments".format(CONTAINER)
PG_CLUSTER_NAME = os.getenv("PG_CLUSTER_NAME")

RETRY_COUNT = 10
RETRY_WAIT = 1000

urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)

class StreamWrapper(io.RawIOBase):

    def __init__(self, object_body):
        self.object_body = object_body
        self.internal_closed = False

    def close(self, *args, **kwargs):
        if not self.internal_closed:
            self.object_body.close()
            self.internal_closed = True

    def read(self, *args, **kwargs):
        return self.object_body.read(*args, **kwargs)

    def __exit__(self, *args, **kwargs):
        self.close()

    def __enter__(self, *args, **kwargs):
        return super(StreamWrapper, self).__enter__(*args, **kwargs)


class AwsS3Storage(storage.Storage):

    __log = logging.getLogger("AwsS3Storage")

    def __init__(self, root):
        self.__log.info("Init storage object with storage root: %s" % root)
        self.root = root
        self.aws_prefix = "%s/" % (os.getenv("AWS_S3_PREFIX", ""))

    def get_encryption(self):
        pass

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
        md5 = hashlib.md5()
        chunk_size = 4096
        fs_filename = "{}/{}".format("/tmp", filename)
        if not os.path.exists(os.path.dirname(fs_filename)):
            try:
                os.makedirs(os.path.dirname(fs_filename))
            except OSError as exc:
                if exc.errno != errno.EEXIST:
                    raise
        with io.FileIO(fs_filename, "w", closefd=True) as target:
            while True:
                data = stream.read(chunk_size)
                if len(data) == 0:
                    stream.close()
                    self.__log.info("Processed stream with sha256 {}".format(sha256.hexdigest()))
                    break
                sha256.update(data)
                md5.update(data)
                target.write(data)

        self.__log.info("Start uploading: %s" % filename)
        # todo[anin] replace implementation
        # AwsS3Vault.get_s3_client().upload_fileobj(data, CONTAINER, filename)
        AwsS3Vault.get_s3_client().upload_file(fs_filename, CONTAINER, filename)
        os.remove(fs_filename)
        return sha256.hexdigest()

    @retry(stop_max_attempt_number=RETRY_COUNT, wait_fixed=RETRY_WAIT)
    def prot_get_as_stream(self, filename):
        self.__log.info("Get stream request for file: %s" % self.aws_prefix + filename)
        object_body = AwsS3Vault.get_s3_resource().Bucket(CONTAINER).Object(self.aws_prefix + filename).get()['Body']
        return StreamWrapper(object_body)

    @retry(stop_max_attempt_number=RETRY_COUNT, wait_fixed=RETRY_WAIT)
    def prot_delete_bundle(self, filename):
        objects_to_delete = AwsS3Vault.get_s3_client().list_objects(Bucket=CONTAINER, Prefix=self.aws_prefix + filename)
        for obj in objects_to_delete.get('Contents', []):
            AwsS3Vault.get_s3_client().delete_object(Bucket=CONTAINER, Key=obj['Key'])

    def prot_delete(self, filename):
        self.prot_delete_bundle(filename)

    def prot_is_file_exists(self, filename):
        exists = True
        try:
            AwsS3Vault.get_s3_resource().Object(CONTAINER, self.aws_prefix + filename).get()
        except botocore.exceptions.ClientError as e:
            if e.response['Error']['Code'] == 'NoSuchKey':
                exists = False
            else:
                raise

        return exists

    def prot_get_file_size(self, filename):
        if self.prot_is_file_exists(filename):
            return int(AwsS3Vault.get_s3_resource().Object(CONTAINER, filename).get()['Size'])
        return 0

    def is_valid_backup_id(self, backup_id):
        try:
            datetime.strptime(backup_id, storage.VAULT_NAME_FORMAT)
            return True
        except ValueError:
            return False

    @retry(stop_max_attempt_number=RETRY_COUNT, wait_fixed=RETRY_WAIT)
    def list(self):
        bucket = AwsS3Vault.get_s3_client().list_objects(Bucket=CONTAINER)
        aws_s3_vault_listing = []
        if 'Contents' in bucket:
            # Collect backups ids only
            aws_s3_vault_listing = [obj["Key"].split('/', 2)[1]
                                    for obj in bucket['Contents']
                                    if '/' in obj['Key'] and self.is_valid_backup_id(obj["Key"].split('/', 2)[1])]

        vaults = [
            AwsS3Vault(backup_id
                       , bucket=CONTAINER
                       , cluster_name=PG_CLUSTER_NAME
                       , cache_enabled=True
                       , aws_s3_bucket_listing=(bucket['Contents'] if 'Contents' in bucket else None))
            for backup_id in aws_s3_vault_listing]
        vaults.sort(key=lambda v: v.create_time())
        return vaults

    def size(self):
        """ Returns whole storage size in bytes """
        total_size = 0
        bucket = AwsS3Vault.get_s3_client().list_objects(Bucket=CONTAINER)

        if 'Contents' not in bucket:
            return 0

        for obj in bucket["Contents"]:
            total_size += obj['Size']
        return total_size

    def archive_size(self):
        """ Returns whole storage size in bytes """
        total_size = 0
        bucket = AwsS3Vault.get_s3_client().list_objects(Bucket=CONTAINER, Prefix="archive/")

        if 'Contents' not in bucket:
            return 0

        for obj in bucket["Contents"]:
            total_size += obj['Size']
        return total_size

    def fs_space(self):
        """ Returns tuple (free, total) space on mount point where is root folder located """
        return (1, 1)

    def open_vault(self, backup_id):
        """
        
        :return:
        :rtype: (str, dict, StringIO)
        """
        return AwsS3Vault("%s" % (datetime.now().strftime(VAULT_NAME_FORMAT)), CONTAINER, cluster_name=PG_CLUSTER_NAME)

    def evict_vault(self, vault):
        self.__log.info("Evict vault: %s" % vault)

        backup_id = vault.get_folder()
        backup_name = "{}/{}/pg_backup_{}.tar.gz".format(self.aws_prefix, backup_id, vault.get_id())
        try:
            self.prot_delete_bundle(backup_id)
        except botocore.exceptions.ClientError as e:
            return "Not Found"

    def prot_list_archive(self):
        bucket = AwsS3Vault.get_s3_client().list_objects(Bucket=CONTAINER, Prefix="archive/", Delimiter="/")
        aws_s3_archive_listing = []
        if 'Contents' in bucket:
            # Collect archive ids only
            aws_s3_archive_listing = [obj
                  for obj in bucket['Contents']
                  if '/' in obj['Key'] and ARCHIVE_NAME_MATCHER.match(obj["Key"].split('/', 1)[1])]
        self.__log.info("Archives: {}".format(aws_s3_archive_listing))
        if aws_s3_archive_listing:
            archives = [storage.Archive(
                ARCHIVE_NAME_MATCHER.match(archive["Key"].split('/', 1)[1]).group("name"),
                1000 * int(archive["LastModified"].strftime("%s")))
                for archive in aws_s3_archive_listing]
            self.__log.info("Parsed archives: {}".format(archives))
            return archives
        else:
            return []

    def get_type(self):
        return "AWS S3"

    def get_type_id(self):
        return 1


class AwsS3VaultCreationException(Exception):
    pass


class AwsS3Vault(storage.Vault):
    __log = logging.getLogger("AwsS3Vault")

    @staticmethod
    def get_s3_resource():
        return boto3.resource("s3",
                              region_name=os.getenv("AWS_DEFAULT_REGION") if os.getenv("AWS_DEFAULT_REGION") else None,
                              endpoint_url=os.getenv("AWS_S3_ENDPOINT_URL"),
                              aws_access_key_id=os.getenv("AWS_ACCESS_KEY_ID"),
                              aws_secret_access_key=os.getenv("AWS_SECRET_ACCESS_KEY"),
                              verify=(False if os.getenv("AWS_S3_UNTRUSTED_CERT", "false").lower() == "true" else None))

    @staticmethod
    def get_s3_client():
        return AwsS3Vault.get_s3_resource().meta.client

    def __init__(self, backup_id, bucket, cluster_name=None, cache_enabled=False,
                 aws_s3_bucket_listing=None):
        super(AwsS3Vault, self).__init__()

        self.backup_id = backup_id
        self.aws_prefix = os.getenv("AWS_S3_PREFIX", "")
        self.bucket = bucket
        self.console = None
        self.cluster_name = cluster_name
        self.cache_enabled = cache_enabled
        self.cached_state = {}
        self.aws_s3_bucket_listing = aws_s3_bucket_listing
        self.backup_name = "{}/{}/pg_backup_{}.tar.gz".format(self.aws_prefix, self.get_folder(), self.get_id())

    def __get_s3_bucket(self):
        return AwsS3Vault.get_s3_resource().Bucket(self.bucket)

    def __cache_current_state(self):
        self.__log.debug("Cache current state")
        # todo[anin] fix cache
        if self.aws_s3_bucket_listing:
            # aws_s3_bucket_listing = [
            #     {
            #         'LastModified': datetime.datetime(2017, 6, 27, 10, 40, 8, 957000, tzinfo=tzlocal()),
            #         'ETag': '"b3b1d912e348a08d57cb9b3ab5a92bc0"',
            #         'StorageClass': 'STANDARD',
            #         'Key': '20170627T1040.console',
            #         'Owner': {'DisplayName': 'platform', 'ID': 'platform'},
            #         'Size': 418
            #     }
            #         ...
            # ]
            self.cached_state["is_locked"] = len([x for x in self.aws_s3_bucket_listing if self.__lock_filepath() in x['Key']]) == 1
            self.cached_state["is_failed"] = len([x for x in self.aws_s3_bucket_listing if self.__failed_filepath() in x['Key']]) == 1
        else:
            self.cached_state["is_locked"] = self.__is_file_exists(self.bucket, self.__lock_filepath())
            self.cached_state["is_failed"] = self.__is_file_exists(self.bucket, self.__failed_filepath())
        self.__log.debug("State: {}".format(self.cached_state))

    def get_id(self):
        return os.path.basename(self.backup_id)

    def get_folder(self):
        return self.backup_id

    def __get_backup_name(self):
        return self.backup_name

    @retry(stop_max_attempt_number=RETRY_COUNT, wait_fixed=RETRY_WAIT)
    def __load_metrics_from_s3(self):
        try:
            metrics = AwsS3Vault.get_s3_resource().Object(self.bucket, self.__metrics_filepath()).get()
            return json.load(metrics["Body"])
        except Exception as e:
            self.__log.exception(e)
            self.__log.warning("Cannot load metrics from s3. Backup can be damaged")
            return {}

    def load_metrics(self):
        self.__log.debug("Load metrics from: %s" % self.__metrics_filepath())
        if self.cache_enabled:
            if "metrics" not in self.cached_state:
                self.cached_state["metrics"] = self.__load_metrics_from_s3()
            return self.cached_state["metrics"]
        else:
            return self.__load_metrics_from_s3()


    def __lock_filepath(self):
        return "%s/%s/%s" % (self.aws_prefix, self.backup_id, self.backup_id + ".lock")

    def __failed_filepath(self):
        return "%s/%s/%s" % (self.aws_prefix, self.backup_id, self.backup_id + ".failed")

    def __metrics_filepath(self):
        return "%s/%s/%s" % (self.aws_prefix, self.backup_id, self.backup_id + ".metrics")

    def __console_filepath(self):
        return "%s/%s/%s" % (self.aws_prefix, self.backup_id, self.backup_id + ".console")

    def __is_file_exists(self, bucket, obj):
        exists = True
        try:
            AwsS3Vault.get_s3_resource().Object(bucket, obj).get()
        except botocore.exceptions.ClientError as e:
            if e.response['Error']['Code'] == 'NoSuchKey':
                exists = False
            else:
                raise

        return exists

    def is_locked(self):
        if self.cache_enabled:
            if not self.cached_state:
                self.__cache_current_state()
            if "is_locked" in self.cached_state.keys():
                return self.cached_state["is_locked"]

        return self.__is_file_exists(self.bucket, self.__lock_filepath())

    def is_failed(self):
        if self.cache_enabled:
            if not self.cached_state:
                self.__cache_current_state()
            if "is_failed" in self.cached_state.keys():
                return self.cached_state["is_failed"]
        return self.__is_file_exists(self.bucket, self.__failed_filepath())

    def is_done(self):
        if not self.__is_file_exists(CONTAINER, self.__metrics_filepath()):
            self.__log.info(self.__is_file_exists)
            return False
        j = self.__load_metrics_from_s3()
        self.__log.info(j)
        return j['exit_code'] == 0

    def is_back_up_archive_exists(self):
        return self.__is_file_exists(self.bucket, self.backup_name)

    def __enter__(self):
        self.__log.info("Init next vault: %s" % self.backup_id)
        super(AwsS3Vault, self).__enter__()

        if self.__is_file_exists(self.bucket, self.__metrics_filepath()):
            raise AwsS3VaultCreationException("Destination backup folder already exists: %s" % self.backup_id)

        self.__log.info("Create .lock file in vault: %s" % self.backup_id)
        lock_marker = "/tmp/.lock"
        subprocess.call("echo .lock > %s" % lock_marker, shell=True)
        self.__get_s3_bucket().upload_file(lock_marker, self.__lock_filepath())

        self.console = StringIO()

        return self.backup_id, self.metrics, self.console

    def create_time(self):
        folder_name = self.get_id()
        d = datetime.strptime(folder_name, "%Y%m%dT%H%M")
        return time.mktime(d.timetuple())

    def __exit__(self, tpe, exception, tb):
        self.__log.debug("Closing vault [%s]" % self)

        super(AwsS3Vault, self).__exit__(tpe, exception, tb)

        console_logs = self.console.getvalue()
        self.console.close()

        console_marker = "/tmp/.console"
        with open(console_marker, "w") as f:
            f.write(console_logs)
        self.__get_s3_bucket().upload_file(console_marker, self.__console_filepath())
        self.__log.info("Console logs are saved to: %s" % self.__console_filepath())

        if exception is None:
            self.__on_successful_upload()
        else:
            e = "\n".join(format_exception(tpe, exception, tb))
            self.__on_failed_upload(exception=e, output=console_logs)

        metrics_marker = "/tmp/.metrics"
        with open(metrics_marker, "w") as f:
            json.dump(self.metrics, f)

        self.__get_s3_bucket().upload_file(metrics_marker, self.__metrics_filepath())
        self.__log.info("Metrics are saved to: %s" % self.__metrics_filepath())

        self.__log.info("Remove lock for %s" % self.get_id())
        self.__get_s3_bucket().Object(self.__lock_filepath()).delete()

    def __on_successful_upload(self):
        self.__log.info("Backup %s is uploaded successfully." % self.backup_id)
        size_str = self.__get_s3_bucket().Object(self.__get_backup_name()).get()["ContentLength"]
        self.metrics["size"] = int(size_str)

    def __on_failed_upload(self, **kwargs):
        failed_marker = "/tmp/.failed"
        subprocess.call("echo .failed > %s" % failed_marker, shell=True)
        self.__get_s3_bucket().upload_file(failed_marker, self.__failed_filepath())

        self.__log.error("Something wrong happened inside block uses vault: " + kwargs['exception'])
        self.__log.error("Backup script output: " + kwargs['output'])
        self.metrics["exception"] = kwargs['exception']
        self.metrics["size"] = -1

    def __repr__(self):
        return "Vault(%s)" % self.get_id()

    def fail(self):
        open(self.__failed_filepath(), "w").close()
