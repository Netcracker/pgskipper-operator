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


source /setEnv.sh

export PASSWD_DIR=$(dirname ${ROOT_DIR})/passwd
export HOME=/home/postgres


# prepare datadir
mkdir -p ${ROOT_DIR}
chmod 700 ${ROOT_DIR}
chown $(id -u):$(id -u) ${ROOT_DIR}


function set_property() {
    PROP_NAME=$1
    PROP_VALUE=$2
    FILE=$3
    sed -i "/^#*${PROP_NAME}[ ]*=/{h;s/.*/${PROP_NAME} = ${PROP_VALUE}/};\${x;/^\$/{s//$PROP_NAME = '$PROP_VALUE'/;H};x}" ${FILE}
}

cur_user=$(id -u)
if [ "$cur_user" != "26" ]
then
    echo "starting as not postgres user"
    set -e

    echo "Adding randomly generated uid to passwd file..."

    sed -i '/postgres/d' /etc/passwd
    export HOME=/home/pg
    if ! whoami &> /dev/null; then
      if [ -w /etc/passwd ]; then
        if [ -n "$PGBACKREST_PG2_HOST" ]; then
          echo "${USER_NAME:-postgres}:x:$(id -u):0:${USER_NAME:-postgres} user:${HOME}:/bin/sh" >> /etc/passwd
        else
          echo "${USER_NAME:-postgres}:x:$(id -u):0:${USER_NAME:-postgres} user:${ROOT_DIR}:/bin/nologin" >> /etc/passwd
        fi
      fi
    fi

fi

if [ -n "$PGBACKREST_PG1_PATH" ]; then
    echo "Create spool directory for pgbackrest..."
    mkdir -p /var/lib/pgsql/data/pgbackrest/spool
    chmod -R 770 /var/lib/pgsql/data/pgbackrest
fi

if [ -n "$PGBACKREST_PG2_HOST" ]; then
    echo "Preparation for standby backup..."
    mkdir -p ${HOME}
    chmod 700 ${HOME}
    mkdir -p ${HOME}/.ssh
    chmod 700 ${HOME}/.ssh

    cp /keys/id_rsa ${HOME}/.ssh/id_rsa
    cp /keys/id_rsa.pub ${HOME}/.ssh/id_rsa.pub
    cp /keys/id_rsa.pub ${HOME}/.ssh/authorized_keys
    cp /keys/id_rsa.pub ${HOME}/.ssh/known_hosts
    sed -i "s/ssh-rsa/pg-patroni ssh-rsa/" ${HOME}/.ssh/known_hosts

    chmod 600 ${HOME}/.ssh/id_rsa

    /usr/sbin/sshd -E /tmp/sshd.log -o PidFile=/tmp/sshd.pid
fi

# removing postmaster.pid file in case if pgsql was stoped not gracefully  
if [ -e "$ROOT_DIR/postmaster.pid" ]
then
    echo "Removing postmaster.pid file"
    rm -rf "$ROOT_DIR/postmaster.pid"
    rm -rf "$ROOT_DIR/postmaster.opts"
fi

if [[ -z "$PG_ROOT_PASSWORD" ]] ; then
    echo "Cannot start cluster with empty postgres password"
    exit 1
fi

# copy file from provided template if needed
if [[ -f /patroni-properties/patroni-config-template.yaml ]] ; then
    echo "File /patroni-properties/patroni-config-template.yaml found. Will use as template for patroni configuration."
    cp -f /patroni-properties/patroni-config-template.yaml /patroni/pg_template.yaml
else
    echo "Cannot work without /patroni-properties/patroni-config-template.yaml. Please provide template via configmap."
    exit 1
fi

if [[ "${DR_MODE}" =~ ^[Tt]rue$ ]]; then
  PATRONI_CLUSTER_MEMBER_ID="${POD_IDENTITY}-dr"
else
  PATRONI_CLUSTER_MEMBER_ID="${POD_IDENTITY}"
  DR_MODE="false"
fi

if [[ -z "${ETCD_HOST}" ]]; then
  ETCD_HOST="etcd"
