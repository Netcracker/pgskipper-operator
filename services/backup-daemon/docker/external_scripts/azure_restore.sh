#!/usr/bin/bash
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


#set -x
#set -e
function log {
    echo -e "[$(date +'%Y-%d-%m-%T.%N')][az_restore] $2$1\e[m"
}

[[ "${DEBUG}" == 'true' ]] && set -x

timestamp=$1

#default values
restoreAsSeparate='false'
geoRestore='false'
mirrorRestore='false'

usage(){
    echo "$0 < timestamp > [ --mirror-restore ] [ --restore-as-separate ] [ --geo-restore ]"
    echo '  timestamp               - format yyyy-mm-ddThh:mm:ssZ'
    echo '  --mirror-restore        - restore to another cloud, src instance does not stop'
    echo '  --restore-as-separate   - restore to another ns/cloud, but src instance does not stop and src svc does not changed'
    echo '  --geo-restore           - invoke geo-restore during restoration of Azure PG'
    echo "  Examples:"
    echo "    $0 2023-07-14T07:23:57Z"
    echo "    $0 2023-01-11T01:11:11Z --restore-as-separate"
    echo "    $0 2023-01-11T01:11:11Z --restore-as-separate --geo-restore"
}

log "azure restore backup invoked"

if [[ -z "$timestamp" ]]; then
    echo 'Timestamp should be set'
    exit 1
fi

isCorrectTimestamp=$(echo "$timestamp" | grep '^[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]T[0-9][0-9]:[0-9][0-9]:[0-9][0-9]Z*$' -c)
if [[ "$isCorrectTimestamp" -ne 1 ]]; then
    log 'Timestamp is incorrect!'
    log 'Timestamp should be the following format yyyy-mm-ddThh:mm:ssZ'
    exit 1
fi


COUNT_ARGS=$#
i=2
while [ $i -le $COUNT_ARGS ]; do
    argument="${!i}"
    let i=i+1

    # find tabs and space
    echo "$argument" | grep -P ' |\t' > /dev/null
    EXIT_CODE=$?
    if [ "$EXIT_CODE" -eq 0 ]; then
        echo ''
        echo '|'"$argument"'| - have space ot tab! Exit!'
        echo ''
        usage
        exit 2
    fi
    if [[ $argument == --restore-as-separate ]];       then restoreAsSeparate='true'; continue; fi
    if [[ $argument == --mirror-restore ]];            then mirrorRestore='true'; continue; fi
    if [[ $argument == --geo-restore ]];               then geoRestore='true'; continue; fi

    log 'ERROR! Wrong parameters!'; usage; exit 1;
done

echo
echo "Set parameters: $@"
echo

log 'Waiting for API server till 120...'

for i in {1..120}
do
   curl --fail --connect-timeout 1 --max-time 2 -o /dev/null -s localhost:8080/v2/health && break
   sleep 1
   log "$i"
done

echo
log 'Check API code for restore method'
checkApiCode=$(curl -XOPTION --max-time 30 -o /dev/null -s -w "%{http_code}\n" localhost:8080/external/restore)
if [[ "$checkApiCode" == '404' ]]; then
    echo "ERROR! API to restore external DB is off"
    exit 1
fi

if [[ "$checkApiCode" == '000' ]]; then
    echo "ERROR! API to restore external DB is not ready"
    exit 1
fi

request="{\"restore_time\":\"$timestamp\", \"restore_as_separate\":\"$restoreAsSeparate\", \"subnet\":\"$mirrorRestore\", \"geo_restore\":\"$geoRestore\"}"

log "request to backup daemon:"
log "$request"

rawResponse=$(curl --max-time 30 -v -XPOST -H "Content-Type: application/json" localhost:8080/external/restore -d "$request")

log "response from backup daemon:"
log "$rawResponse"

restoreId=$(echo "$rawResponse" | grep '^restore-20[0-9T]*$' || echo 'error')

if [[ "$restoreId" == 'error' ]]; then
    log "Error. Expected restore id but found '$rawResponse'"
    exit 1
fi

log "restoreId is: $restoreId"

operationStatus=''
counter=0


for i in {1..1500}
do
    if [[ "$operationStatus" == 'Successful' ]]; then
        break
    fi
    operationStatus=$(curl -XGET --max-time 3 localhost:8080/external/restore/$restoreId | jq -r '.status')
    if [[ "$operationStatus" == 'Failed' ]]; then
        log 'Restore operation is in Failed status'
        exit 1
    fi
    sleep 10
    log "waiting Successful status, current status: $operationStatus"
done

log 'Successful status'

exit 0
