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

import time
from itertools import groupby


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
        self.interval = "delete" if (intervalStr == "delete") else self.__parseTimeSpec(intervalStr)

    def __parseTimeSpec(self, spec):
        import re
        if (spec == "0"):
            return 0

        r = re.match("^(\\d+)(%s)$" % "|".join(list(self.magnifiers.keys())), spec)
        if (r is None):
            raise Exception("Incorrect eviction start/interval specification: %s" % spec)

        digit = int(r.groups()[0])
        magnifier = self.magnifiers[r.groups()[1]]

        return digit * magnifier

    def __str__(self):
        return "%d/%d" % (self.start, self.interval)


def parse(rules):
    rules = [Rule(r) for r in rules.split(",")]
    return rules


def evict(items, rules, start_point_time=None, accessor=lambda x: x):
    """
		Calculate what to evict from given list of versions (version is timestamp value, when each lbackup was created)
	"""

    if start_point_time is None:
        start_point_time = time.time()

    evictionVersions = []
    # TODO: to cache rules
    for rule in parse(rules):
        operateVersions = [t for t in items if accessor(t) <= start_point_time - rule.start]
        if (rule.interval == "delete"):
            # all versions should be evicted catched by this interval
            evictionVersions.extend(operateVersions)
        else:
            # group by interval and leave only first on each
            thursday = 3 * 24 * 60 * 60
            for _, versionsIt in groupby(operateVersions, lambda t: int((accessor(t) - thursday) / rule.interval)):
                grouped = sorted(list(versionsIt), key=lambda t: accessor(t))
                evictionVersions.extend(grouped[:-1])

    return sorted(list(set(evictionVersions)), reverse=True)
