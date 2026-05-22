#!/bin/bash

export TAGS="${TESTS_TAGS:-$TAGS}"
cd "${ROBOT_HOME:-/opt/robot}" || exit 1

/docker-entrypoint.sh "$@"
robot_exit=$?

echo "Robot finished with exit code ${robot_exit}. Waiting before pod exit to allow result copy..."
sleep "${RESULT_COPY_GRACE_PERIOD:-300}"

exit "${robot_exit}"