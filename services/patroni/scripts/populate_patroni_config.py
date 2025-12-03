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

import sys
import yaml
# python /patroni/populate_patroni_config.py /patroni/pg_node.yml patroni/pg_conf_active.conf
import json
from utils import get_log_level

logging.basicConfig(
    level=get_log_level(),
    format='[%(asctime)s][%(levelname)-5s][category=%(name)s] %(message)s',
    datefmt='%Y-%m-%dT%H:%M:%S'
)
logger = logging.getLogger(__name__)


def is_number(s):
    """ Returns True if string is a number. """
    try:
        float(s)
        return True
    except ValueError:
        return False


def populate_patroni_config(patroni_conf_filename, settings_conf_filename):
    with open(patroni_conf_filename) as f:
        patroni_conf = yaml.safe_load(f)

        # todo[anin] check utils.read_property_file instead
    config_data = ''
    with open(settings_conf_filename) as f:
        for line in f:
            if "=" in line:
                param_name = line[0:line.find("=")]
                param_value = line[line.find("=")+1:-1]
                config_data = \
                    config_data + '\n' + \
                    param_name + ": " + \
                    ("!!str " if not is_number(param_value) else "") + \
                    param_value
            else:
                config_data = config_data + '\n' + line
    logger.debug("Result data from config file: {}\n".format(config_data))
    conf = yaml.safe_load(config_data)
    logger.debug(conf)
    if conf:
        params = patroni_conf['bootstrap']['dcs']['postgresql']['parameters']
        for key in conf:
            value = conf[key]
            if key == "log_line_prefix" and value[:1] == "\\":
                value = value[1:]

            logger.debug("Apply {}={}".format(key, value))
            params[key] = value

    with open(patroni_conf_filename, mode='w') as f:
        yaml.dump(patroni_conf, f, default_flow_style=False)


def main():
    logger.info("Try to apply provided settings to patroni config. {}"
                .format(sys.argv))
    if len(sys.argv) == 3:
        patroni_conf_filename = sys.argv[1]
        settings_conf_filename = sys.argv[2]
        populate_patroni_config(patroni_conf_filename, settings_conf_filename)

    else:
        sys.exit("Usage: {0} /patroni/pg_node.yml /patroni/pg_conf_active.conf"
                 .format(sys.argv[0]))


if __name__ == '__main__':
    main()
