#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
name="idem-test-redis-$$-$RANDOM"
cleanup() { docker rm -f "$name" >/dev/null 2>&1 || true; }
trap cleanup EXIT
docker run -d --name "$name" -p 127.0.0.1::6379 docker.m.daocloud.io/library/redis:7-alpine >/dev/null
port=$(docker port "$name" 6379/tcp | head -1 | awk -F: '{print $NF}')
export IDEM_TEST_REDIS_ADDR="127.0.0.1:$port" IDEM_TEST_REDIS_CONTAINER="$name"
export SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk GOPROXY=off GOTOOLCHAIN=local
printf 'head=%s\n' "$(git rev-parse HEAD)"
printf 'redis_image=%s\n' "$(docker image inspect docker.m.daocloud.io/library/redis:7-alpine --format '{{.Id}}')"
go test -race ./eval/project-load-2026-11-02 -run '^TestIdem' -count=1 -v
