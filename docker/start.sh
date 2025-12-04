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


RETRIES=100
SLEEP_BETWEEN_ITERATIONS=5

[[ "${DEBUG}" == 'true' ]] && set -x

function handle_master_upgrade() {
    cd /var/lib/pgsql/data/
    echo "[$(date +%Y-%m-%dT%H:%M:%S)] cur path: `pwd`"


    DB_SIZE_GB_FLOAT=$(du -sk /var/lib/pgsql/data/${DATA_DIR} | awk '{ print $1 / 1024 / 1024 }')
    DB_SIZE_GB=`printf "%.0f\n" ${DB_SIZE_GB_FLOAT}`
    PV_SIZE_GB=$(echo "${PV_SIZE}" | tr -dc '0-9')

    echo
    echo '##########################'
    echo "MIGRATION_PV_USED: $MIGRATION_PV_USED"
    echo "CLEAN_MIGRATION_PV: $CLEAN_MIGRATION_PV"
    echo "MIGRATION_PATH: $MIGRATION_PATH"
    echo "INITDB_PARAMS: $INITDB_PARAMS"
    echo "SIZE OF VOLUME: $PV_SIZE_GB Gb"
    echo "SOURCE DB SIZE: $DB_SIZE_GB Gb"
    echo "DATA_DIR: $DATA_DIR"
    echo '##########################'
    echo

    # check if there is enough space in case when migration pv is used
    if [[ "${MIGRATION_PV_USED}" =~ ^[Tt]rue$ ]]; then
        echo "[$(date +%Y-%m-%dT%H:%M:%S)] Migration PV is used, check if there is enough space for migration in migration PV"
        if [[ $DB_SIZE_GB -gt $PV_SIZE_GB ]]; then
            echo "[$(date +%Y-%m-%dT%H:%M:%S)] DB size is more than PV size, exiting ..."
            exit 1
        fi
        if [[ "${CLEAN_MIGRATION_PV}" =~ ^[Tt]rue$ ]]; then
            echo "[$(date +%Y-%m-%dT%H:%M:%S)] CLEAN_MIGRATION_PV set to True, All data from MIGRATION PV will be deleted! "
            rm -rf /var/lib/pgsql/tmp_data/*
        fi
    else
        echo "[$(date +%Y-%m-%dT%H:%M:%S)] Migration PV is NOT used, check if there is enough space for migration in master PV"
        DOUBLE_DB_SIZE="$((${DB_SIZE_GB} * 2))"
        if [[ ${DOUBLE_DB_SIZE} -gt ${PV_SIZE_GB} ]]; then
            echo "[$(date +%Y-%m-%dT%H:%M:%S)] DB size is more than PV size, exiting ..."
            exit 1
        fi
    fi
    export PATH="/usr/lib/postgresql/${PG_VERSION_TARGET}/bin:${PATH}"

    [ -d "$MIGRATION_PATH/tmp/pg" ] && echo "[$(date +%Y-%m-%dT%H:%M:%S)] Prev upgrade dir exists, removing .." && rm -rf "$MIGRATION_PATH/tmp/pg"

    echo "[$(date +%Y-%m-%dT%H:%M:%S)] initializing target db with parameters $INITDB_PARAMS"
    /usr/lib/postgresql/"${PG_VERSION_TARGET}"/bin/initdb ${INITDB_PARAMS} --pgdata="$MIGRATION_PATH/tmp/pg"


    echo "[$(date +%Y-%m-%dT%H:%M:%S)] initialize complete, copying configs"
    mkdir "/tmp/configs/"
    cp /var/lib/pgsql/data/${DATA_DIR}/*.conf "/tmp/configs/"

    echo "turning off wal archiving"
    sed -e '/archive_command/ s/^#*/#/' -i "$MIGRATION_PATH/tmp/pg/postgresql.conf"
    sed -e '/archive_mode/ s/^#*/#/' -i "$MIGRATION_PATH/tmp/pg/postgresql.conf"
    sed -e '/ssl/ s/^#*/#/' -i "$MIGRATION_PATH/tmp/pg/postgresql.conf"
    sed -e '/archive_command/ s/^#*/#/' -i "/var/lib/pgsql/data/${DATA_DIR}/postgresql.conf"
    sed -e '/archive_mode/ s/^#*/#/' -i "/var/lib/pgsql/data/${DATA_DIR}/postgresql.conf"
    sed -e '/ssl/ s/^#*/#/' -i "/var/lib/pgsql/data/${DATA_DIR}/postgresql.conf"

    echo "[$(date +%Y-%m-%dT%H:%M:%S)] proceed with upgrade"
    echo "[$(date +%Y-%m-%dT%H:%M:%S)] Content of /tmp/configs:"
    ls -la /tmp/configs/
    echo "[$(date +%Y-%m-%dT%H:%M:%S)] Content of /tmp/configs/postgresql.conf file:"
    cat /tmp/configs/postgresql.conf

    echo "[$(date +%Y-%m-%dT%H:%M:%S)] making chmod 750 to datadir"

    chmod 750 $MIGRATION_PATH/${DATA_DIR}

    SHARED_PRELOAD_LIBRARIES=$(grep "shared_preload_libraries" /var/lib/pgsql/data/${DATA_DIR}/postgresql.conf)

    if [[ -z ${SHARED_PRELOAD_LIBRARIES} ]]; then
        echo "shared_preload_libraries is not found in PostgreSQL config, please check PostgreSQL params, exiting..."
        exit 1
    fi

    echo ${SHARED_PRELOAD_LIBRARIES} >> $MIGRATION_PATH/tmp/pg/postgresql.conf

    ls -la $MIGRATION_PATH

    echo "[$(date +%Y-%m-%dT%H:%M:%S)] Check cluster before upgrade"
      /usr/lib/postgresql/"${PG_VERSION_TARGET}"/bin/pg_upgrade \
      --old-datadir "/var/lib/pgsql/data/${DATA_DIR}" \
      --new-datadir "$MIGRATION_PATH/tmp/pg" \
      --old-bindir  "/usr/lib/postgresql/${PG_VERSION}/bin" \
      --new-bindir  "/usr/lib/postgresql/${PG_VERSION_TARGET}/bin" \
      --check \
      > /var/lib/pgsql/data/check_result

    CHECK_CODE=$?

    if [[ "$CHECK_CODE" -ne 0 ]]; then
        echo "[$(date +%Y-%m-%dT%H:%M:%S)] Check cluster before upgrade - Failed."
        echo "check exit code: ${CHECK_CODE}"
        cat /var/lib/pgsql/data/check_result
        exit 13
    fi

    if [[ "$OPERATOR" =~ ^[Tt]rue$ ]]; then
      echo "[$(date +%Y-%m-%dT%H:%M:%S)] using link parameter"
      /usr/lib/postgresql/"${PG_VERSION_TARGET}"/bin/pg_upgrade \
      --link \
      --old-datadir "/var/lib/pgsql/data/${DATA_DIR}" \
      --new-datadir "$MIGRATION_PATH/tmp/pg" \
      --old-bindir  "/usr/lib/postgresql/${PG_VERSION}/bin" \
      --new-bindir  "/usr/lib/postgresql/${PG_VERSION_TARGET}/bin"
    else
      /usr/lib/postgresql/"${PG_VERSION_TARGET}"/bin/pg_upgrade \
      --old-datadir "/var/lib/pgsql/data/${DATA_DIR}" \
      --new-datadir "$MIGRATION_PATH/tmp/pg" \
      --old-bindir  "/usr/lib/postgresql/${PG_VERSION}/bin" \
      --new-bindir  "/usr/lib/postgresql/${PG_VERSION_TARGET}/bin"
    fi

    EXIT_CODE=$?
    if [[ "$EXIT_CODE" -ne 0 ]]; then
        echo "[$(date +%Y-%m-%dT%H:%M:%S)] Error! Can not proceed, because upgrade failed, exiting"
        ls -al /var/lib/pgsql/data
        echo "[$(date +%Y-%m-%dT%H:%M:%S)] Printing all of the logs:"
        awk '{print}' /var/lib/pgsql/data/*.log
#        cat pg_upgrade_server.log
        exit 1
    fi

    echo "[$(date +%Y-%m-%dT%H:%M:%S)] Upgrade Successful"
    echo "[$(date +%Y-%m-%dT%H:%M:%S)] Sizing After Upgrade"
    du -sh /var/lib/pgsql/data/

    rm -rf "/var/lib/pgsql/data/${DATA_DIR}"

    echo "[$(date +%Y-%m-%dT%H:%M:%S)] moving new data to directory"
    echo "[$(date +%Y-%m-%dT%H:%M:%S)] from -> $MIGRATION_PATH/tmp/pg"
    echo "[$(date +%Y-%m-%dT%H:%M:%S)] to -> /var/lib/pgsql/data/${DATA_DIR}"
    mv "$MIGRATION_PATH/tmp/pg" "/var/lib/pgsql/data/${DATA_DIR}"

    /usr/lib/postgresql/"${PG_VERSION_TARGET}"/bin/pg_ctl start -D "/var/lib/pgsql/data/${DATA_DIR}"

    until psql -c "select 1" > /dev/null 2>&1 || [ $RETRIES -eq 0 ]; do
      echo "[$(date +%Y-%m-%dT%H:%M:%S)] Waiting for postgres server, $((RETRIES--)) remaining attempts..."
      sleep ${SLEEP_BETWEEN_ITERATIONS}
    done

    EXIT_CODE=$?
    if [[ "$EXIT_CODE" -ne 0 ]]; then
        echo "[$(date +%Y-%m-%dT%H:%M:%S)] Error! Can not proceed, because select failed, exiting"
        cat pg_upgrade_server.log
        exit 1
    fi

    /usr/lib/postgresql/"${PG_VERSION_TARGET}"/bin/psql -c "DROP ROLE IF EXISTS replicator; CREATE ROLE replicator with replication login password '$PGREPLPASSWORD';"
    /usr/lib/postgresql/"${PG_VERSION_TARGET}"/bin/psql -c "ALTER ROLE postgres with password '$PGPASSWORD';"

    # update extensions
    [ -f /var/lib/pgsql/data/update_extensions.sql ] && psql --username=postgres --file=/var/lib/pgsql/data/update_extensions.sql postgres

    echo "[$(date +%Y-%m-%dT%H:%M:%S)] Running vacuumdb --all --analyze-in-stages"
    /usr/lib/postgresql/"${PG_VERSION_TARGET}"/bin/vacuumdb --username=postgres --all --analyze-in-stages

    /usr/lib/postgresql/"${PG_VERSION_TARGET}"/bin/pg_ctl stop -D "/var/lib/pgsql/data/${DATA_DIR}"

    [ -f /var/lib/pgsql/data/target_version ] && rm -rf /var/lib/pgsql/data/target_version

    echo "[$(date +%Y-%m-%dT%H:%M:%S)] moving configs to directory"
    mv /tmp/configs/* "/var/lib/pgsql/data/${DATA_DIR}/"
    echo "[$(date +%Y-%m-%dT%H:%M:%S)] exiting"
}

function handle_replica_upgrade() {
    rm -rf "/var/lib/pgsql/data/${DATA_DIR}/*"
}

# restricted scc
function check_user(){
    cur_user=$(id -u)
    if [[ "$cur_user" != "26" ]]
    then
        echo "[$(date +%Y-%m-%dT%H:%M:%S)] starting as not postgres user"
        set -e

        echo "[$(date +%Y-%m-%dT%H:%M:%S)] Adding randomly generated uid to passwd file..."

        sed -i '/postgres/d' /etc/passwd

        if ! whoami &> /dev/null; then
          if [[ -w /etc/passwd ]]; then
            export USER_ID=$(id -u)
            export GROUP_ID=$(id -g)
            echo "postgres:x:${USER_ID}:${GROUP_ID}:PostgreSQL Server:${PGDATA}:/bin/bash" >> /etc/passwd
            echo "UID added ..."
          fi
        fi

    fi
}

function check_pgsql_version(){
    echo "[$(date +%Y-%m-%dT%H:%M:%S)] => Check Source And Target PGSQL version"
    # get version of data files
    PG_VERSION=$(head -n 1 "/var/lib/pgsql/data/${DATA_DIR}/PG_VERSION")

    if python -c "import sys; sys.exit(0 if 11.0 <= float("${PG_VERSION}") < 12.0 else 1)"; then
        PG_VERSION_TARGET="12"
    elif python -c "import sys; sys.exit(0 if 10.0 <= float("${PG_VERSION}") < 11.0 else 1)"; then
        PG_VERSION_TARGET="11"
    else
        PG_VERSION_TARGET="10"
    fi

    for i in {1..10}; do
      echo "[$(date +%Y-%m-%dT%H:%M:%S)] Will try to find target_version file"
      [ -f /var/lib/pgsql/data/target_version ] && echo "Target file found, will use version from this file" && \
        PG_VERSION_TARGET=`cat /var/lib/pgsql/data/target_version` && break
      sleep 1
    done

    echo "[$(date +%Y-%m-%dT%H:%M:%S)] Target version of PostgreSQL is $PG_VERSION_TARGET"
}

check_user

check_pgsql_version

if [[ "${TYPE}" == "master" ]]; then
    handle_master_upgrade
elif [[ "${TYPE}" == "replica" ]]; then
    handle_replica_upgrade
fi

exit 0
