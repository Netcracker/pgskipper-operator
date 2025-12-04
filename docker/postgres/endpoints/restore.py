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

"""
Set of endpoints to performs actions with external restore.
"""

import datetime
import time
import errno
import hashlib
import json
import logging
import os
import subprocess
from threading import Thread
import boto3
from dateutil import parser

from kubernetes import client as k8s_client, config as k8s_config

import utils
import fcntl
from flask import Response, request
from flask_httpauth import HTTPBasicAuth
from flask_restful import Resource

auth = HTTPBasicAuth()


@auth.verify_password
def verify(username, password):
    return utils.validate_user(username, password)


class ExternalRestoreRequest(Resource):
    __endpoints = [
        '/external/restore',
    ]

    def __init__(self, storage):
        self.__log = logging.getLogger("ExternalRestoreEndpoint")
        self.restore_folder = f"{storage.root}/external/restore"
        self.allowable_db_types = ['AZURE', 'RDS']
        if not os.path.exists(self.restore_folder):
            try:
                os.makedirs(self.restore_folder)
            except OSError as exc:
                if exc.errno != errno.EEXIST:
                    raise
        self.status_file = f"{self.restore_folder}/status"

    @staticmethod
    def cleanup_restore_status(storage):
        log=logging.getLogger("ExternalRestoreEndpoint")
        restore_folder = f"{storage.root}/external/restore"
        status_file = f"{restore_folder}/status"
        if os.path.exists(status_file):
            with open(status_file, 'r') as f:
                status_map = json.load(f)
                f.close()
                restoreId = status_map['restoreId']
                restore_file = f"{restore_folder}/{restoreId}"
                if os.path.exists(restore_file):
                    stuck_status = None
                    with open(restore_file, 'r') as o:
                        stuck_status = json.load(o)
                        o.close()
                    stuck_status['status'] = "Failed"
                    with open(restore_file, 'w') as o:   
                        o.write(json.dumps(stuck_status))
                        o.close()
            os.remove(status_file)


    @staticmethod
    def get_endpoints():
        return ExternalRestoreRequest.__endpoints

    @auth.login_required
    def post(self):
        self.__log.debug("Endpoint /external/restore has been called")

        external_pg_type = os.getenv("EXTERNAL_POSTGRESQL", "FALSE").upper()

        if external_pg_type not in self.allowable_db_types:
            return 404

        req = request.get_json()

        if req.get('restore_time'):
            restore_time = req.get('restore_time')
        else:
            self.__log.info("No restore_time provided")
            return "No restore_time provided", 400

        if req.get('restore_as_separate'):
            restore_as_separate = req.get('restore_as_separate')
        else:
            restore_as_separate = "false"

        if req.get('geo_restore'):
            geo_restore = req.get('geo_restore')
        else:
            geo_restore = "false"

        subnet = req.get('subnet', 'false')
        if os.path.isfile(self.status_file):
            with open(self.status_file, 'r') as f:
                status_map = json.load(f)
                f.close()
                if status_map['status'] == "In Progress":
                    return status_map, 200
                else:
                    os.remove(self.status_file)

        restore_id = ExternalRestore.generate_restore_id()

        if external_pg_type == "AZURE":
            restore = ExternalRestore(self.restore_folder, restore_id, restore_time, restore_as_separate, geo_restore, subnet)

        if external_pg_type == "RDS":
            restore = RDSRestore(self.restore_folder, restore_id, restore_time, restore_as_separate)

        restore.start()
        return Response(restore_id, 202)


