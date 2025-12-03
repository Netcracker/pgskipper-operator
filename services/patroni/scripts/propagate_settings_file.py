#!/usr/bin/env python
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
import re
import subprocess
import sys
import os
import time

from utils import read_property_file, get_host_ip, get_log_level
from utils_db import get_settings_data, get_context_data, is_values_diff, is_restart_pending, schedule_restart, patroni_restart_state
import logging
import requests

logging.basicConfig(
    level=logging.INFO,
    format='[%(asctime)s][%(levelname)-5s][category=%(name)s] %(message)s',
    datefmt='%Y-%m-%dT%H:%M:%S'
)
logger = logging.getLogger(__name__)


def main():

    logger.info("Try to propagate property file to cluster. {}".format(sys.argv))
    if len(sys.argv) == 2:
        source_file = sys.argv[1]
        properties = read_property_file(source_file)

        # find properties which requires update
        properties4update = {}
        restart_required = False
        for key, value in list(properties.items()):
            current_value = get_settings_data(key)
            if is_values_diff(value, current_value):
                context = get_context_data(key)
                logger.info(context)
                if context == "internal":
                    logger.error("We cannot change variable of internal context: {}".format(key))
                    sys.exit(1)
                properties4update[key] = value

        if not properties4update:
            logger.info("No properties to update")
            return
        logger.info("Need to update: {}".format(properties4update))

        # form patch
        #  todo[anin] add parameters validation (int - max, min val; string; enum)
        patch_data = {"postgresql": {"parameters": {}}}
        for key, value in list(properties4update.items()):
            tmp = ""
            if key != 'log_line_prefix':
                tmp = value.strip()
                if "\\" in tmp:
                    tmp = tmp.replace("\\", "\\\\")
            else:
                if "\\" == value[:2]:
                    tmp = value[2:]
                else:
                    tmp = value

            patch_data["postgresql"]["parameters"][key] = tmp  # json.dumps(value)

        # send patch
        # curl -i -XPATCH -d @/patroni/parameters_data http://$(hostname -i):8008/config
        logger.debug("Patch prepared: {}".format(patch_data))
        user = os.getenv('PATRONI_REST_API_USER')
        password = os.getenv('PATRONI_REST_API_PASSWORD')
        from requests.auth import HTTPBasicAuth
        basic_auth = HTTPBasicAuth(user, password)
        logger.info(requests.patch(
            "http://{}:8008/config".format(get_host_ip()),
            data=json.dumps(patch_data),
            auth=basic_auth))


        # todo[anin] replace with pg_settings.pending_restart check.
        # There is problem - patroni updates config after restart command.
        # So we cannot detect pending_restart flag until actual restart.
        # for key, value in properties4update.items():
        #     (current_value, unit, category, vartype, context) = get_setting_data(key)
        #     if is_values_differs(value, current_value, unit, vartype):
        #         logger.info("Schedule restart because some settings requires restart")
        #         schedule_restart()
        #         return
        iterations = int(os.getenv('CHANGE_SETTINGS_RETRIES', 5))
        sleep = int(os.getenv('CHANGE_SETTINGS_INTERVAL', 3))
        if patroni_restart_state(basic_auth, iterations, sleep):
            schedule_restart()

            return

        # # todo[anin] this code can be interrupted by callback executor
        # # wait while value will be applied to current server
        # applied = True
        # for i in range(1, 60):
        #     applied = True
        #     for key, value in properties4update.items():
        #         (current_value, unit, category, vartype) = get_setting_data(key)
        #         if is_values_differs(value, current_value, unit, vartype):
        #             applied = False
        #             sleep(1)
        #             break
        #
        # if not applied:
        #     sys.exit("Setting were not applied")

    else:
        sys.exit("Usage: {0} ./active.properties".format(sys.argv[0]))


if __name__ == '__main__':
    main()
