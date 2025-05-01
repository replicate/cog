#!/usr/bin/env bash
set -euo pipefail

echo "Creating test image fixtures"

docker pull alpine:latest --platform linux/amd64

docker tag alpine:latest cog-test-fixture:alpine

docker save -o alpine.tar cog-test-fixture:alpine

docker rmi cog-test-fixture:alpine

echo "Test fixtures created"
