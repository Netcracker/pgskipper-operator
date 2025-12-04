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

import os
import socket, struct
import re
import subprocess

import logging

import time

comment_pattern = re.compile("\s*#.*")


def read_property_file(filename):
    """
    Reads data from filename and parse it to dictionary.
    :param filename:
    :return:
    :rtype: dict
    """
    result = {}
    with open(filename) as f:
        for line in f:
            if "=" in line and not comment_pattern.match(line):
                param_name = (line[0:line.find("=")]).strip()
                param_value = (line[line.find("=")+1:])
                if param_name != 'log_line_prefix':
                    param_value = param_value.strip()
                else:
                    if param_value[:1] == '%':
                        param_value = "\{}".format(param_value)
                    param_value = param_value.lstrip()
                    param_value = param_value.rstrip('\n')
                result[param_name] = param_value

    return result

def is_ipv4(host):
    p = re.compile("^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$")
    return p.match(host)

def get_host_ip():
    IP = os.getenv("POD_IP")
    if not IP:
        import fcntl
        s = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
        return socket.inet_ntoa(fcntl.ioctl(s.fileno(), 0x8915, struct.pack('256s', b"eth0"[:15]))[20:24])
    else:
        if is_ipv4(IP):
            return IP
        else:
            if IP:
                return "[{}]".format(IP)

def get_log_level():
    # todo[anin] change default
    # loglevel = os.getenv('LOG_LEVEL', 'info')
    loglevel = os.getenv('LOG_LEVEL', 'debug')
    return logging.DEBUG if loglevel == "debug" else logging.INFO

def execute_shell_command(cmd):
    p = subprocess.Popen(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, shell=True)
    (output, error) = p.communicate()
    exit_code = p.wait()
    return {'output': output.decode("utf-8"), 'exit_code': exit_code, 'error': error}


def retry(exceptions=None, tries=5, delay=1, backoff=1, logger=None):
    """
    :param exceptions: if defined - only specified exceptions will be checked
    :type exceptions: tuple of Exception or Exception
    :param tries: how much to try before fail. <=0 means no limits.
    :param delay: basic delay between tries
    :param backoff: delay increase factor after each retry
    :param logger:
    :type logger: logging.Logger
    :return:
    """
    def deco_retry(f):

        def handle_error(e, mtries, mdelay):
            msg = "Error occurred during execution: {}. Will retry in {} seconds.".format(str(e), delay)
            if logger:
                logger.exception(msg)
            else:
                print(msg)
            time.sleep(mdelay)
            mtries -= 1
            mdelay *= backoff
            return mtries, mdelay

        def f_retry(*args, **kwargs):
            mtries, mdelay = tries, delay
            while tries <= 0 or mtries > 1:
                if exceptions:
                    try:
                        return f(*args, **kwargs)
                    except exceptions as e:
                        mtries, mdelay = handle_error(e, mtries, mdelay)
                else:
                    try:
                        return f(*args, **kwargs)
                    except Exception as e:
                        mtries, mdelay = handle_error(e, mtries, mdelay)
            return f(*args, **kwargs)

        return f_retry
    return deco_retry

