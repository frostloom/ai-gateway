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
go test -race ./eval/billing-comparison-2026-11-02 -run '^TestReserveConflictingTenant$' -count=1 -v

# The repository's integration fixture hardcodes 3307; substitute only that
# fixture via Go's overlay, never altering the source or touching that service.
export COMPARISON_TMP="$tmp_dir" COMPARISON_PORT="$mysql_port"
if [[ "${BILLING_COMPARE_TERMINAL_REPAIR:-0}" == 1 ]]; then
  mkdir "$tmp_dir/repair"
  python3 eval/billing-comparison-2026-11-02/build_terminal_repair_overlay.py "$tmp_dir/repair"
  shasum -a 256 "$tmp_dir/repair/mysql.go"
fi
if [[ "${BILLING_COMPARE_LEDGER_SINGLE:-0}" == 1 ]]; then
  mkdir "$tmp_dir/ledger"
  python3 eval/billing-comparison-2026-11-02/build_ledger_single_overlay.py "$tmp_dir/ledger"
  shasum -a 256 "$tmp_dir/ledger/plan.go"
fi
python3 - <<'PYOVERLAY'
import json, os, pathlib
root = pathlib.Path.cwd()
source = root / 'internal/billing/store/reserve_test.go'
target = pathlib.Path(os.environ['COMPARISON_TMP']) / 'reserve_test.go'
text = source.read_text()
assert '127.0.0.1:3307' in text
text = text.replace('root:root@tcp(127.0.0.1:3307)/', 'root:isolated@tcp(127.0.0.1:' + os.environ['COMPARISON_PORT'] + ')/')
target.write_text(text)
mapping = {str(source): str(target)}
(pathlib.Path(os.environ['COMPARISON_TMP']) / 'baseline-overlay.json').write_text(json.dumps({'Replace': dict(mapping)}))
if os.environ.get('BILLING_COMPARE_TERMINAL_REPAIR') == '1':
    mapping.update(json.loads((pathlib.Path(os.environ['COMPARISON_TMP']) / 'repair/overlay.json').read_text())['Replace'])
if os.environ.get('BILLING_COMPARE_LEDGER_SINGLE') == '1':
    mapping.update(json.loads((pathlib.Path(os.environ['COMPARISON_TMP']) / 'ledger/overlay.json').read_text())['Replace'])
    profile_source = root / 'internal/billing/store/cash_profile_test.go'
    profile_text = profile_source.read_text()
    old = '''aggregates := metrics["sql.ledger_aggregate"].(map[string]any)["count"]
	initial := metrics["sql.ledger_initial"].(map[string]any)["count"]
	if aggregates != 3 || initial != 1 {
		t.Fatalf("ledger read twice: aggregate=%v initial=%v; want 3 and 1", aggregates, initial)
	}'''
    new = '''aggregate, ok := metrics["sql.ledger_aggregate"].(map[string]any)
	if !ok || aggregate["count"] != 1 || metrics["sql.ledger_initial"] != nil {
		t.Fatalf("single ledger statement expected: metrics=%v", metrics)
	}'''
    assert profile_text.count(old) == 1
    profile_target = pathlib.Path(os.environ['COMPARISON_TMP']) / 'cash_profile_test.go'
    profile_target.write_text(profile_text.replace(old, new))
    mapping[str(profile_source)] = str(profile_target)