class ExternalRestore(Thread):
    def __init__(self, restore_folder, restore_id, restore_time, restore_as_separate, geo_restore, subnet):
        Thread.__init__(self)
        self.__log = logging.getLogger('ExternalRestore')
        self.restore_id = restore_id
        self.restore_time = restore_time
        self.restore_folder = restore_folder
        self.restore_as_separate = restore_as_separate
        self.geo_restore = geo_restore
        self.subnet = subnet

    def run(self):
        cmd_processed = self.__process_cmd(self.restore_id, self.restore_time, self.restore_folder,
                                           self.restore_as_separate, self.geo_restore, self.subnet)
        exit_code = subprocess.call(cmd_processed)
        if exit_code != 0:
            self.__log.error("Restore process has been failed")
        else:
            self.__log.info("Restore process successfully finished")

    def __process_cmd(self, restore_id, restore_time, restore_folder, restore_as_separate, geo_restore, subnet):
        self.__log.info(f"restore_id {restore_id}, restore_time {restore_time}, "
                        f"restore_folder {restore_folder}, restore_as_separate {restore_as_separate}, "
                        f"subnet: {subnet}")
        cmd_processed = self.__split_command_line(
            f"/opt/backup/azure_restore "
            f"--restore_id {restore_id} "
            f"--restore_time {restore_time} "
            f"--restore_folder {restore_folder} "
            f"--restore_as_separate {restore_as_separate} "
            f"--geo_restore {geo_restore} "
            f"--subnet {subnet}")
        return cmd_processed

    @staticmethod
    def __split_command_line(cmd_line):
        import shlex
        lex = shlex.shlex(cmd_line)
        lex.quotes = '"'
        lex.whitespace_split = True
        lex.commenters = ''
        return list(lex)

    @staticmethod
    def generate_id():
        return datetime.datetime.now().strftime("%Y%m%dT%H%M%S%f")

    @staticmethod
    def generate_restore_id():
        return 'restore-%s' % ExternalRestore.generate_id()


