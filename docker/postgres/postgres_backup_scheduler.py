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
from datetime import datetime
import threading
import time
import os

from croniter import croniter
from flask_restful import Resource, Api
from flask import Flask
from queue import Queue, Empty
from storage import VAULT_NAME_FORMAT

import locks
import storage
import workers


# TODO: move to postgres/endpoints
class SchedulerEndpoint(Resource):

    __endpoint = '/schedule'

    def __init__(self, scheduler):
        self.__scheduler = scheduler

    @staticmethod
    def get_endpoint():
        return SchedulerEndpoint.__endpoint

    def post(self):
        return self.__scheduler.enqueue_backup("http-request")

    def get(self):
        return self.__scheduler.get_metrics()


class BackupsScheduler:
    def __init__(self, schedule, backup_options):
        self.__log = logging.getLogger("BackupsScheduler")
        self.__backup_options = backup_options

        self.__storage = storage.init_storage(storageRoot=self.__backup_options['storage_root'])
        self.__task_queue = Queue()

        if schedule.lower() != 'none':
            self.__log.info("Start backup scheduler with: %s" % schedule)
            self.__cron = croniter(schedule)
            self.__reschedule()
        else:
            self.__next_timestamp = None
            self.__log.info("Skip backup schedule.")

    def __reschedule(self):
        self.__next_timestamp = self.__cron.get_next()
        self.__log.info("Scheduled next run at %s" % datetime.fromtimestamp(self.__next_timestamp).strftime(
            "%Y-%m-%d %H:%M:%S"))

        # TODO check on negative value after substraction
        delay = self.__next_timestamp - time.time()
        if delay < 0:
            self.__log.warn("Task execution performed longer than specified repeat interval")
            delay = 0

        self.timer = threading.Timer(delay, self.__execute_and_reschedule)
        self.timer.setDaemon(True)
        self.timer.start()

    def __execute_and_reschedule(self):
        self.__log.info("[reason=schedule] Requesting new backup by schedule.")
        self.enqueue_backup("schedule")
        self.__reschedule()

    def run(self):
        # Accept manual requests for backup.
        _activate_endpoint(self)
        self.__log.info("Waiting for backup requests...")

        while True:
            try:
                backup = self.__task_queue.get(True, timeout=1)
            except Empty:
                continue

            lock = locks.backup_lock()
            try:


                lock.acquire_lock()
                self.__log.info("[reason={}] Spawn new backup worker. Backup requests queue length: {}".format
                                (backup.get_reason(), self.__task_queue.qsize()))
                oldest_backup = self.__storage.get_oldest_backup()
                worker = workers.spawn_backup_worker(self.__storage,
                                                     backup_command=self.__backup_options['backup_command'],
                                                     eviction_rule=self.__backup_options['eviction_rule'],
                                                     backup_id=backup.get_backup_id()
                                                     )

                if oldest_backup:
                    self.__log.info("Id of latest backup: {}".format(oldest_backup.get_id()))
                    spent_time = oldest_backup.load_metrics().get('spent_time')

                    if spent_time:
                        # time stored as milliseconds converting to seconds and double the value
                        time_out = spent_time / 1000 * 2
                        self.__log.info("Setting timeout for backup process: {}".format(time_out))
                        worker.join(time_out)

                    else:
                        worker.join()
                else:
                    worker.join()

                self.__log.info("Worker completed: {}".format(not worker.is_alive()))

                if worker.is_alive():
                    self.__log.error("Backup worker for {} is not completed after timeout: {}".format(backup.get_backup_id(), time_out))
                    worker.fail()
                    worker.kill()
                    raise Exception("Backup worker timeout exceeded")

            except:
                self.__log.error("Error execute schedule callback", exc_info=1)
            finally:
                lock.release_lock()
                self.__task_queue.task_done()

    def enqueue_backup(self, reason="manual"):
        backup_id = datetime.now().strftime(VAULT_NAME_FORMAT)
        backup = Backup(backup_id, reason)
        
        self.__task_queue.put(backup)
        queue_size = self.__task_queue.qsize()
        self.__log.info("[reason={}] New backup request has been received and added to queue. Queue length: {}".format
                        (reason, queue_size))

        return {
            'accepted': True,
            'reason': reason,
            'backup_requests_in_queue': queue_size,
            'message': "PostgreSQL backup has been scheduled successfully.",
            'backup_id': backup_id
        }

    def get_metrics(self):
        return {
            'requests_in_queue': self.__task_queue.qsize(),
            'time_until_next_backup': 'none' if self.__next_timestamp is None else self.__next_timestamp - time.time(),
            'eviction_rule': self.__backup_options['eviction_rule']
        }


class Backup:
    def __init__(self, backup_id, reason):
        self.__backup_id = backup_id
        self.__reason = reason

    def get_backup_id(self):
        return self.__backup_id

    def get_reason(self):
        return self.__reason


def start(backups_schedule, backups_params):
    scheduler = BackupsScheduler(backups_schedule, backups_params)
    scheduler.run()


def _activate_endpoint(scheduler):
    app = Flask("ScheduleEndpoint")
    api = Api(app)
    api.add_resource(SchedulerEndpoint, SchedulerEndpoint.get_endpoint(), resource_class_args=(scheduler, ))
    backup_endpoint_thread = threading.Thread(target=app.run, args=('127.0.0.1', 8085))
    backup_endpoint_thread.setDaemon(True)
    backup_endpoint_thread.start()

    log = logging.getLogger("ScheduleEndpoint")
    log.info("Endpoint `/schedule` has been activated.")
