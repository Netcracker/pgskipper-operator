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


cd $(dirname "$0")

readonly AWS_S3_STORAGE="s3"
readonly SWIFT_STORAGE="swift"

# In case of Encryption password for encryption will be passed as
# 2nd input parameter, 1st one is data_folder
readonly ENCRYPTION_KEY="$2"

BACKUP_DESTINATION_DIRECTORY="$1"
BACKUP_NAME="pg_${PG_CLUSTER_NAME}_backup_$(basename ${BACKUP_DESTINATION_DIRECTORY}).tar.gz"

source utils.sh

function test_swift() {
  local out
  out=$(/opt/backup/scli ls ${CONTAINER} 2>&1)
  process_exit_code $? "$out"
}

function do_backup() {
  if [[ -z "${BACKUP_ID}" ]]; then
    log "Backup id must be specified explicitly"
  fi
  log BACKUP_ID
  log "BACKUP_ID"
  local validation_pipe="pg-backup-${BACKUP_ID}.pipe"
  local pg_basebackup_stderr_file="pg-backup-${BACKUP_ID}.error.log"
  local validation_stderr_file="pg-backup-validation-${BACKUP_ID}.error.log"

  register_delete_on_exit "${validation_pipe}" "${validation_stderr_file}" "${pg_basebackup_stderr_file}"

  # Validate TAR stream on the fly.
  # This will not validate backup data itself, but will check archive's integrity.
  mkfifo "${validation_pipe}"
  tar -tz <"${validation_pipe}" > /dev/null 2> "${validation_stderr_file}" &

  log "start backup streaming to mounted storage"
  if [[ -n "$ENCRYPTION_KEY" ]]; then
     BACKUP_NAME="pg_backup_$(basename ${BACKUP_DESTINATION_DIRECTORY})_enc.tar.gz"
    log "Encryption key is set will encrypt backup"
    $PG_BASEBACKUP -h "${POSTGRES_HOST}" -p "${POSTGRES_PORT}" -U "${REPLICATION_USER}" -D - -X fetch --format=tar --gzip 2> "${pg_basebackup_stderr_file}" \
    | tee "${validation_pipe}" | openssl enc -aes-256-cbc -nosalt -pass pass:"$ENCRYPTION_KEY" > "${BACKUP_DESTINATION_DIRECTORY}/${BACKUP_NAME}"
  else
    $PG_BASEBACKUP -h "${POSTGRES_HOST}" -p "${POSTGRES_PORT}" -U "${REPLICATION_USER}" -D - -X fetch --format=tar --gzip 2> "${pg_basebackup_stderr_file}" \
    | tee "${validation_pipe}" > "${BACKUP_DESTINATION_DIRECTORY}/${BACKUP_NAME}"
  fi

  # PIPESTATUS can be overridden, so need to keep it.
  local exit_codes=(${PIPESTATUS[@]})
  local pg_basebackup_exit_code=${exit_codes[0]}

  # Wait for TAR validation to complete.
  wait $!
  local validation_exit_code=$?

  local validation_stderr="$(cat ${validation_stderr_file})"
  local pg_basebackup_log="$(cat ${pg_basebackup_stderr_file})"

  process_exit_code ${pg_basebackup_exit_code} "pg_basebackup has failed. Details: ${pg_basebackup_log}"
  process_exit_code ${validation_exit_code} "Backup archive integrity validation not passed. This backup will be marked as failed. Details: ${validation_stderr}"
}

