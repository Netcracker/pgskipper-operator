#!/bin/bash
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

set -e

# pgBackRest version to build
PGBACKREST_VERSION="2.55.1"
PGBACKREST_URL="https://github.com/pgbackrest/pgbackrest/archive/release/${PGBACKREST_VERSION}.tar.gz"

# Build directory
BUILD_DIR="/tmp/pgbackrest-build"
INSTALL_PREFIX="/usr"

echo "Building pgBackRest ${PGBACKREST_VERSION} from source..."

# Create build directory
mkdir -p ${BUILD_DIR}
cd ${BUILD_DIR}

# Download and extract source
echo "Downloading pgBackRest ${PGBACKREST_VERSION}..."
wget -O pgbackrest-${PGBACKREST_VERSION}.tar.gz ${PGBACKREST_URL}
tar -xzf pgbackrest-${PGBACKREST_VERSION}.tar.gz
cd pgbackrest-release-${PGBACKREST_VERSION}

# Configure build with Meson (pgBackRest 2.51+ uses Meson instead of autotools)
echo "Configuring build with Meson..."
meson setup build \
    --prefix=${INSTALL_PREFIX} \
    --buildtype=release

# Build with Ninja
echo "Building pgBackRest..."
ninja -C build

# Install
echo "Installing pgBackRest..."
ninja -C build install

# Verify installation
echo "Verifying installation..."
pgbackrest version

# Create directories for pgBackRest
mkdir -p /var/lib/pgbackrest
mkdir -p /var/log/pgbackrest
mkdir -p /var/spool/pgbackrest

rm -rf ${BUILD_DIR}

echo "pgBackRest ${PGBACKREST_VERSION} build completed successfully!"

