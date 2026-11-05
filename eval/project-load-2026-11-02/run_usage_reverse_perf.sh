#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
suffix="$$-$RANDOM"
mysql_name="billing-comparison-mysql-$suffix"
redis_name="billing-comparison-redis-$suffix"
tmp_dir=$(mktemp -d)
cleanup() { docker rm -f "$mysql_name" "$redis_name" >/dev/null 2>&1 || true; rm -rf "$tmp_dir"; }
trap cleanup EXIT
# Fresh containers, ephemeral writable layers, and loopback-only random ports.
docker run -d --name "$mysql_name" -e MYSQL_ROOT_PASSWORD=isolated -e MYSQL_DATABASE=billing_compare -p 127.0.0.1::3306 docker.m.daocloud.io/library/mysql:8.0.40 >/dev/null
docker run -d --name "$redis_name" -p 127.0.0.1::6379 docker.m.daocloud.io/library/redis:7-alpine >/dev/null
ready=0
for i in {1..60}; do
  if docker exec "$mysql_name" mysqladmin ping -h 127.0.0.1 -uroot -pisolated --silent >/dev/null 2>&1; then ready=1; break; fi
  sleep 1
done
if [[ "$ready" != 1 ]]; then echo 'disposable MySQL did not start' >&2; exit 1; fi
mysql_port=$(docker port "$mysql_name" 3306/tcp | head -1 | awk -F: '{print $NF}')
redis_port=$(docker port "$redis_name" 6379/tcp | head -1 | awk -F: '{print $NF}')
export COMPARISON_MYSQL_DSN="root:isolated@tcp(127.0.0.1:${mysql_port})/billing_compare?charset=utf8mb4&parseTime=true&loc=Local"
export COMPARISON_REDIS_ADDR="127.0.0.1:${redis_port}"
export COMPARISON_TMP="$tmp_dir" COMPARISON_PORT="$mysql_port" REDIS_ADDR="$COMPARISON_REDIS_ADDR"
export SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk GOPROXY=off GOTOOLCHAIN=local
python3 - <<'PYOVERLAY'
import json, os, pathlib
root=pathlib.Path.cwd(); tmp=pathlib.Path(os.environ['COMPARISON_TMP']); replace={}
fixture=root/'internal/billing/store/reserve_test.go'; src=fixture.read_text(); old='root:root@tcp(127.0.0.1:3307)/'; assert old in src
p=tmp/'reserve_test.go';p.write_text(src.replace(old,'root:isolated@tcp(127.0.0.1:'+os.environ['COMPARISON_PORT']+')/'));replace[str(fixture)]=str(p)
fixture=root/'internal/billing/store/settle_test.go';src=fixture.read_text().replace('"context"','"context"\n "fmt"',1)
p=tmp/'settle_test.go';p.write_text(src+(root/'eval/project-load-2026-11-02/usage_reverse_regression.go.txt').read_text());replace[str(fixture)]=str(p)
(tmp/'baseline.json').write_text(json.dumps({'Replace':replace}))
replace[str(root/'internal/billing/store/mysql.go')]=str(root/'eval/project-load-2026-11-02/usage_reverse_candidate.go.txt')
(tmp/'candidate.json').write_text(json.dumps({'Replace':replace}))
PYOVERLAY
for rep in 1 2 3; do
 if [[ "$rep" == 2 ]]; then order="candidate baseline"; else order="baseline candidate"; fi
 for variant in $order; do
  echo "variant=$variant rep=$rep"
  BILLING_CASH_LOAD=1 BILLING_CASH_PROFILE=1 BILLING_CASH_LOAD_DURATION=2s go test -overlay "$tmp_dir/$variant.json" ./internal/billing/store -run '^TestCashLoad/(tenants1_workers32|tenants8_workers32)$' -count=1 -v
 done
done
