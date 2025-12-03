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
import logging.config
import os
from pyhocon import ConfigFactory

GLOBAL_CONFIG_FILE_PATH = '/etc/backup-daemon.conf'
LOGGING_CONFIG_FILE_PATH = '/opt/backup/logging.conf'


def load_logging_configs():
    logging.config.fileConfig(LOGGING_CONFIG_FILE_PATH)


def load_configs():
    default_config = os.path.join(os.path.dirname(__file__), 'backup-daemon.conf')
    if os.path.exists(GLOBAL_CONFIG_FILE_PATH):
        conf = ConfigFactory.parse_file(GLOBAL_CONFIG_FILE_PATH).with_fallback(default_config)
    else:
        conf = ConfigFactory.parse_file(default_config)

    log = logging.getLogger("BackupDaemonConfiguration")
    log.info("Loaded PostgreSQL backup configuration: %s" % json.dumps(conf))

    return conf

def is_external_pg():
    return os.getenv("EXTERNAL_POSTGRESQL", "") != ""
