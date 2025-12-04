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

import copy
import time


class RecoveryException(Exception):
    """
    This class should be used if exception contains meaningful message to user.
    """

    def __init__(self, *args):
        super(RecoveryException, self).__init__(*args)


class Differ:

    def is_primitive_array(self, array):
        for entity in array:
            if isinstance(entity, list) or isinstance(entity, dict):
                return False
        return True

    def ensure_path(self, path, result):
        target = result
        for i in range(0, len(path)):
            el = path[i]
            nextEl = path[i + 1] if i + 1 < len(path) else None
            if isinstance(el, str):
                if el in target:
                    target = target[el]
                else:
                    if nextEl is not None:
                        target[el] = {} if isinstance(nextEl, str) else []
                        target = target[el]
                    else:
                        target[el] = None
            else:
                if len(target) > 0:
                    target = target[el]
                else:
                    if nextEl is not None:
                        target.append({} if isinstance(nextEl, str) else [])
                        target = target[el]
                    else:
                        target[el] = None

    def set_value_for_path(self, data, path, result):
        self.ensure_path(path, result)
        target = result
        for i in range(0, len(path) - 1):
            el = path[i]
            target = target[el]
        target[path[len(path) - 1]] = data

    def trace_tree_for_diffs(self, source, data, path, result, keep_name=False):
        if isinstance(data, list):
            if source != data:
                if source and self.is_primitive_array(source) or data and self.is_primitive_array(data):
                    self.set_value_for_path(data, path, result)
                else:
                    if len(data) != len(source):
                        raise AssertionError("Cannot get diff for different arrays")
                    for i in range(0, len(data)):
                        newPath = copy.copy(path)
                        newPath.append(i)
                        self.trace_tree_for_diffs(source[i], data[i], newPath, result, keep_name=keep_name)
        elif isinstance(data, dict):
            if source != data:
                for k, v in data.items():
                    newPath = copy.copy(path)
                    newPath.append(k)
                    if k not in source:
                        self.set_value_for_path(data[k], newPath, result)
                    else:
                        self.trace_tree_for_diffs(source[k], data[k], newPath, result, keep_name=keep_name)
                for k, v in source.items():
                    if k not in data:
                        newPath = copy.copy(path)
                        newPath.append(k)
                        self.set_value_for_path(None, newPath, result)
        else:
            if source != data:
                self.set_value_for_path(data, path, result)
            elif keep_name and len(path) > 0 and path[len(path) - 1] == "name":
                self.set_value_for_path(data, path, result)

    def get_json_diff(self, source, data, keep_name=False):
        result = {}
        self.trace_tree_for_diffs(source, data, [], result, keep_name=keep_name)
        return result


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
