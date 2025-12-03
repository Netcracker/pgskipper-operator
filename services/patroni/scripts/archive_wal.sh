#!/usr/bin/env bash
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


p="${1}"
f="${2}"

set +x

export `cat /proc/1/environ  | tr '\0' '\n' | grep PG_ROOT_PASSWORD`

sha256sum -b "$p" | cut -d " " -f1 | xargs -I {} echo sha256={} | \
python3 -c "import sys; print(chr(38) + sys.stdin.read().strip())" | \
xargs -I SHA curl -u postgres:"${PG_ROOT_PASSWORD}" -s -S -f --connect-timeout 5 --speed-time 30 --speed-limit 100 -XPOST -F "file=@$p" postgres-backup-daemon:8082/archive/put?filename="$f"SHA