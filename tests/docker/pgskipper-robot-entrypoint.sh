#!/bin/bash
# Bridge TESTS_TAGS (set by operator) to TAGS (expected by base image entrypoint).
# Ensures base image flow (adapter-S3, output, etc.) runs with correct tags.
export TAGS="${TESTS_TAGS:-$TAGS}"
exec /docker-entrypoint.sh "$@"
