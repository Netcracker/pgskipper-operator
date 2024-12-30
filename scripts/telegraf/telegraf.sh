#!/usr/bin/env bash

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
