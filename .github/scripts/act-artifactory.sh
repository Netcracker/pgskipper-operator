#!/usr/bin/env bash
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
builder=${ACT_ARTIFACTORY_BUILDER:-act-artifactory}
if [[ ! $builder =~ ^[a-zA-Z0-9][a-zA-Z0-9_.-]*$ ]]; then
  echo "Invalid Buildx builder name: $builder" >&2
  exit 2
fi

context=${ACT_ARTIFACTORY_DOCKER_CONTEXT:-$(docker context show)}
endpoint=$(docker context inspect "$context" --format '{{.Endpoints.docker.Host}}')
proxy_settings=$(python3 - "$endpoint" "${DOCKER_CONFIG:-$HOME/.docker}/config.json" <<'PY'
import json
import os
import re
import sys
from pathlib import Path
from urllib.parse import urlsplit

endpoint, config_file = sys.argv[1:]
config = Path(config_file)
proxies = json.loads(config.read_text()).get("proxies", {}) if config.is_file() else {}
settings = proxies.get(endpoint, proxies.get("default", {}))
for variable, key in (("HTTP_PROXY", "httpProxy"), ("HTTPS_PROXY", "httpsProxy"), ("NO_PROXY", "noProxy")):
    lower = variable.lower()
    value = os.environ.get(variable, os.environ.get(lower, settings.get(key, "")))
    if value:
        if variable == "NO_PROXY":
            valid = re.fullmatch(r"[a-zA-Z0-9.*_,:/\[\]-]+", value)
        else:
            url = urlsplit(value)
            valid = (
                url.scheme in ("http", "https")
                and url.hostname
                and url.username is None
                and url.password is None
                and url.path in ("", "/")
                and not url.query
                and not url.fragment
                and re.fullmatch(r"https?://[a-zA-Z0-9._:\[\]-]+/?", value)
            )
        if not valid:
            sys.exit(f"{variable} must contain an unauthenticated proxy setting for this profile.")
        print(f"{variable}\t{value}")
        print(f"{lower}\t{value}")
PY
)
runner_image=artifactory.netcracker.com:17114/catthehacker/ubuntu:act-latest@sha256:beea02d9454a21c06de0104bb1f8c1542b88dd88f33022f9d36b52b48e022bd7
profile_args=(
  -P "ubuntu-latest=$runner_image"
  --network bridge
  --env ACT_ARTIFACTORY_BUILDER="$builder"
  --env BUILDX_CONFIG=/var/lib/act-artifactory/buildx
  --container-options "--volume ${builder}-config:/var/lib/act-artifactory"
)
while IFS=$'\t' read -r name value; do
  if [[ -n $name ]]; then
    profile_args+=(--env "$name=$value")
  fi
done <<< "$proxy_settings"

exec act "$@" "${profile_args[@]}"
