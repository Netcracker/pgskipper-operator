#!/bin/bash

export TAGS="${TESTS_TAGS:-$TAGS}"
cd "${ROBOT_HOME:-/opt/robot}" || exit 1

/docker-entrypoint.sh "$@"
robot_exit=$?

sleep "${RESULT_COPY_GRACE_PERIOD:-60}"

exit "$robot_exit"