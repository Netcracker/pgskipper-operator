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


BACKUP_DAEMON_DIR="$(cd $(dirname $0); pwd)"

DEFAULT_WORKERS_NUMBER=2 # Suppose that one accepted long-running request and another deals with quick ones.
DEFAULT_WORKERS_TIMEOUT=21600 # 6 hours

PUBLIC_ENDPOINTS_WORKERS_NUMBER=${PUBLIC_ENDPOINTS_WORKERS_NUMBER:-${DEFAULT_WORKERS_NUMBER}}
PRIVATE_ENDPOINTS_WORKERS_NUMBER=${PRIVATE_ENDPOINTS_WORKERS_NUMBER:-${DEFAULT_WORKERS_NUMBER}}
ARCHIVE_ENDPOINTS_WORKERS_NUMBER=${ARCHIVE_ENDPOINTS_WORKERS_NUMBER:-${DEFAULT_WORKERS_NUMBER}}

WORKERS_TIMEOUT=${WORKERS_TIMEOUT:-${DEFAULT_WORKERS_TIMEOUT}}
LOG_FORMAT=${LOG_FORMAT:-generic}

function update_logging_configuration() {
  sed -i s/formatter=.*/formatter=${LOG_FORMAT}/g ${BACKUP_DAEMON_DIR}/logging.conf
  if [[ -z "${LOG_LEVEL}" ]]; then
    return
  fi
  sed -i s/level=.*/level=${LOG_LEVEL}/g ${BACKUP_DAEMON_DIR}/logging.conf

}

function check_ipv6() {
  if [[ -d "/proc/sys/net/ipv6" ]]; then
    echo "IPv6 availiable will listen on [::]"
    export LISTEN_ADDR="[::]"
  else
    echo "IPv6 is not availiable will listen on 0.0.0.0"
    export LISTEN_ADDR="0.0.0.0"
  fi
}


function ride_unicorn() {
  params="--daemon --timeout ${WORKERS_TIMEOUT} --preload --enable-stdio-inheritance --log-config ${BACKUP_DAEMON_DIR}/logging.conf"
  if [[ $TLS ]]; then
    params="${params} --certfile=/certs/tls.crt --keyfile=/certs/tls.key"
  fi
  # 2 workers
  # Roughly, one is for /health endpoint since it's quite fast operation and should not block,
  # second one is for /backup which just schedules backups.
  echo ${params}
  gunicorn --chdir "${BACKUP_DAEMON_DIR}/gunicorn" \
           ${params} \
           -w ${PUBLIC_ENDPOINTS_WORKERS_NUMBER} \
           --pythonpath ${BACKUP_DAEMON_DIR} \
           -b "${LISTEN_ADDR}":8080 \
           public:app

  # 2 workers
  # /get is long-running operation so it will be blocked
  # /list and /evict are quite short.
  gunicorn --chdir "${BACKUP_DAEMON_DIR}/gunicorn" \
           ${params} \
           -w ${PRIVATE_ENDPOINTS_WORKERS_NUMBER} \
           --pythonpath ${BACKUP_DAEMON_DIR} \
           -b "${LISTEN_ADDR}":8081 \
           private:app

  gunicorn --chdir "${BACKUP_DAEMON_DIR}/gunicorn" \
           ${params} \
           -w ${ARCHIVE_ENDPOINTS_WORKERS_NUMBER} \
           --pythonpath ${BACKUP_DAEMON_DIR} \
           -b "${LISTEN_ADDR}":8082 \
           archive:app

  # Granular backup are async, so one worker should be more than enough.
  # Timeout is just 60 seconds since it should be enough to parse and validate any request.
  gunicorn --chdir ${BACKUP_DAEMON_DIR}/granular \
           ${params} \
           -w 1 \
           --pythonpath ${BACKUP_DAEMON_DIR}/granular \
           -b "${LISTEN_ADDR}":9000 \
           granular:app
}

function summon_daemon() {
  python "${BACKUP_DAEMON_DIR}/postgres_backup_daemon.py"
}

function check_user(){
    cur_user=$(id -u)
    if [ "$cur_user" != "0" ]
    then
        echo "Adding randomly generated uid to passwd file..."
        sed -i '/backup/d' /etc/passwd
        if ! whoami &> /dev/null; then
          if [ -w /etc/passwd ]; then
            echo "${USER_NAME:-backup}:x:$(id -u):0:${USER_NAME:-backup} user:/backup-storage:/sbin/nologin" >> /etc/passwd
          fi
        fi
    fi
}

check_ipv6
check_user
update_logging_configuration
ride_unicorn
summon_daemon