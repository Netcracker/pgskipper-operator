#!/bin/bash
# Build custom pgskipper-operator images using Makefile
#
# This script builds Docker images for pgskipper-operator using the upstream Makefile.
#
# Usage:
#   ./scripts/build-operators.sh [TAG]
#
# Arguments:
#   TAG - Image tag to use (default: local)
#
# Examples:
#   ./scripts/build-operators.sh           # Build with tag "local"
#   ./scripts/build-operators.sh v1.2.3    # Build with tag "v1.2.3"

set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
EXAMPLE_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
OPERATOR_DIR="$(cd "$EXAMPLE_ROOT/../../.." && pwd)"

TAG="${1:-local}"
IMAGE_NAME="${2:-pgskipper-operator}"

echo "=========================================="
echo "Building pgskipper-operator Images"
echo "=========================================="
echo ""
echo "Tag: $TAG"
echo "Image: $IMAGE_NAME:$TAG"
echo "Operator directory: $OPERATOR_DIR"
echo ""

# Check if operator directory exists
if [ ! -d "$OPERATOR_DIR" ]; then
  echo "ERROR: pgskipper-operator directory not found at: $OPERATOR_DIR"
  echo ""
  echo "Expected pgskipper-operator root at:"
  echo "  $OPERATOR_DIR"
  echo "This script should be run from within the pgskipper-operator repository."
  exit 1
fi

# Check if Makefile exists
if [ ! -f "$OPERATOR_DIR/Makefile" ]; then
  echo "ERROR: Makefile not found at: $OPERATOR_DIR/Makefile"
  exit 1
fi

cd "$OPERATOR_DIR"

echo "Building using Makefile..."
TAG_ENV="$TAG" DOCKER_NAMES="$IMAGE_NAME:$TAG" make docker-build

echo ""
echo "=========================================="
echo "Build Complete!"
echo "=========================================="
echo ""
echo "Image built:"
echo "  - $IMAGE_NAME:$TAG"
echo ""
echo "Next steps:"
echo "  1. Deploy with local images:"
echo "     helmfile -e orbstack sync"
echo ""
