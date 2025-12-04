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


export METRICS_PROFILE=${METRICS_PROFILE:-prod}

# for DEV metrics all of the configuration vars should be specified in minutes
export DEV_METRICS_TIMEOUT=${DEV_METRICS_TIMEOUT:-5}
export DEV_METRICS_INTERVAL=${DEV_METRICS_INTERVAL:-5}

set -e

python /telegrafwd/preparation_script.py || true

# substitute environment variables
echo "Applying environment variables to Telegraf configs"
for f in /etc/telegraf/telegraf.templates/*.conf; do envsubst < "${f}" > /etc/telegraf/telegraf.d/$(basename ${f}) ; done

envsubst < "${TELEGRAF_CONFIG_TEMP}" > "${TELEGRAF_CONFIG}"

echo "Telegraf config file after applying envs:"
cat "${TELEGRAF_CONFIG}" | grep -vE '\s*#' | grep -v '^$' | grep -v 'password'

METRICS_PROFILE=`echo "print('$METRICS_PROFILE'.lower())" | python`
echo "Metrics profile is set to $METRICS_PROFILE."

# showing info about timeouts and intervals for DEV metrics if profile is set to dev
[ "$METRICS_PROFILE" == dev ] && echo "DEV_METRICS_TIMEOUT: $DEV_METRICS_TIMEOUT DEV_METRICS_INTERVAL: $DEV_METRICS_INTERVAL"

exec env METRICS_PROFILE=${METRICS_PROFILE} telegraf --config-directory /etc/telegraf/telegraf.d
