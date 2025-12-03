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

export ST_AUTH="${SWIFT_AUTH_URL}"
export ST_USER="${SWIFT_USER}"
export ST_KEY="${SWIFT_PASSWORD}"
export ST_TENANT="${TENANT_NAME}"

export PGPASSWORD="$(echo ${PGPASSWORD} | tr -d '\n' | tr -d '[[:space:]]')"

function log() {
  log_module "" "backup_uploader" "$1"
}

function log_module() {
  local priority="${1:-INFO}"
  local module="$2"
  local message="$3"

  local timestamp=$(date --iso-8601=seconds)
  echo "[${timestamp}][${priority}][category=${module}] ${message}"
}

function process_exit_code() {
  local exit_code=$1
  local message="$2"
  if [ ${exit_code} -ne 0 ];then
    log_module "ERROR" "" "${message}"
    exit 1
  fi
}

function register_delete_on_exit() {
  trap "rm -f $*" EXIT
}