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


source utils.sh

################################
#
# $1 - bucket name
# $2 - backup id
#
################################

readonly BUCKET="$1" # Go to AWS S3 terminology
readonly BACKUP_ID="$2"
BACKUP_NAME="pg_${PG_CLUSTER_NAME}_backup_${BACKUP_ID}.tar.gz"


function log() {
  log_module "$1" "aws-s3-backup" "$2"
}

function log_info() {
  log "INFO" "$1"
}

function log_error() {
  log "ERROR" "$1"
  exit 1
}

function aws_process_exit_code() {
  local exit_code=$1
  local message="$2"
  if [ ${exit_code} -ne 0 ];then
    log_error "${message}"
    exit 1
  fi
}

function smoke_aws_s3() {
  log_info "validate provided configuration and test AWS S3 storage availability"

  if [[ -z "${AWS_S3_ENDPOINT_URL}" ]]; then
    log_error "endpoint URL for AWS S3 must be specified in AWS_S3_ENDPOINT environment variable."
  fi

  # http://docs.aws.amazon.com/cli/latest/topic/config-vars.html
  if [[ -z "${AWS_ACCESS_KEY_ID}" ]]; then
    log_error "access key for AWS S3 must be specified in AWS_ACCESS_KEY_ID environment variable."
  fi

  if [[ -z "${AWS_SECRET_ACCESS_KEY}" ]]; then
    log_error "secret access key for AWS S3 must be specified in AWS_SECRET_ACCESS_KEY environment variable."
  fi

  if [[ -z "${BUCKET}" ]]; then
    log_error "bucket name must be specified in CONTAINER environment variable."
  fi

  local output=$(aws --endpoint-url "${AWS_S3_ENDPOINT_URL}" s3 ls "s3://${BUCKET}" 2>&1)
  process_exit_code $? "${output}"
}

function stream_backup_to_aws_s3() {
  if [[ -z "${BACKUP_ID}" ]]; then
    log_error "Backup id must be specified explicitly"
  fi


  unset flag_ssl_no_verify
  
  if [[ "${AWS_S3_UNTRUSTED_CERT}" == "True" || "${AWS_S3_UNTRUSTED_CERT}" == "true" ]]; then
    flag_ssl_no_verify="--no-verify-ssl"
  fi

  local s3_object_name="${BACKUP_ID}/${BACKUP_NAME}"

  # S3 required to specify "expected size" of backup to divide large stream
  # into smaller pieces for multi-part upload correctly.
  # But, unfortunately, we don't know exact backup size after gzip,
  # so we tell S3 that our backup is of size of our database (but a cake is a lie).
  local expected_backup_size=$(PGPASSWORD=$POSTGRES_PASSWORD psql --no-align \
                                    --tuples-only \
                                    -h "${POSTGRES_HOST}" \
                                    -p "${POSTGRES_PORT}" \
                                    -U "${POSTGRES_USER}" \
                                    -d postgres \
                                    -c "select sum(pg_database_size(db.name)) from (select datname as name from pg_database) db")

  log_info "expected backup size is approximately ${expected_backup_size} bytes."

  local validation_pipe="pg-backup-${BACKUP_ID}.pipe"
  local pg_basebackup_stderr_file="pg-backup-${BACKUP_ID}.error.log"
  local validation_stderr_file="pg-backup-validation-${BACKUP_ID}.error.log"
  local storage_client_stdout_file="storage-client-${BACKUP_ID}.log"

  register_delete_on_exit "${validation_pipe}" "${storage_client_stdout_file}" "${validation_stderr_file}" "${pg_basebackup_stderr_file}"

  # Validate TAR stream on the fly.
  # This will not validate backup data itself, but will check archive's integrity.
  mkfifo "${validation_pipe}"
  tar -tz <"${validation_pipe}" > /dev/null 2> "${validation_stderr_file}" &

  log_info "start backup streaming to AWS S3"
    $PG_BASEBACKUP -h "${POSTGRES_HOST}" -p "${POSTGRES_PORT}" -U "${REPLICATION_USER}" -D - -X fetch --format=tar --gzip 2> "${pg_basebackup_stderr_file}" \
    | tee "${validation_pipe}" \
    | aws --endpoint-url "${AWS_S3_ENDPOINT_URL}" ${flag_ssl_no_verify} \
          s3 cp - "s3://${BUCKET}/${AWS_S3_PREFIX}/${s3_object_name}" \
          --expected-size "${expected_backup_size}" 2> "${storage_client_stdout_file}"

  # PIPESTATUS can be overridden, so need to keep it.
  local exit_codes=(${PIPESTATUS[@]})
  local pg_basebackup_exit_code=${exit_codes[0]}
  local storage_client_exit_code=${exit_codes[2]}

  # Wait for TAR validation to complete.
  wait $!
  local validation_exit_code=$?

  local storage_client_stdout="$(cat ${storage_client_stdout_file})"
  local validation_stderr="$(cat ${validation_stderr_file})"
  local pg_basebackup_log="$(cat ${pg_basebackup_stderr_file})"

  aws_process_exit_code "${pg_basebackup_exit_code}" "pg_basebackup has failed. Details: ${pg_basebackup_log}"
  aws_process_exit_code "${validation_exit_code}" "Backup archive integrity validation not passed. This backup will be marked as failed. Details: ${validation_stderr}"
  aws_process_exit_code "${storage_client_exit_code}" "Backup uploading to AWS S3 has failed. Details: ${storage_client_stdout}"

  log_info "completed"
}

