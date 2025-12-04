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


restore_version=""

die() {
    printf '%s\n' "$1" >&2
    exit 1
}

while :; do
    case $1 in
        --restore-version)
            if [ "$2" ]; then
                restore_version=$2
                shift
            else
                echo "Proceed with empty restore_version"
#                die 'ERROR: "--restore-version" requires a non-empty option argument.'
            fi
            ;;
        --restore-version=?*)
            restore_version=${1#*=} # Delete everything up to "=" and assign the remainder.
            ;;
        --restore-version=)
            echo "Proceed with empty restore_version"
#            die 'ERROR: "--restore-version" requires a non-empty option argument if specified.'
            ;;
        --)
            shift
            break
            ;;
        -?*)
            printf 'WARN: Unknown option (ignored): %s\n' "$1" >&2
            ;;
        *)
            break
    esac
    shift
done

cd /var/lib/pgsql/data/postgresql_${POD_IDENTITY}


if [[ -z "${restore_version}" ]] ; then
    curl -u postgres:"${PG_ROOT_PASSWORD}" postgres-backup-daemon:8081/get | tar -xzf -
else
    curl -u postgres:"${PG_ROOT_PASSWORD}"  postgres-backup-daemon:8081/get?id=${restore_version} | tar -xzf -
fi