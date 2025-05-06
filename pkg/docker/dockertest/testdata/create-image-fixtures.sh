#!/usr/bin/env bash
set -euo pipefail

SRC="amd64/alpine:3.14"
TAG="cog-test-fixture:alpine"

echo "Creating test image fixtures"

docker pull $SRC

docker tag $SRC $TAG

docker save -o alpine.tar $TAG

docker rmi $TAG

echo "Test fixtures created"
