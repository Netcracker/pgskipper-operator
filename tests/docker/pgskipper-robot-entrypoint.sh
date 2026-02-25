#!/bin/bash
# Bridge TESTS_TAGS (set by operator) to TAGS (expected by base image entrypoint).
export TAGS="${TESTS_TAGS:-$TAGS}"
# Base scripts use ./tests and ./output relative to cwd; image has WORKDIR=/app but tests live in ROBOT_HOME/tests
cd "${ROBOT_HOME:-/opt/robot}" || exit 1
exec /docker-entrypoint.sh "$@"
