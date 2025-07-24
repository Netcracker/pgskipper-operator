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

import configs
import postgres_backup_scheduler


if __name__ == "__main__":
    configs.load_logging_configs()
    conf = configs.load_configs()

    log = logging.getLogger("PostgreSQLBackupDaemon")
    log.info("Backup daemon raised again.")

    backups_params = {
        'backup_command': conf.get_string("command"),
        'storage_root': conf.get_string("storage"),  # TODO (vladislav.kaverin): Actually almost nobody needs storage root, only file-system storage.
        'eviction_rule': conf.get_string("eviction"),
        'timeout': conf.get_int("timeout")
    }
    backups_schedule = conf.get_string("schedule")

    # Start scheduler and activate `/backup` endpoint.
    backup_scheduler = postgres_backup_scheduler.start(backups_schedule, backups_params)


    # scheduler <- backup_executor <- storage
    # http_api <- storage