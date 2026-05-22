#!/bin/bash
export TAGS="${TESTS_TAGS:-$TAGS}"
cd "${ROBOT_HOME:-/opt/robot}" || exit 1
exec /docker-entrypoint.sh "$@"