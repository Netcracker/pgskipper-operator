#!/bin/bash
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

