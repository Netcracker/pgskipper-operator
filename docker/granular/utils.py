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

import json

import tarfile
import psycopg2
from retrying import retry
import fcntl
import logging
import configs
import os
import re
import backups
import encryption
import subprocess
import fnmatch
import kube_utils

import http.client

from googleapiclient.discovery import build
from googleapiclient import errors

log = logging.getLogger("utils")

"""
    sometimes write operations can take more than 5 seconds (storage problems)
    hence, file will be locked for more than 5 seconds, for this cases
    `stop_max_delay` should be increased, so `FILE_OPERATION_DELAY`
    env can be used
"""


@retry(stop_max_delay=int(os.getenv("FILE_OPERATION_DELAY", '5000')))
def get_json_by_path(path):
    with open(path) as fd:
        try:
            fcntl.lockf(fd, fcntl.LOCK_SH | fcntl.LOCK_NB)
            return json.load(fd)
        except IOError:  # another process accessing
            log.info("trying to access locked file while reading")
            raise
        finally:
            fcntl.lockf(fd, fcntl.LOCK_UN)


@retry(stop_max_delay=int(os.getenv("FILE_OPERATION_DELAY", '5000')))
def write_in_json(path, data):
    with open(path, 'w+') as fd:
        try:
            fcntl.lockf(fd, fcntl.LOCK_EX | fcntl.LOCK_NB)
            json.dump(data, fd)
            return data
        except IOError:  # another process accessing
            log.info("trying to access locked file while writing")
            raise
        finally:
            fcntl.lockf(fd, fcntl.LOCK_UN)


def execute_query(conn_properties, query):
    conn = None
    try:
        conn = psycopg2.connect(**conn_properties)
        with conn.cursor() as cur:
            cur.execute(query)
            return cur.fetchone()[0]
    finally:
        if conn:
            conn.close()

def get_version_of_pgsql_server():
    conn_properties = configs.connection_properties()
    result = execute_query(conn_properties, 'SHOW SERVER_VERSION;')
    return list(map(int, result.split(' ')[0].split('.')))


# need to rewrite this one with usage of execute_query()
def get_owner_of_db(database):
    conn_properties = configs.connection_properties()
    conn = None
    try:
        conn = psycopg2.connect(**conn_properties)
        with conn.cursor() as cur:
            cur.execute("""
            SELECT pg_catalog.pg_get_userbyid(d.datdba) as "Owner"
            FROM pg_catalog.pg_database d
            WHERE d.datname = %s;
            """, (database,))
            return cur.fetchone()[0]
    finally:
        if conn:
            conn.close()

def get_database_list(databases):
    databases = tuple(databases)
    conn_properties = configs.connection_properties()
    conn = None
    try:
        conn = psycopg2.connect(**conn_properties)
        with conn.cursor() as cur:
            cur.execute("SELECT datname from pg_database where datname in %s;", (databases,))
            return [p[0] for p in cur.fetchall()]
    finally:
        if conn:
            conn.close()

def get_pg_version_from_dump(backup_path, key_name, bin_path):
  
    parallel_jobs = configs.get_parallel_jobs()
    if key_name:
        command = "openssl enc -aes-256-cbc -nosalt -d -pass " \
                  "pass:'{}' < '{}' | {} -l ".format(
            encryption.KeyManagement.get_object().get_password_by_name(
                key_name), backup_path, bin_path)
    else:
        if int(parallel_jobs) > 1:
            command = '{}/pg_restore -j {} -l {}'.format(bin_path, parallel_jobs, backup_path)
        else:    
            command = '{}/pg_restore -l {}'.format(bin_path, backup_path) 

    subprocess.check_output("ls -la {}".format(backup_path), shell=True)

    output = \
        subprocess.Popen(command, shell=True,
                         stdout=subprocess.PIPE).communicate()[0]

    # pg_restore -l output format example
    # ; Archive created at 2019-02-11 09:38:35 UTC
    # ;     dbname: test1
    # ;     Dumped from database version: 10.3
    # ;     Dumped by pg_dump version: 10.6
    # ; Selected TOC Entries:
    # ;
    for item in output.decode().split(";"):
        if "Dumped from database version" in item:
            version_as_string = item.split(": ")[1]
            return list(map(int, version_as_string.split(' ')[0].split('.')))
    return None



class Rule:
    magnifiers = {
        "min": 60,
        "h": 60 * 60,
        "d": 60 * 60 * 24,
        "m": 60 * 60 * 24 * 30,
        "y": 60 * 60 * 24 * 30 * 12,
    }

    def __init__(self, rule):
        (startStr, intervalStr) = rule.strip().split("/")
        self.start = self.__parseTimeSpec(startStr)
        self.interval = "delete" if (
                intervalStr == "delete") else self.__parseTimeSpec(intervalStr)

    def __parseTimeSpec(self, spec):
        import re
        if (spec == "0"):
            return 0

        r = re.match("^(\\d+)(%s)$" % "|".join(list(self.magnifiers.keys())), spec)
        if (r is None):
            raise Exception(
                "Incorrect eviction start/interval specification: %s" % spec)

        digit = int(r.groups()[0])
        magnifier = self.magnifiers[r.groups()[1]]

        return digit * magnifier

    def __str__(self):
        return "%d/%d" % (self.start, self.interval)