function do_swift_backup() {
  local pg_backup_pipe="pg-backup-${BACKUP_DESTINATION_DIRECTORY}.pipe"
  local pg_basebackup_error_file="pg-backup-${BACKUP_DESTINATION_DIRECTORY}.error.log"
  local pg_backup_validation_error_file="pg-backup-validation-${BACKUP_DESTINATION_DIRECTORY}.error.log"
  local swift_upload_log_file="pg-backup-${BACKUP_DESTINATION_DIRECTORY}.log"
  local swift_object_path="${CONTAINER}/${BACKUP_DESTINATION_DIRECTORY}/${BACKUP_NAME}"

  log "Streaming backup to Swift under path: ${swift_object_path}"

  mkfifo "${pg_backup_pipe}"
  tar -tz <"${pg_backup_pipe}" > /dev/null 2> "${pg_backup_validation_error_file}" &
  # Validate TAR stream on the fly.
  # This will not validate backup data itself, but will check archive's integrity.
  if [[ -n "$ENCRYPTION_KEY" ]]; then
    BACKUP_NAME="pg_backup_$(basename ${BACKUP_DESTINATION_DIRECTORY})_enc.tar.gz"
    log "Encryption key is set will encrypt backup"
    $PG_BASEBACKUP -h "${POSTGRES_HOST}" -p "${POSTGRES_PORT}" -U "${REPLICATION_USER}" -D - -X fetch --format=tar --gzip 2> "${pg_basebackup_error_file}" \
    | tee "${pg_backup_pipe}" \
    | openssl enc -aes-256-cbc -nosalt -pass pass:"$ENCRYPTION_KEY" \
    | /opt/backup/scli put "${swift_object_path}" 2>&1 > "${swift_upload_log_file}"
  else
    $PG_BASEBACKUP -h "${POSTGRES_HOST}" -p "${POSTGRES_PORT}" -U "${REPLICATION_USER}" -D - -X fetch --format=tar --gzip 2> "${pg_basebackup_error_file}" \
    | tee "${pg_backup_pipe}" \
    | /opt/backup/scli put "${swift_object_path}" 2>&1 > "${swift_upload_log_file}"
  fi

  # PIPESTATUS can be overridden, so need to keep it.
  local exit_codes=(${PIPESTATUS[@]})
  local pg_backup_exit_code=${exit_codes[0]}
  local swift_upload_exit_code=${exit_codes[2]}

  # Wait for TAR validation to complete.
  wait $!
  local validation_exit_code=$?

  local swift_log="$(cat ${swift_upload_log_file})"
  local validation_log="$(cat ${pg_backup_validation_error_file})"
  local pg_basebackup_log="$(cat ${pg_basebackup_error_file})"

  rm "${pg_backup_pipe}" "${swift_upload_log_file}" "${pg_backup_validation_error_file}" "${pg_basebackup_error_file}"

  process_exit_code ${pg_backup_exit_code} "pg_basebackup has failed. Details: ${pg_basebackup_log}"
  process_exit_code ${validation_exit_code} "Backup archive integrity validation not passed. This backup will be marked as failed. Details: ${validation_log}"
  process_exit_code ${swift_upload_exit_code} "Backup uploading to Swift has failed. Details: ${swift_log}"

  log "PostgreSQL backup streaming to Swift completed successfully."
}

function remove_backup() {
  local out
  out=$(rm -f "${BACKUP_DESTINATION_DIRECTORY}/${BACKUP_NAME}" 2>&1)
  process_exit_code $? "$out"
}

