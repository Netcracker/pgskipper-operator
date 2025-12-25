#!/bin/bash

# Spring Boot Application Build Script
# Builds multi-architecture Docker images using Docker Buildx

set -e

# Configuration
IMAGE_NAME="${IMAGE_NAME:-postgresql-failover-test}"
IMAGE_TAG="${IMAGE_TAG:-latest}"
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"
PUSH="${PUSH:-false}"

echo "===================================="
echo "Building Multi-Architecture Docker Image"
echo "===================================="
echo ""

echo "Image: ${IMAGE_NAME}:${IMAGE_TAG}"
echo "Platforms: ${PLATFORMS}"
echo "Push: ${PUSH}"
echo ""

# Check if Docker is available
command -v docker >/dev/null 2>&1 || { echo "Error: docker is not installed" >&2; exit 1; }

# Navigate to spring-app directory
cd "$(dirname "$0")/../spring-app" || exit 1

# Ensure buildx is available
if ! docker buildx version &> /dev/null; then
    echo "Error: docker buildx is not available" >&2
    exit 1
fi

# Create or use existing buildx builder
BUILDER_NAME="multiarch-builder"
if ! docker buildx inspect "${BUILDER_NAME}" &> /dev/null; then
    echo "Creating buildx builder: ${BUILDER_NAME}"
    docker buildx create --name "${BUILDER_NAME}" --use
    echo ""
else
    echo "Using existing buildx builder: ${BUILDER_NAME}"
    docker buildx use "${BUILDER_NAME}"
    echo ""
fi

# Build arguments
BUILD_ARGS=""
if [ "${PUSH}" = "true" ]; then
    BUILD_ARGS="--push"
else
    BUILD_ARGS="--load"
    # Note: --load only supports single platform, so we'll build for current platform only
    CURRENT_ARCH=$(uname -m)
    if [ "${CURRENT_ARCH}" = "x86_64" ]; then
        PLATFORMS="linux/amd64"
    elif [ "${CURRENT_ARCH}" = "aarch64" ] || [ "${CURRENT_ARCH}" = "arm64" ]; then
        PLATFORMS="linux/arm64"
    fi
    echo "Note: Loading to local Docker (--load) only supports current platform: ${PLATFORMS}"
    echo "To build for multiple platforms, set PUSH=true to push to a registry"
    echo ""
fi

echo "Building Docker image..."
echo ""

docker buildx build \
    --builder "${BUILDER_NAME}" \
    --platform "${PLATFORMS}" \
    --tag "${IMAGE_NAME}:${IMAGE_TAG}" \
    ${BUILD_ARGS} \
    .

echo ""
echo "✓ Docker image built successfully"
echo ""

# Display image info (only if loaded locally)
if [ "${PUSH}" != "true" ]; then
    echo "Image details:"
    docker images "${IMAGE_NAME}" | grep -E "REPOSITORY|${IMAGE_NAME}"
    echo ""
fi

echo "Build complete!"
echo ""
echo "To run the image:"
echo "  docker run -p 8080:8080 -e DATABASE_URL=jdbc:postgresql://host:5432/db ${IMAGE_NAME}:${IMAGE_TAG}"
echo ""
echo "To build for multiple architectures and push to registry:"
echo "  PUSH=true IMAGE_NAME=registry/image PLATFORMS=linux/amd64,linux/arm64 ./build.sh"
echo ""
echo "To inspect the image:"
echo "  docker inspect ${IMAGE_NAME}:${IMAGE_TAG}"
echo ""
