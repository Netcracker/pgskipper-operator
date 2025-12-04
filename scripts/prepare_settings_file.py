#!/usr/bin/env python3
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

import logging
from utils import read_property_file, get_log_level

logging.basicConfig(
    level=get_log_level(),
    format='[%(asctime)s][%(levelname)-5s][category=%(name)s] %(message)s',
    datefmt='%Y-%m-%dT%H:%M:%S'
)
logger = logging.getLogger(__name__)

PG_USER_CONF = "/properties/postgresql.user.conf"
POSTGRESQL_VERSION = int(os.getenv("POSTGRESQL_VERSION", "96"))
RUN_PROPAGATE_SCRIPT = os.getenv("RUN_PROPAGATE_SCRIPT", "True").lower()


def get_parameters_from_env():
    result = {}
    for key in os.environ:
        if key.lower().startswith("pg_conf_"):
            prop_name = (key.lower()[8:len(key)]).strip()
            prop_value = (os.environ[key]).strip()
            result[prop_name] = prop_value
    return result


def get_parameters_from_user_conf():
    return read_property_file(PG_USER_CONF)


def main():
    logger.info("Try to prepare active properties configuration based on "
                "current env and provided properties file. {}"
                .format(sys.argv))
    if len(sys.argv) == 2:
        target_file = sys.argv[1]

        params = {
            "shared_preload_libraries": "pg_stat_statements, "
                                        "pg_hint_plan, pg_cron",
        }

        logger.debug("Default parameters: {}".format(params))

        env_params = get_parameters_from_env()
        logger.info("Parameters from env: {}".format(env_params))
        for key, value in list(env_params.items()):
            params[key] = value

        logger.info("RUN_PROPAGATE_SCRIPT is set to: {}, ".format(RUN_PROPAGATE_SCRIPT))
        if RUN_PROPAGATE_SCRIPT == "true":
            conf_params = get_parameters_from_user_conf()
            logger.debug("Parameters from user config: {}".format(conf_params))
            for key, value in list(conf_params.items()):
                params[key] = value
        else:
            params.pop('shared_preload_libraries', None)

        # pg_cron is required extension.
        # check if it is present in shared_preload_libraries
        if RUN_PROPAGATE_SCRIPT == "true":
            libraries = [x.strip() for x in params.get("shared_preload_libraries", "").split(",")]
            if 'pg_cron' not in libraries:
                libraries.append("pg_cron")
                params["shared_preload_libraries"] = ", ".join(libraries)

        logger.info("Result: {}".format(params))
        logger.debug("Target file {}".format(target_file))
        with open(target_file, mode='w') as f:
            for key, value in list(params.items()):
                f.write("{}={}\n".format(key, value))
    else:
        sys.exit("Usage: {0} ./new.properties".format(sys.argv[0]))


if __name__ == '__main__':
    main()
