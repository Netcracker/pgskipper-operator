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


NAMESPACE=$(cat /var/run/secrets/kubernetes.io/serviceaccount/namespace)

# user defined variables
export OC_PROJECT=${OC_PROJECT:-${NAMESPACE}}
export OC_CONFIG_FILE=${OC_CONFIG_FILE:-oc_config_file.yaml}
export PG_CLUSTER_NAME=${PG_CLUSTER_NAME:-patroni}
export PRESERVE_OLD_FILES=${PRESERVE_OLD_FILES:-no}
export RESTORE_VERSION=${RESTORE_VERSION:-}
export SKIP_REPLICATION_CHECK=${SKIP_REPLICATION_CHECK:-False}

#############################################################################
#### Recovery target settings according to
#### https://www.postgresql.org/docs/9.6/static/recovery-target-settings.html
#############################################################################

export RECOVERY_TARGET_INCLUSIVE=${RECOVERY_TARGET_INCLUSIVE:-true}
export RECOVERY_TARGET_TIMELINE=${RECOVERY_TARGET_TIMELINE:-latest}

# specify only one parameter
export RECOVERY_TARGET_TIME=${RECOVERY_TARGET_TIME:-}
export RECOVERY_TARGET_NAME=${RECOVERY_TARGET_NAME:-}
export RECOVERY_TARGET_XID=${RECOVERY_TARGET_XID:-}
export RECOVERY_TARGET=${RECOVERY_TARGET:-}
#############################################################################


#############################################################################
#### oc client settings
#############################################################################
export OC_PATH=${OC_PATH:-oc}
export OC_SKIP_TLS_VERIFY=${OC_SKIP_TLS_VERIFY:-true}
#############################################################################

# system vars (do not change)
export PG_DEPL_NAME=${PG_DEPL_NAME:-pg-${PG_CLUSTER_NAME}}

export LOG_ERROR="\e[0;101m"
export LOG_SUCCESS="\e[0;32m"
export LOG_INFO="\e[0;104m"

