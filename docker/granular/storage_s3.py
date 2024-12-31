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

import boto3

import botocore
import botocore.exceptions
import urllib3
import os
import logging
import configs
from retrying import retry

try:
    from io import StringIO
except ImportError:
    from io import StringIO

bucket = os.getenv("CONTAINER")
CONTAINER_SEG = "{}_segments".format(bucket)
PG_CLUSTER_NAME = os.getenv("PG_CLUSTER_NAME")

RETRY_COUNT = 10
RETRY_WAIT = 1000

urllib3.disable_warnings(urllib3.exceptions.InsecureRequestWarning)
class AwsS3Vault:
    __log = logging.getLogger("AwsS3Granular")

    def __init__(self, cluster_name=None, cache_enabled=False,
                 aws_s3_bucket_listing=None):

        self.bucket = bucket
        self.console = None
        self.cluster_name = cluster_name
        self.cache_enabled = cache_enabled
        self.cached_state = {}
        self.aws_s3_bucket_listing = aws_s3_bucket_listing
        self.aws_prefix = os.getenv("AWS_S3_PREFIX", "")

    def get_s3_client(self):
        return boto3.client("s3",
                            region_name=os.getenv("AWS_DEFAULT_REGION") if os.getenv("AWS_DEFAULT_REGION") else None,
                            endpoint_url=os.getenv("AWS_S3_ENDPOINT_URL"),
                            aws_access_key_id=os.getenv("AWS_ACCESS_KEY_ID"),
                            aws_secret_access_key=os.getenv("AWS_SECRET_ACCESS_KEY"),
                            verify=(False if os.getenv("AWS_S3_UNTRUSTED_CERT", "false").lower() == "true" else None))

    @retry(stop_max_attempt_number=RETRY_COUNT, wait_fixed=RETRY_WAIT)
    def upload_file(self, file_path):
        return self.get_s3_client().upload_file(file_path, self.bucket, self.aws_prefix + file_path)

    @retry(stop_max_attempt_number=RETRY_COUNT, wait_fixed=RETRY_WAIT)
    def delete_file(self, filename):
        return self.get_s3_client().delete_object(Bucket=self.bucket, Key=self.aws_prefix + filename)

    @retry(stop_max_attempt_number=RETRY_COUNT, wait_fixed=RETRY_WAIT)
    def delete_objects(self, filename):
        objects_to_delete = self.get_s3_client().list_objects(Bucket=self.bucket, Prefix=self.aws_prefix + filename)
        for obj in objects_to_delete.get('Contents', []):
            self.get_s3_client().delete_object(Bucket=self.bucket, Key=obj['Key'])

    @retry(stop_max_attempt_number=RETRY_COUNT, wait_fixed=RETRY_WAIT)
    def read_object(self, file_path):
        self.__log.info("Reading object %s" % self.aws_prefix + file_path)
        obj = self.get_s3_client().get_object(Bucket=self.bucket, Key=self.aws_prefix + file_path)
        # self.__log.info(obj['Body'].read().decode('utf8'))
        return obj['Body'].read().decode('utf8')

    @retry(stop_max_attempt_number=RETRY_COUNT, wait_fixed=RETRY_WAIT)
    def get_file_size(self, file_path):
        obj = self.get_s3_client().list_objects_v2(Bucket=self.bucket, Prefix=self.aws_prefix + file_path)
        if 'Contents' in obj:
            for field in obj["Contents"]:
                return field["Size"]
        else:
            self.__log.info("Requested {} file not found".format(file_path))

    @retry(stop_max_attempt_number=RETRY_COUNT, wait_fixed=RETRY_WAIT)
    def download_file(self, filename):
        logging.info("Downloading file {}" .format(self.aws_prefix + filename))
        try:
            self.get_s3_client().download_file(self.bucket, self.aws_prefix + filename, filename)
        except Exception as e:
            raise e
        return

    @retry(stop_max_attempt_number=RETRY_COUNT, wait_fixed=RETRY_WAIT)
    def is_file_exists(self, file):
        exists = True
        try:
            self.get_s3_client().head_object(Bucket=self.bucket, Key=self.aws_prefix + file)
        except botocore.exceptions.ClientError as e:
            exists = False
        return exists

    def is_s3_storage_path_exist(self, storage):
        bucket = self.get_s3_client().list_objects_v2(Bucket=self.bucket, Prefix=self.aws_prefix + storage)
        if 'Contents' in bucket:
            s3_storage_path = bucket['Contents'][0]["Key"]
            return True if storage in s3_storage_path else False
        return False

    def get_granular_namespaces(self, storage):
        bucket = self.get_s3_client().list_objects_v2(Bucket=self.bucket, Prefix=self.aws_prefix + storage)
        namespaces = []
        if 'Contents' in bucket:
            for obj in bucket["Contents"]:
                vault = obj["Key"].split(storage, 1)[1]
                namespace = vault.split("/", 2)[1]
                if namespace not in namespaces:
                    namespaces.append(namespace)
                else:
                    pass
            return namespaces

    def get_backup_ids(self, storage, namespace):
        namespaced_path = self.aws_prefix + storage + "/" + namespace
        bucket = self.get_s3_client().list_objects_v2(Bucket=self.bucket, Prefix=namespaced_path)
        backup_ids = []
        if 'Contents' in bucket:
            for obj in bucket["Contents"]:
                vault = obj["Key"].split(storage, 2)[1]
                backup_id = vault.split("/", 3)[2]
                if backup_id not in backup_ids:
                    backup_ids.append(backup_id)
                else:
                    pass
            return backup_ids
