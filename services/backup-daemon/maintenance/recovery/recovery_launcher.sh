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


source recovery_utils.sh
source recovery_setEnv.sh

validate_binary jq


# check if need to setup RESTORE_VERSION
if [[ -z "${RESTORE_VERSION}" ]] ; then
    log "Current RESTORE_VERSION value is empty."

    if [[ -n "${RECOVERY_TARGET_TIME}" ]] ; then
        log "RECOVERY_TARGET_TIME is specified. Leave RESTORE_VERSION value is empty to allow procedure to guess RESTORE_VERSION."
    fi

    backup_list_json=$(curl http://postgres-backup-daemon:8081/list)
    backup_list=$(echo ${backup_list_json} | jq -r '.[].id')
    for item in ${backup_list[@]}; do
        echo ${item}
    done
    RESTORE_VERSION_CHECK=false
    while [ ${RESTORE_VERSION_CHECK} == "false" ] ; do
        log "Please select one of backups. Empty input means last available backup or guessed backup."
        read RESTORE_VERSION
        # empty value allowed
        if [[ -z "${RESTORE_VERSION}" ]] ; then
            RESTORE_VERSION_CHECK=true
        fi
        # check non empty value for correctness
        for item in ${backup_list[@]}; do
            if [[ "${RESTORE_VERSION}" == ${item} ]] ; then
                RESTORE_VERSION_CHECK=true
                break
            fi
        done
    done
fi


if [[ -z "${RESTORE_VERSION}" ]] ; then
    log "Will try to restore last available backup or guessed backup."
    confirm || exit 1
else
    log "Will try to restore backup ${RESTORE_VERSION}."
    confirm || exit 1
fi

RESTORE_VERSION=${RESTORE_VERSION} ./recovery.sh