class RDSRestore(Thread):

    NAMESPACE_PATH = '/var/run/secrets/kubernetes.io/serviceaccount/namespace'

    def __init__(self, restore_folder, restore_id, restore_time, restore_as_separate):
        Thread.__init__(self)
        try:
            self.__log = logging.getLogger('RDSRestore')
            self.restore_id = restore_id
            self.restore_time = restore_time
            self.restore_folder = restore_folder
            self.client = self.get_rds_client()
            self.pg_service_name = 'pg-patroni'
            self.namespace = open(self.NAMESPACE_PATH).read()
            k8s_config.load_incluster_config()
            self.k8s_core_api = k8s_client.CoreV1Api()
            self.status = {
                'trackingId': self.restore_id,
                'namespace': self.namespace,
                'status': BackupStatus.PLANNED
            }
            self.restore_as_separate = restore_as_separate
        except Exception as e:
            print("RDS: Client Error: %s " % e)
            raise Exception(e)

    def run(self):
        try:
            self.__log.info("RDS: Restore Cluster Running")
            self.update_status("status", BackupStatus.IN_PROGRESS, True)

            external_name = self.get_service_external_name()
            cluster_name = external_name.split(".")[0]

            response, restored_cluster_name = self.restore_cluster(cluster_name)
            current_instances, restored_instances = self.restore_db_instances(cluster_name, restored_cluster_name)
            self.wait_for_db_instance(restored_instances)
            if self.restore_as_separate != 'true':
                self.update_service_external_name(restored_cluster_name)
                self.stop_db_cluster(cluster_name)
            self.update_status("status", BackupStatus.SUCCESSFUL, True)
            self.__log.info("RDS: Restore Cluster Successful")
        except Exception as e:
            self.__log.error("RDS: Restore failed %s" % e)
            self.update_status("status", BackupStatus.FAILED, True)

    @staticmethod
    def generate_id():
        return datetime.datetime.now().strftime("%Y%m%dT%H%M%S%f")

    @staticmethod
    def generate_restore_id():
        return 'restore-%s' % RDSRestore.generate_id()

    def get_rds_client(self):
        self.__log.info("RDS: Init RDS Client")
        return boto3.client("rds")

    def restore_cluster(self, cluster_name):

        self.check_name(cluster_name)

        restored_cluster_name = self.get_restored_name(cluster_name)
        self.__log.info("RDS: Restore cLuster: %s" % cluster_name)
        self.__log.info("RDS: Restore cLuster with new name: %s" % restored_cluster_name)

        try:
            security_groups = []
            instance_response = self.client.describe_db_instances(
                Filters=[{'Name': 'db-cluster-id', 'Values': [cluster_name, ]}, ])
            if instance_response.get('DBInstances'):
                security_groups = self.extract_vpc_security_group_ids(
                    instance_response.get('DBInstances')[0].get('VpcSecurityGroups'))

            cluster_response = self.client.describe_db_clusters(DBClusterIdentifier=cluster_name)
            if cluster_response.get('DBClusters'):
                cluster = cluster_response.get('DBClusters')[0]
                subnet_group_name = cluster.get('DBSubnetGroup')
                port = cluster.get('Port')
            else:
                raise Exception("Cluster response is empty. Can not read parameters to continue")

            restore_date = parser.parse(self.restore_time)

            response = self.client.restore_db_cluster_to_point_in_time(
                DBClusterIdentifier=restored_cluster_name,
                SourceDBClusterIdentifier=cluster_name,
                RestoreToTime=restore_date,
                DBSubnetGroupName=subnet_group_name,
                Port=port,
                VpcSecurityGroupIds=security_groups,
                Tags=[{
                    'Key': 'SOURCE-CLUSTER',
                    'Value': cluster_name
                }, {
                    'Key': 'RESTORE-TIME',
                    'Value': self.restore_time
                },
                ],
            )

            status_code = response.get('ResponseMetadata').get('HTTPStatusCode')
            if status_code != 200:
                raise Exception("Error occurred while cluster restoring. http code: %s" % status_code)
            self.__log.info("RDS: Cluster %s restored successfully" % restored_cluster_name)
        except Exception as e:
            raise Exception("RDS: Client Error: %s " % e)

        return response, restored_cluster_name

    def wait_for_db_instance(self, restored_instances):
        for instance_name in restored_instances:
            self.__log.info("RDS: Wait For DB Instance: %s" % instance_name)
            waiter = self.client.get_waiter('db_instance_available')
            waiter.wait(DBInstanceIdentifier=instance_name)
            self.__log.info("RDS: DB Instance %s is ready" % instance_name)

    def restore_db_instances(self, cluster_name, restored_cluster_name):
        instance_response = self.client.describe_db_instances(
            Filters=[{'Name': 'db-cluster-id', 'Values': [cluster_name, ]}, ])
        if instance_response.get('DBInstances'):
            restored_instances = []
            current_instances = []
            for current_instance in instance_response.get('DBInstances'):
                current_instance, restored_instance = self.create_db_instance(restored_cluster_name, current_instance)
                restored_instances.append(restored_instance)
                current_instances.append(current_instance)
        else:
            raise Exception("RDS: Instance response is empty. Can not read parameters to continue")
        return current_instances, restored_instances

    def create_db_instance(self, restored_cluster_name, current_instance):
        try:
            current_instance_id = current_instance.get('DBInstanceIdentifier')
            self.check_name(current_instance_id)
            restored_instance_id = self.get_restored_name(current_instance_id)
            self.__log.info("RDS: Create DB Instance: %s" % restored_instance_id)

            db_instance_class = current_instance.get('DBInstanceClass')
            db_engine = current_instance.get('Engine')
            # db_master_username = current_instance.get('MasterUsername')

            self.client.create_db_instance(
                DBInstanceIdentifier=restored_instance_id,
                DBInstanceClass=db_instance_class,
                Engine=db_engine,
                DBClusterIdentifier=restored_cluster_name,
                Tags=[{
                    'Key': 'SOURCE-CLUSTER',
                    'Value': current_instance_id
                },
                ],
            )
        except Exception as e:
            raise Exception("RDS: Restore DB Instance Failed: %s" % e)

        return current_instance_id, restored_instance_id

    def extract_vpc_security_group_ids(self, current_instance_security_groups):
        vpc_security_group_ids = []
        for group in current_instance_security_groups:
            vpc_security_group_ids.append(group.get('VpcSecurityGroupId'))
        return vpc_security_group_ids

    def get_cluster_name(self, instance_name):
        instance_response = self.client.describe_db_instances(DBInstanceIdentifier=instance_name)
        return instance_response.get('DBClusterIdentifier')

    def update_service_external_name(self, restored_cluster_name):
        self.__log.info("RDS: update service external name: %s" % restored_cluster_name)
        # instance_response = self.client.describe_db_instances(
        #     Filters=[{'Name': 'db-cluster-id', 'Values': [restored_cluster_name,]},])
        # if instance_response.get('DBInstances'):
        #     restored_db_endpoint = instance_response.get('DBInstances')[0].get('Endpoint').get('Address')
        cluster_response = self.client.describe_db_clusters(DBClusterIdentifier=restored_cluster_name)
        if cluster_response.get('DBClusters'):
            restored_db_endpoint = cluster_response.get('DBClusters')[0].get('Endpoint')
        else:
            raise Exception("RDS: Restored instance response is empty. Can not read parameters to continue")

        service_patch = self.create_service(restored_db_endpoint)
        self.k8s_core_api.patch_namespaced_service(name=self.pg_service_name, namespace=self.namespace, body=service_patch)

    def get_service_external_name(self):
        services = self.k8s_core_api.list_namespaced_service(
            namespace=self.namespace,
            field_selector="metadata.name=%s" % self.pg_service_name)
        external_name = services.items[0].spec.external_name
        self.__log.info("RDS: Current service external name: %s" % external_name)
        return external_name

    def create_service(self, external_name) -> k8s_client.V1Service:
        return k8s_client.V1Service(
            spec=k8s_client.V1ServiceSpec(
                external_name=external_name
            ),
        )

    def get_restored_name(self, name):
        hashed_time = hashlib.md5(self.restore_id.encode('utf-8')).hexdigest()[0:8]
        if "-restore-" in name:
            old_name = name.split("-restore-")
            return old_name[0] + "-restore-" + hashed_time
        else:
            return name + "-restore-" + hashed_time

    #RDS cluster or instance names must contain from 1 to 63 symbols. 17 of them are reserved for restored name.
    def check_name(self, name):
        if len(name) > 46:
            raise Exception("Name :%s is too long. Must me not more than 46 symbols")

    def stop_db_instances(self, instances):
        for instance in instances:
            self.__log.info("RDS: Stop instance: %s" % instance)
            self.client.stop_db_instance(DBInstanceIdentifier=instance)
            self.__log.info("RDS: Stop instance: %s - Stopped" % instance)

    def stop_db_cluster(self, cluster_name):
        self.__log.info("RDS: Stop cluster: %s" % cluster_name)
        self.client.stop_db_cluster(DBClusterIdentifier=cluster_name)
        self.__log.info("RDS: Stop cluster: %s - Stopped" % cluster_name)

    def build_restore_status_file_path(self, ):
        return '%s/%s' % (self.restore_folder, self.restore_id)

    def flush_status(self):
        path = self.build_restore_status_file_path()
        self.write_in_json(path, self.status)

    def update_status(self, key, value, flush=False):
        self.status[key] = value
        if flush:
            self.flush_status()

    def write_in_json(self, path, data):
        with open(path, 'w') as fd:
            try:
                fcntl.lockf(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
                json.dump(data, fd)
                return data
            except IOError:  # another process accessing
                self.__log.info("trying to access locked file while writing")
                raise
            finally:
                fcntl.lockf(fd, fcntl.LOCK_UN)


class BackupStatus:
    SUCCESSFUL = 'Successful'
    FAILED = 'Failed'
    IN_PROGRESS = 'In progress'
    PLANNED = 'Planned'
    UNKNOWN = 'Unknown'
    CANCELED = 'Canceled'
