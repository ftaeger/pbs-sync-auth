#!/usr/bin/env bash
# Builds the static client binary WITHOUT a Go installation on the PBS,
# using Docker only. Result: ./pbs-auth-client (static, no dependencies).
#
# Target architecture is fixed to linux/amd64 (x86_64) — this also works on an
# Apple Silicon Mac (arm64), since Go cross-compiles. The golang container is
# pulled explicitly as amd64 so the build is independent of the host arch.
set -euo pipefail
cd "$(dirname "$0")"

docker run --rm --platform linux/amd64 -v "$PWD":/src -w /src golang:1.23-alpine \
    sh -c 'CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags="-s -w" -o pbs-auth-client .'

echo "Done: $PWD/pbs-auth-client (linux/amd64)"
echo "Install:  sudo cp pbs-auth-client /usr/local/bin/ && sudo chmod +x /usr/local/bin/pbs-auth-client"
