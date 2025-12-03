#!/bin/bash
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


LOG_FILE=/tmp/health.log

. /opt/backup/utils.sh

function test_postgresql() {
  local output=$(psql -h $POSTGRES_HOST -p $POSTGRES_PORT -U replicator -l 2>&1)
  process_exit_code $? "${ouput}"
}

if [ "$STORAGE_TYPE" = "swift" ]; then
  test_postgresql >> ${LOG_FILE}
  output=$(/opt/backup/scli ls $CONTAINER 2>&1)
  process_exit_code $? "${ouput}" >> $LOG_FILE
elif [[ "${STORAGE_TYPE}" == "s3" ]]; then
  test_postgresql >> ${LOG_FILE}

  unset flag_ssl_no_verify
  if [[ "${AWS_S3_UNTRUSTED_CERT}" == "True" || "${AWS_S3_UNTRUSTED_CERT}" == "true" ]]; then
    flag_ssl_no_verify="--no-verify-ssl"
  fi

  output=$(aws ${flag_ssl_no_verify} --endpoint-url "${AWS_S3_ENDPOINT_URL}" s3 ls "s3://${CONTAINER}")
  process_exit_code $? "${ouput}" >> $LOG_FILE
elif [[ "$STORAGE_TYPE" = "hostpath" ]] || [[ "$STORAGE_TYPE" = "pv" ]] || [[ "$STORAGE_TYPE" = "pv_label" ]] || [[ "$STORAGE_TYPE" = "provisioned" ]] || [[ "$STORAGE_TYPE" = "provisioned-default" ]]; then
  test_postgresql >> ${LOG_FILE}
else
  log "wrong storage type $STORAGE_TYPE" >> $LOG_FILE
  exit 1
fi