(pathlib.Path(os.environ['COMPARISON_TMP']) / 'overlay.json').write_text(json.dumps({'Replace': mapping}))
PYOVERLAY
export REDIS_ADDR="$COMPARISON_REDIS_ADDR"
go test -race -overlay "$tmp_dir/overlay.json" ./internal/billing/store -run '^(TestReserveConcurrentNoOversell|TestReserveIdempotentSameRequest|TestReserveConcurrentSameRequest|TestReserveInsufficient|TestSettleFullFlow|TestSettleIdempotent|TestReverseIdempotent|TestReverseAfterSettled|TestRebuildBalance|TestRebuildBalanceIdempotent|TestReserveOutboxIdempotent|TestFullFlowOutboxEvents|TestReserveSubscriptionAllowance|TestStrictSubscriptionReserveReplay|TestTerminalSQLFailureRebuildAndRetry)$' -count=1 -v
if [[ "${BILLING_COMPARE_LEDGER_AB:-0}" == 1 ]]; then
  if [[ "${BILLING_COMPARE_LEDGER_SINGLE:-0}" != 1 ]]; then echo 'ledger A/B requires candidate overlay' >&2; exit 1; fi
  for round in 1 2 3; do
    if [[ "$round" == 2 ]]; then modes="candidate baseline"; else modes="baseline candidate"; fi
    for mode in $modes; do
      if [[ "$mode" == baseline ]]; then target="$tmp_dir/baseline-overlay.json"; else target="$tmp_dir/overlay.json"; fi
      echo "ledger A/B round=$round mode=$mode"
      BILLING_CASH_LOAD=1 BILLING_CASH_PROFILE=1 BILLING_CASH_LOAD_DURATION=2s BILLING_CASH_LOAD_TAG="round${round}-${mode}" go test -race -overlay "$target" ./internal/billing/store -run '^TestCashLoad/tenants(1|8)_workers(8|32)$' -count=1 -v
    done
  done
fi
if [[ "${BILLING_COMPARE_RUN_LOAD:-0}" == 1 ]]; then
  for run in 1 2 3; do
    echo "load repetition=$run"
    BILLING_CASH_LOAD=1 BILLING_CASH_PROFILE=1 BILLING_CASH_LOAD_DURATION=2s go test -race -overlay "$tmp_dir/overlay.json" ./internal/billing/store -run '^TestCashLoad$' -count=1 -v
  done
fi
if [[ "${BILLING_COMPARE_RUN_FULL_STORE:-0}" == 1 ]]; then
  go test -race -overlay "$tmp_dir/overlay.json" ./internal/billing/store -count=1 -v
fi
if [[ "${BILLING_COMPARE_RUN_CANDIDATE:-0}" == 1 ]]; then
  go test -race ./eval/billing-comparison-2026-11-02 -run '^TestCandidateCorrectness$' -count=1 -v
fi
if [[ "${BILLING_COMPARE_RUN_PAIRED:-0}" == 1 ]]; then
  BILLING_COMPARE_LOAD=1 go test -race ./eval/billing-comparison-2026-11-02 -run '^TestComparisonLoad$' -count=1 -v
fi
if [[ "${BILLING_COMPARE_RUN_SOAK:-0}" == 1 ]]; then
  BILLING_COMPARE_LOAD=1 BILLING_COMPARE_SOAK=1 go test -race ./eval/billing-comparison-2026-11-02 -run '^TestComparisonLoad$' -count=1 -v
fi
if [[ "${BILLING_COMPARE_REDIS_VALUE:-0}" == 1 ]]; then
  export COMPARISON_REDIS_CONTAINER="$redis_name" COMPARISON_MYSQL_CONTAINER="$mysql_name"
  BILLING_COMPARE_READ_LOAD=1 go test -race ./eval/billing-comparison-2026-11-02 -run '^(TestRedisOutageValue|TestBalanceReadValue|TestMySQLDownCachedRead)$' -count=1 -v
fi
if [[ "${BILLING_COMPARE_HYBRID:-0}" == 1 ]]; then
  go test -race ./eval/billing-comparison-2026-11-02 -run '^TestHybridCacheBoundaries$' -count=1 -v
fi
if [[ "${BILLING_COMPARE_HYBRID_LOAD:-0}" == 1 ]]; then
  BILLING_COMPARE_LOAD=1 BILLING_COMPARE_HYBRID_LOAD=1 go test -race ./eval/billing-comparison-2026-11-02 -run '^TestComparisonLoad$' -count=1 -v
fi
if [[ "${BILLING_COMPARE_STABILITY_RUN:-0}" == 1 ]]; then
  go test -race ./eval/billing-comparison-2026-11-02 -run '^TestCandidateKilledTransaction$' -count=1 -v
  BILLING_COMPARE_LOAD=1 BILLING_COMPARE_STABILITY=1 go test -race ./eval/billing-comparison-2026-11-02 -run '^TestComparisonLoad$' -count=1 -v
fi
if [[ "${BILLING_COMPARE_GATEWAY_BUDGET:-0}" == 1 ]]; then
  go test -race ./eval/billing-comparison-2026-11-02 -run '^TestGatewayBudgetLoad$' -count=1 -v
fi
