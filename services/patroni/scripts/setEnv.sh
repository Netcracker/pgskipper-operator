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


LOG_LEVEL=${LOG_LEVEL:-info}

if [[ -z "${POD_IDENTITY}" ]];
then
    echo "POD_IDENTITY is not defined, defaulting POD_IDENTITY"
    export POD_IDENTITY="node"
fi

ROOT_DIR_NAME="postgresql_${POD_IDENTITY}"
ROOT_DIR="/var/lib/pgsql/data/${ROOT_DIR_NAME}"

#####################################################################################################
## Calculate PG_CONF_MAX_CONNECTIONS parameters based on merge of old and new configurations
PG_MAX_CONNECTIONS=${PG_MAX_CONNECTIONS:-200}
export PG_CONF_MAX_CONNECTIONS=${PG_CONF_MAX_CONNECTIONS:-$PG_MAX_CONNECTIONS}
echo "PG_CONF_MAX_CONNECTIONS=${PG_CONF_MAX_CONNECTIONS}"

#####################################################################################################
## Calculate max_prepared_transactions
PG_CONF_MAX_PREPARED_TRANSACTIONS=${PG_CONF_MAX_PREPARED_TRANSACTIONS:-200}

#####################################################################################################
## Calculate memory setting based on memory limit and max_connections settings
PG_RESOURCES_LIMIT_MEM=${PG_RESOURCES_LIMIT_MEM:-256Mi}
echo "PG_RESOURCES_LIMIT_MEM=${PG_RESOURCES_LIMIT_MEM}"

declare -A m
# see https://kubernetes.io/docs/concepts/configuration/manage-compute-resources-container/#meaning-of-memory
m=(["ki"]=1 ["mi"]=1024 ["gi"]=1048576 ["k"]=1 ["m"]=1000 ["g"]=1000000 )

if [[ ${PG_RESOURCES_LIMIT_MEM} =~ ^([0-9]*)([A-Za-z]+) ]] ; then
    ei=`echo ${BASH_REMATCH[2]} | tr '[:upper:]' '[:lower:]'`
    LIM_VAL=${BASH_REMATCH[1]}
    LIM_MULT=${m[$ei]}
    _PG_RESOURCES_LIMIT_MEM_KIB=$(expr $LIM_VAL \* $LIM_MULT)
elif [[ ${PG_RESOURCES_LIMIT_MEM} =~ ^([0-9]*) ]] ; then
    _PG_RESOURCES_LIMIT_MEM_KIB=$(expr ${PG_RESOURCES_LIMIT_MEM} / 1024)
else
    echo "Cannot parse PG_RESOURCES_LIMIT_MEM value ${PG_RESOURCES_LIMIT_MEM}"
    exit 1
fi

echo "_PG_RESOURCES_LIMIT_MEM_KIB=${_PG_RESOURCES_LIMIT_MEM_KIB}"
patroni_mem=$((${_PG_RESOURCES_LIMIT_MEM_KIB}>512000?102400:51200))
_PG_AVAILABLE_KIB=$(expr ${_PG_RESOURCES_LIMIT_MEM_KIB} - ${patroni_mem})
PG_AVAILABLE=${_PG_AVAILABLE_KIB}kB
#    echo "PG_AVAILABLE=${PG_AVAILABLE}"

_PG_CONF_SHARED_BUFFERS_KIB=$(expr ${_PG_AVAILABLE_KIB} / 4 )
export PG_CONF_SHARED_BUFFERS=${_PG_CONF_SHARED_BUFFERS_KIB}kB
#    echo "PG_CONF_SHARED_BUFFERS=${PG_CONF_SHARED_BUFFERS}"

export PG_CONF_EFFECTIVE_CACHE_SIZE=$(expr ${_PG_AVAILABLE_KIB} - ${_PG_CONF_SHARED_BUFFERS_KIB})kB
#    echo "PG_CONF_EFFECTIVE_CACHE_SIZE=${PG_CONF_EFFECTIVE_CACHE_SIZE}"

_PG_CONF_WORK_MEM_KIB=$(expr ${_PG_CONF_SHARED_BUFFERS_KIB} / ${PG_CONF_MAX_CONNECTIONS})
_PG_CONF_WORK_MEM_KIB=$(($_PG_CONF_WORK_MEM_KIB>64?$_PG_CONF_WORK_MEM_KIB:64))
export PG_CONF_WORK_MEM=${_PG_CONF_WORK_MEM_KIB}kB
#    echo "PG_CONF_WORK_MEM=${PG_CONF_WORK_MEM}"

export PG_CONF_MAINTENANCE_WORK_MEM=$(expr ${_PG_CONF_SHARED_BUFFERS_KIB} / 4)kB
#    echo "PG_CONF_MAINTENANCE_WORK_MEM=${PG_CONF_MAINTENANCE_WORK_MEM}"
