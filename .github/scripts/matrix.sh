#! /bin/bash
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


# Usage: ./matrix.sh .github/build-config.cfg .github/outputs/all_changed_files.json

components_file="$1"
files_file="$2"

# files=$(jq -c . "$files_file")

# Use jq to filter components where any file starts with any changeset entry in the list

jq -c --argjson files "$(cat "$files_file")" '
  .components |
  [ .[] | select(
      . as $component |
      any($files[];
        [ $component.changeset[] as $ch | startswith($ch) ] | any
      )
    )
  ]
' "$components_file"