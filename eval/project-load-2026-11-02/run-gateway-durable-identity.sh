#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
suffix="$$-$RANDOM"
mysql_name="auth-cache-test-mysql-$suffix"
redis_name="auth-cache-test-redis-$suffix"
cleanup() { docker rm -f "$mysql_name" "$redis_name" >/dev/null 2>&1 || true; }
trap cleanup EXIT
docker run -d --name "$mysql_name" -e MYSQL_ROOT_PASSWORD=isolated -e MYSQL_DATABASE=billing_compare -p 127.0.0.1::3306 docker.m.daocloud.io/library/mysql:8.0.40 >/dev/null
docker run -d --name "$redis_name" -p 127.0.0.1::6379 docker.m.daocloud.io/library/redis:7-alpine >/dev/null
ready=0
for i in {1..300}; do
 if docker exec "$mysql_name" mysqladmin ping -h 127.0.0.1 -uroot -pisolated --silent >/dev/null 2>&1; then ready=1; break; fi
 sleep 1
done
if [[ "$ready" != 1 ]]; then echo 'isolated MySQL not ready' >&2; exit 1; fi
mysql_port=$(docker port "$mysql_name" 3306/tcp | head -1 | awk -F: '{print $NF}')
redis_port=$(docker port "$redis_name" 6379/tcp | head -1 | awk -F: '{print $NF}')
export COMPARISON_MYSQL_DSN="root:isolated@tcp(127.0.0.1:${mysql_port})/billing_compare?charset=utf8mb4&parseTime=true&loc=Local"
export COMPARISON_REDIS_ADDR="127.0.0.1:${redis_port}"
export SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk GOPROXY=off GOTOOLCHAIN=local AUTH_CACHE_LOAD=1
printf 'head=%s\n' "$(git rev-parse HEAD)"
printf 'mysql_image=%s\n' "$(docker image inspect docker.m.daocloud.io/library/mysql:8.0.40 --format '{{.Id}}')"
printf 'redis_image=%s\n' "$(docker image inspect docker.m.daocloud.io/library/redis:7-alpine --format '{{.Id}}')"
tmp_dir=$(mktemp -d)
trap 'rm -rf "$tmp_dir"; cleanup' EXIT
export IDENTITY_TMP="$tmp_dir"
python3 - <<'PYOVERLAY'
import json,os,pathlib
root=pathlib.Path.cwd();tmp=pathlib.Path(os.environ['IDENTITY_TMP']);fixture=root/'eval/billing-comparison-2026-11-02/router_event_process_test.go'
s=fixture.read_text().replace('"bufio"','"bufio"\n "encoding/json"\n "errors"\n "net/http"\n "net/http/httptest"\n "sync/atomic"\n "github.com/frostloom/ai-gateway/internal/gateway/client"',1)
target=tmp/'router_event_process_test.go';target.write_text(s+(root/'eval/project-load-2026-11-02/gateway_durable_identity_regression.go.txt').read_text())
replace={str(fixture):str(target),str(root/'internal/gateway/client/router.go'):str(root/'eval/project-load-2026-11-02/gateway_router_identity_candidate.go.txt')}
(tmp/'overlay.json').write_text(json.dumps({'Replace':replace}))
PYOVERLAY
go test -race -overlay "$tmp_dir/overlay.json" ./eval/billing-comparison-2026-11-02 -run '^TestGatewayReportIdentityDurableRetry$' -count=3 -v
