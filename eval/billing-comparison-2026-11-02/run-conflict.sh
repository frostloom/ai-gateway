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
go test ./eval/billing-comparison-2026-11-02 -run '^TestReserveConflictingTenant$' -count=1 -v

# The repository's integration fixture hardcodes 3307; substitute only that
# fixture via Go's overlay, never altering the source or touching that service.
export COMPARISON_TMP="$tmp_dir" COMPARISON_PORT="$mysql_port"
python3 - <<'PYOVERLAY'
import json, os, pathlib
root = pathlib.Path.cwd()
source = root / 'internal/billing/store/reserve_test.go'
target = pathlib.Path(os.environ['COMPARISON_TMP']) / 'reserve_test.go'
text = source.read_text()
assert '127.0.0.1:3307' in text
text = text.replace('root:root@tcp(127.0.0.1:3307)/', 'root:isolated@tcp(127.0.0.1:' + os.environ['COMPARISON_PORT'] + ')/')
target.write_text(text)
(pathlib.Path(os.environ['COMPARISON_TMP']) / 'overlay.json').write_text(json.dumps({'Replace': {str(source): str(target)}}))
PYOVERLAY
export REDIS_ADDR="$COMPARISON_REDIS_ADDR"
go test -overlay "$tmp_dir/overlay.json" ./internal/billing/store -run '^(TestReserveConcurrentNoOversell|TestReserveIdempotentSameRequest|TestReserveConcurrentSameRequest|TestReserveInsufficient|TestSettleFullFlow|TestSettleIdempotent|TestReverseIdempotent|TestReverseAfterSettled|TestRebuildBalance|TestRebuildBalanceIdempotent|TestReserveOutboxIdempotent|TestFullFlowOutboxEvents|TestReserveSubscriptionAllowance|TestTerminalSQLFailureRebuildAndRetry)$' -count=1 -v
if [[ "${BILLING_COMPARE_RUN_LOAD:-0}" == 1 ]]; then
  for run in 1 2 3; do
    echo "load repetition=$run"
    BILLING_CASH_LOAD=1 BILLING_CASH_PROFILE=1 BILLING_CASH_LOAD_DURATION=2s go test -overlay "$tmp_dir/overlay.json" ./internal/billing/store -run '^TestCashLoad$' -count=1 -v
  done
fi
if [[ "${BILLING_COMPARE_RUN_FULL_STORE:-0}" == 1 ]]; then
  go test -overlay "$tmp_dir/overlay.json" ./internal/billing/store -count=1 -v
fi
if [[ "${BILLING_COMPARE_RUN_CANDIDATE:-0}" == 1 ]]; then
  go test ./eval/billing-comparison-2026-11-02 -run '^TestCandidateCorrectness$' -count=1 -v
fi
if [[ "${BILLING_COMPARE_RUN_PAIRED:-0}" == 1 ]]; then
  BILLING_COMPARE_LOAD=1 go test ./eval/billing-comparison-2026-11-02 -run '^TestComparisonLoad$' -count=1 -v
fi
if [[ "${BILLING_COMPARE_RUN_SOAK:-0}" == 1 ]]; then
  BILLING_COMPARE_LOAD=1 BILLING_COMPARE_SOAK=1 go test ./eval/billing-comparison-2026-11-02 -run '^TestComparisonLoad$' -count=1 -v
fi
if [[ "${BILLING_COMPARE_REDIS_VALUE:-0}" == 1 ]]; then
  export COMPARISON_REDIS_CONTAINER="$redis_name" COMPARISON_MYSQL_CONTAINER="$mysql_name"
  BILLING_COMPARE_READ_LOAD=1 go test ./eval/billing-comparison-2026-11-02 -run '^(TestRedisOutageValue|TestBalanceReadValue|TestMySQLDownCachedRead)$' -count=1 -v
fi
if [[ "${BILLING_COMPARE_HYBRID:-0}" == 1 ]]; then
  go test ./eval/billing-comparison-2026-11-02 -run '^TestHybridCacheBoundaries$' -count=1 -v
fi
if [[ "${BILLING_COMPARE_HYBRID_LOAD:-0}" == 1 ]]; then
  BILLING_COMPARE_LOAD=1 BILLING_COMPARE_HYBRID_LOAD=1 go test ./eval/billing-comparison-2026-11-02 -run '^TestComparisonLoad$' -count=1 -v
fi
if [[ "${BILLING_COMPARE_STABILITY_RUN:-0}" == 1 ]]; then
  go test ./eval/billing-comparison-2026-11-02 -run '^TestCandidateKilledTransaction$' -count=1 -v
  BILLING_COMPARE_LOAD=1 BILLING_COMPARE_STABILITY=1 go test ./eval/billing-comparison-2026-11-02 -run '^TestComparisonLoad$' -count=1 -v
fi
if [[ "${BILLING_COMPARE_GATEWAY_BUDGET:-0}" == 1 ]]; then
  go test ./eval/billing-comparison-2026-11-02 -run '^TestGatewayBudgetLoad$' -count=1 -v
fi
