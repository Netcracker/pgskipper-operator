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


function log {
    echo -e "[$(date +'%H:%M:%S')] $2$1\e[m"
}

function validate_binary {
    command -v $1 &> /dev/null
    if [ $? -gt 0 ] ; then
        log "Please install $1"
        exit 1
    fi
}

function validate_python_package {
    local package="$1"
    local package_pip_name="$2"
    local package_version="$3"
    python -c "import $package"
    if [ $? -gt 0 ] ; then
        log "Please install python package $package"
        [[ -n "$package_pip_name" ]] && log "To install $package execute command 'sudo pip install $package_pip_name'."
        exit 1
    fi

    if [[ -n "${package_version}" ]] ; then
        package_current_version=$(pip3 show ${package_pip_name} | grep -e '^Version:' | cut -d ':'  -f2)
        if [ $? -gt 0 ] ; then
            log "Cannot get version of $2 via pip"
            exit 1
        fi
        cmp_result=$(python -c "from pkg_resources import parse_version;  print(parse_version('${package_current_version}'.strip()) >= parse_version('${package_version}'))")
        if [ $? -gt 0 ] ; then
            log "Cannot compare version of ${package_pip_name} with desired version ${package_version}. Current version: ${package_current_version}."
            exit 1
        fi
        if [[ "${cmp_result}" != "True" ]] ; then
            log "Installed version of ${package_pip_name} is too old. Please install version ${package_version} or above. Current version: ${package_current_version}."
            exit 1
        fi
    fi

}

function get_replicas {
  REPL_NAME=${1}
  gr_replicas=($(oc get pods --config="$OC_CONFIG_FILE" | grep -v deploy | grep Running | grep ${REPL_NAME} | cut -d\  -f1))
  echo "${gr_replicas[*]}"
}

function get_pod_ip(){
    oc --config="$OC_CONFIG_FILE" get pod $1 -o json | jq -r '.status.podIP'
}

confirm() {
	while(true); do
		read -p "Continue (y/n)?" choice
		case "$choice" in
			y|Y ) return 0;;
			n|N ) return 1;;
		esac
	done
}