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

import sys
import os
import argparse

from utils import read_property_file, get_log_level
from utils_db import get_settings_data, is_values_diff, is_restart_pending, schedule_restart, patroni_restart_state
import logging

logging.basicConfig(
    level=get_log_level(),
    format='[%(asctime)s][%(levelname)-5s][category=%(name)s] %(message)s',
    datefmt='%Y-%m-%dT%H:%M:%S'
)
logger = logging.getLogger(__name__)


def main(conf_file, restart_pg=False):
    logger.info("Start settings validation {}".format(sys.argv))
    properties = read_property_file(conf_file)

    # find properties which requires update
    properties4update = {}
    for key, value in list(properties.items()):
        logger.debug("Try to check setting: {} with expected value {}".format(key, value))
        (current_value) = get_settings_data(key)
        logger.debug("Value from DB: {}".format(current_value))
        if is_values_diff(value, current_value):
            properties4update[key] = value

    if not properties4update:
        logger.info("No properties to update")
        return
    user = os.getenv('PATRONI_REST_API_USER')
    password = os.getenv('PATRONI_REST_API_PASSWORD')
    from requests.auth import HTTPBasicAuth
    basic_auth = HTTPBasicAuth(user, password)

    if patroni_restart_state(basic_auth):
        logger.info("Schedule restart because some settings requires restart and restart_pg is true")
        schedule_restart()
        sys.exit(1)
    else:
        sys.exit(0)



if __name__ == '__main__':
    parser = argparse.ArgumentParser(description='Validation procedure')
    parser.add_argument('--conf-file', dest='conf_file', default=None, required=True,
                        help='path to file with postgresql settings')
    parser.add_argument('--restart-pg', dest='restart_pg', default='false',
                        help='Restart postgres if there are settings which requires restart')

    args = parser.parse_args()

    main(args.conf_file, args.restart_pg == "true")