def parse(rules):
    rules = [Rule(r) for r in rules.split(",")]
    return rules


def is_auth_needed():
    return os.getenv("AUTH", "false").lower() == "true"


def get_backup_tar_file_path(backup_id, namespace):
    path = backups.build_backup_path(backup_id, namespace)
    if os.path.exists(path):
        tar_path = os.path.join(path, backup_id)
        items = [x for x in os.listdir(path) if x.endswith('.dump') or x.endswith('.sql')]
        with tarfile.open(tar_path + ".tar.gz", "w:gz") as tar:
            for item in items:
                tar.add(os.path.join(path, item), arcname=item)
        full_path_tar = tar_path + ".tar.gz"
        return full_path_tar
    else:
        return None


class GkeBackupApiCaller:
    def __init__(self):
        self.log = logging.getLogger('BackupRequestEndpoint')
        self.service = build('sqladmin', 'v1beta4', cache_discovery=False)
        self.project = os.getenv("GKE_PROJECT")
        self.instance = os.getenv("GKE_INSTANCE")

    def perform_backup(self):
        req = self.service.backupRuns().insert(project=self.project, instance=self.instance)
        resp = req.execute()
        self.log.info(json.dumps(resp, indent=2))
        if "error" in resp:
            return resp
        else:
            return self.get_backup_id(resp["insertTime"])

    def get_backup_id(self, insert_time):
        req = self.service.backupRuns().list(project=self.project, instance=self.instance)
        resp = req.execute()
        self.log.info(json.dumps(resp, indent=2))
        for item in resp["items"]:
            if item["startTime"] == insert_time:
                self.log.info("Backup requested id: {}".format(item["id"]))
                return item["id"]
        else:
            return "Can't perform backup"

    def delete_backup(self, backup_id):
        req = self.service.backupRuns().delete(project=self.project, instance=self.instance, id=backup_id)
        try:
            resp = req.execute()
            self.log.info(json.dumps(resp, indent=2))
            return {
                "backupId": backup_id,
                "message": resp["status"]
                   }, http.client.OK

        except errors.HttpError as e:
            if e.resp["status"] == "404":
                return "Backup with id {} not found".format(backup_id), http.client.NOT_FOUND
            else:
                return http.client.BAD_REQUEST

    def backup_status(self, backup_id):
        req = self.service.backupRuns().get(project=self.project, instance=self.instance, id=backup_id)
        try:
            resp = req.execute()
            self.log.info(json.dumps(resp, indent=2))
            return resp, http.client.OK
        except errors.HttpError as e:
            if e.resp["status"] == "404":
                return "Backup with id {} not found".format(backup_id), http.client.NOT_FOUND
            else:
                return http.client.BAD_REQUEST

    def restore(self, restore_request):
        body = {
            "restoreBackupContext": {
                "backupRunId": restore_request["backupId"]
            }
        }
        req = self.service.instances().restoreBackup(project=self.project, instance=self.instance, body=body)
        try:
            resp = req.execute()
            self.log.info(json.dumps(resp, indent=2))
            return {
                   'trackingId': resp["name"]
               }, http.client.ACCEPTED
        except errors.HttpError as e:
            if e.resp["status"] == "404":
                return "Backup with id {} not found".format(restore_request["backupId"]), http.client.NOT_FOUND
            else:
                return "Bad request", http.client.BAD_REQUEST

    def restore_status(self, restore_id):
        req = self.service.operations().get(project=self.project, operation=restore_id)
        try:
            resp = req.execute()
            self.log.info(json.dumps(resp, indent=2))
            return resp, http.client.ACCEPTED
        except errors.HttpError as e:
            if e.resp["status"] == "404":
                return "Restore with id {} not found".format(restore_id), http.client.NOT_FOUND
            else:
                return http.client.BAD_REQUEST

    def backup_list(self):
        req = self.service.backupRuns().list(project=self.project, instance=self.instance)
        try:
            resp = req.execute()
            self.log.info(json.dumps(resp, indent=2))
            result = {}
            result["default"] = {}
            for item in resp["items"]:
                result["default"][item["id"]] = {"status": item["status"]}
            return result, http.client.OK
        except:
            return http.client.BAD_REQUEST


def get_postgres_version_by_path(storage_path):
    #storage_path = '/usr'
    pg_dirs = fnmatch.filter(os.listdir(storage_path), 'pg*')
    log.info(f"Possible directories for backup store using path method {pg_dirs}")
    versions = sorted([int(re.search(r'\d+', x).group()) for x in pg_dirs], reverse=True)
    version_postfix = "pg" + str(versions[0])
    log.info(f"PostgreSQL server version from path method is equal to {version_postfix}, "
             f"so will save all backups in {version_postfix} dir")
    version_as_list = [int(version_postfix.replace("pg", "")), 0]
    return version_as_list

def is_mirror_env():
    cm = kube_utils.get_configmap('mirror-config')
    return cm is not None