fi

NODE_NAME="${POD_IDENTITY}"

export NODE_NAME
export PATRONI_CLUSTER_MEMBER_ID
export DR_MODE
export ETCD_HOST

if [[ -n "${FROM_SCRATCH}" ]]; then
  echo "Cluster from scratch. Removing everything under ${ROOT_DIR}/*"
  rm -rf ${ROOT_DIR}/*
fi

# prepare config for patroni
PG_BIN_DIR=${PG_BIN_DIR} \
PG_ROOT_PASSWORD=${PG_ROOT_PASSWORD} \
PG_REPL_PASSWORD=${PG_REPL_PASSWORD} \
LISTEN_ADDR=`hostname -i` \
PG_CLUST_NAME=${PG_CLUST_NAME} \
POD_NAMESPACE=${POD_NAMESPACE} \
envsubst < /patroni/pg_template.yaml > /patroni/pg_node.yml

echo "Prepare file with initial properties"
python3 /prepare_settings_file.py /patroni/pg_conf_initial.conf

echo "Initial properties: "
cat /patroni/pg_conf_initial.conf

echo "Apply properties to bootstrap section"
python3 /populate_patroni_config.py /patroni/pg_node.yml /patroni/pg_conf_initial.conf

echo "Config result"
cat /patroni/pg_node.yml | grep -v password

echo "Check if we have datafiles from previous start and apply required settings"

if [[ -d /var/lib/pgsql/data/data/${ROOT_DIR_NAME} ]] ; then
    echo "Find an uncommon database location"
    echo "Moving it to ROOT_DIR, might take a while"
    mv /var/lib/pgsql/data/data/${ROOT_DIR_NAME} /var/lib/pgsql/data/
    [[ $? != 0 ]] && echo "Something goes wrong, please check is moving correctly" || echo "Moving complete"
fi

required_array=("shared_buffers" "effective_cache_size" "work_mem" "maintenance_work_mem")
if [[ -f ${ROOT_DIR}/postgresql.base.conf ]] ; then
    echo "Set properties"
    cat /patroni/pg_conf_initial.conf | while read line
    do
      if ! [[ ${line} =~ \s*#.* ]]; then
        IFS='=' read -r pg_setting_name value <<< "${line}"
        for e in "${@:required_array}"; do
            if [[ "$e" == "${pg_setting_name}" ]] ; then
                echo "Setting from config file: ${pg_setting_name} = ${value}"
                set_property ${pg_setting_name} ${value} ${ROOT_DIR}/postgresql.base.conf;
            fi
        done
      fi
    done
fi

if [[ -f /certs/server.crt ]] ; then
    cp /certs/server.crt /patroni/server.crt && chmod 600 /patroni/server.crt
    cp /certs/server.key /patroni/server.key && chmod 600 /patroni/server.key
    ls -ll /
fi

# Disable coredumps to keep PV clean and free.
ulimit -c 0

# Start Patroni in the background. We need background mode to be
# able to handle TERM, which is sent by docker upon stopping
# the container. This is our chance to stop DB gracefully.
PATH="${PATH}:${PG_BIN_DIR}" patroni /patroni/pg_node.yml &

PATRONI_PID=$!
if [[ -z ${PATRONI_PID} ]]
then
    echo -e "\nERROR: could not find PID of Patroni!"
    exit 1
else
    echo -e "\nPatroni PID is ${PATRONI_PID}."
fi

function exit_handler() {
    echo "Received termination signal. Propagating it to Patroni..."

    # Handle both SIGINT and SIGTERM similarly, because Patroni
    # shuts the database down using SIGINT both on SIGINT and SIGTERM.
    kill ${PATRONI_PID}

    echo "Termination signal sent, waiting for the process to stop..."
    wait ${PATRONI_PID}
    echo "Patroni process terminated."

    echo "Try to collect controldata"
    pg_controldata -D ${ROOT_DIR}
}

trap exit_handler SIGINT SIGTERM

wait ${PATRONI_PID}