function main() {
  version="$(PGPASSWORD=$POSTGRES_PASSWORD psql -h "${POSTGRES_HOST}" -p "${POSTGRES_PORT}" -U "${POSTGRES_USER}" -d postgres -c "SHOW SERVER_VERSION;" -tA | egrep -o '[0-9]{1,}\.[0-9]{1,}' | awk 'END{print $1}')"
  REPLICATION_USER="replicator"
  log "version of pgsql server is: ${version}"
  if python -c "import sys; sys.exit(0 if float("${version}") >= 17.0 else 1)"; then
    log "Using pgsql 17 bins for pg_basebackup"
    PG_BASEBACKUP="/usr/lib/postgresql/17/bin/pg_basebackup"
    BACKUP_NAME="pg_backup_$(basename ${BACKUP_DESTINATION_DIRECTORY}).tar.gz"
  elif python -c "import sys; sys.exit(0 if 16.0 <= float("${version}") < 17.0 else 1)"; then
    log "Using pgsql 16 bins for pg_basebackup"
    PG_BASEBACKUP="/usr/lib/postgresql/16/bin/pg_basebackup"
    BACKUP_NAME="pg_backup_$(basename ${BACKUP_DESTINATION_DIRECTORY}).tar.gz"
  elif python -c "import sys; sys.exit(0 if 15.0 <= float("${version}") < 16.0 else 1)"; then
    log "Using pgsql 15 bins for pg_basebackup"
    PG_BASEBACKUP="/usr/lib/postgresql/15/bin/pg_basebackup"
    BACKUP_NAME="pg_backup_$(basename ${BACKUP_DESTINATION_DIRECTORY}).tar.gz"
  elif python -c "import sys; sys.exit(0 if 14.0 <= float("${version}") < 15.0 else 1)"; then
    log "Using pgsql 14 bins for pg_basebackup"
    PG_BASEBACKUP="/usr/lib/postgresql/14/bin/pg_basebackup"
    BACKUP_NAME="pg_backup_$(basename ${BACKUP_DESTINATION_DIRECTORY}).tar.gz"
  elif python -c "import sys; sys.exit(0 if 13.0 <= float("${version}") < 14.0 else 1)"; then
    log "Using pgsql 13 bins for pg_basebackup"
    PG_BASEBACKUP="/usr/lib/postgresql/13/bin/pg_basebackup"
    BACKUP_NAME="pg_backup_$(basename ${BACKUP_DESTINATION_DIRECTORY}).tar.gz"
  elif python -c "import sys; sys.exit(0 if 12.0 <= float("${version}") < 13.0 else 1)"; then
    log "Using pgsql 12 bins for pg_basebackup"
    PG_BASEBACKUP="/usr/lib/postgresql/12/bin/pg_basebackup"
    BACKUP_NAME="pg_backup_$(basename ${BACKUP_DESTINATION_DIRECTORY}).tar.gz"
  elif python -c "import sys; sys.exit(0 if 11.0 <= float("${version}") < 12.0 else 1)"; then
    log "Using pgsql 11 bins for pg_basebackup"
    PG_BASEBACKUP="/usr/lib/postgresql/11/bin/pg_basebackup"
    BACKUP_NAME="pg_backup_$(basename ${BACKUP_DESTINATION_DIRECTORY}).tar.gz"
  elif python -c "import sys; sys.exit(0 if 10.0 <= float("${version}") < 11.0 else 1)"; then
    log "Using pgsql 10 bins for pg_basebackup"
    PG_BASEBACKUP="/usr/lib/postgresql/10/bin/pg_basebackup"
    BACKUP_NAME="pg_backup_$(basename ${BACKUP_DESTINATION_DIRECTORY}).tar.gz"
  else
    if [ "${PG_CLUSTER_NAME}" != "gpdb" ]
    then
        log "Using pgsql 9.6 bins for  pg_basebackup"
        PG_BASEBACKUP="/usr/pgsql-9.6/bin/pg_basebackup"
    else
        log "Using gpdb bins for greenplum pg_basebackup"
        TARGET_DB_ID="$(psql -h "${POSTGRES_HOST}" -p "${POSTGRES_PORT}" -U "${POSTGRES_USER}" -d postgres -c "select dbid from gp_segment_configuration where content = -1 and status = 'up' and role = 'p';" -tA )"
        PG_BASEBACKUP="/usr/local/greenplum-db/bin/pg_basebackup --target-gp-dbid="${TARGET_DB_ID}""
        REPLICATION_USER=${POSTGRES_USER}

    fi
  fi

  if [ "$STORAGE_TYPE" == "${SWIFT_STORAGE}" ]; then
    log "check swift is ready"
    test_swift
    log "do backup"
    do_swift_backup
  elif [[ "${STORAGE_TYPE}" == "${AWS_S3_STORAGE}" ]]; then
    bash aws-s3-backup.sh "${CONTAINER}" "${BACKUP_DESTINATION_DIRECTORY}"
    process_exit_code $? "PostgreSQL backup to AWS S3 has finished with an error."
  elif [[ "${STORAGE_TYPE}" == "pgbackrest" ]]; then
        log "Using pgbackrest as external backuper"
        BACKUP_ID=$(basename ${BACKUP_DESTINATION_DIRECTORY})
        log "'$BACKUP_ID'"
        log "BACKUP_DESTINATION_DIRECTORY"
        # Check cluster state via patroni API
        PGBACKREST_SRV="backrest"
        if [ "${BACKUP_FROM_STANDBY}" == "true" ]; then
          PATRONI_RESPONSE=$(curl -s pg-patroni:8008/cluster)
          if [ $? -eq 0 ]; then
              # First verify we got valid JSON response
              if echo "$PATRONI_RESPONSE" | jq . >/dev/null 2>&1; then
                  # Look for healthy streaming replicas
                  STREAMING_REPLICAS=$(echo "$PATRONI_RESPONSE" | jq -r '.members[] | select(.role=="replica" and .state=="streaming")')
                  
                  if [ ! -z "$STREAMING_REPLICAS" ]; then
                      log "Found healthy streaming replica(s)"
                      PGBACKREST_SRV="backrest-standby"
                  else
                      log "No healthy streaming replicas found, leader will be used"
                  fi
              else
                  log "Invalid JSON response from patroni API"
                  process_exit_code 1 "Invalid JSON response from patroni API"
              fi
          else
              log "Failed to query patroni API"
              process_exit_code 1 "Failed to query patroni API"
          fi
        fi

        curl  -H "Content-Type: application/json" -H "Accept: application/json" -d '{"timestamp": "'$BACKUP_ID'"}' -XPOST ${PGBACKREST_SRV}:3000/backup
  elif [[ "$STORAGE_TYPE" == "hostpath" ]] || [[ "$STORAGE_TYPE" == "pv" ]] || [[ "$STORAGE_TYPE" == "pv_label" ]] || [[ "$STORAGE_TYPE" == "provisioned" ]] || [[ "$STORAGE_TYPE" == "provisioned-default" ]]; then
    log "do backup"
    do_backup
  fi
  log "completed"
}

main "$@"