function main() {

  version="$(PGPASSWORD=$POSTGRES_PASSWORD psql -h "${POSTGRES_HOST}" -p "${POSTGRES_PORT}" -U "${POSTGRES_USER}" -d postgres -c "SHOW SERVER_VERSION;" -tA | egrep -o '[0-9]{1,}\.[0-9]{1,}')"
  REPLICATION_USER="replicator"

  log "version of pgsql server is: ${version}"

  if python -c "import sys; sys.exit(0 if float("${version}") >= 16.0 else 1)"; then
    log "Using pgsql 16 bins for pg_basebackup"
    PG_BASEBACKUP="/usr/lib/postgresql/16/bin/pg_basebackup"
    BACKUP_NAME="pg_backup_$(basename ${BACKUP_ID}).tar.gz"
  elif python -c "import sys; sys.exit(0 if 15.0 <= float("${version}") < 16.0 else 1)"; then
    log "Using pgsql 15 bins for pg_basebackup"
    PG_BASEBACKUP="/usr/lib/postgresql/15/bin/pg_basebackup"
    BACKUP_NAME="pg_backup_$(basename ${BACKUP_ID}).tar.gz"
  elif python -c "import sys; sys.exit(0 if 14.0 <= float("${version}") < 15.0 else 1)"; then
    log "Using pgsql 14 bins for pg_basebackup"
    PG_BASEBACKUP="/usr/lib/postgresql/14/bin/pg_basebackup"
    BACKUP_NAME="pg_backup_$(basename ${BACKUP_ID}).tar.gz"
  elif python -c "import sys; sys.exit(0 if 13.0 <= float("${version}") < 14.0 else 1)"; then
    log "Using pgsql 13 bins for pg_basebackup"
    PG_BASEBACKUP="/usr/lib/postgresql/13/bin/pg_basebackup"
    BACKUP_NAME="pg_backup_$(basename ${BACKUP_ID}).tar.gz"
  elif python -c "import sys; sys.exit(0 if 12.0 <= float("${version}") < 13.0 else 1)"; then
    log "Using pgsql 12 bins for pg_basebackup"
    PG_BASEBACKUP="/usr/lib/postgresql/12/bin/pg_basebackup"
    BACKUP_NAME="pg_backup_$(basename ${BACKUP_ID}).tar.gz"
  elif python -c "import sys; sys.exit(0 if 11.0 <= float("${version}") < 12.0 else 1)"; then
    log "Using pgsql 11 bins for pg_basebackup"
    PG_BASEBACKUP="/usr/lib/postgresql/11/bin/pg_basebackup"
    BACKUP_NAME="pg_backup_$(basename ${BACKUP_ID}).tar.gz"
  elif python -c "import sys; sys.exit(0 if 10.0 <= float("${version}") < 11.0 else 1)"; then
    log "Using pgsql 10 bins for pg_basebackup"
    PG_BASEBACKUP="/usr/lib/postgresql/10/bin/pg_basebackup"
    BACKUP_NAME="pg_backup_$(basename ${BACKUP_ID}).tar.gz"
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

  smoke_aws_s3
  stream_backup_to_aws_s3
}

main "$@"
