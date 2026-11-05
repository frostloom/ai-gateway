#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
suffix="$$-$RANDOM"
mysql_name="auth-cache-test-mysql-$suffix"
redis_name="auth-cache-test-redis-$suffix"
cleanup() { docker rm -f "$mysql_name" "$redis_name" >/dev/null 2>&1 || true; }
overlay_dir=""
cleanup_all() { cleanup; if [[ -n "$overlay_dir" ]]; then rm -rf "$overlay_dir"; fi; }
trap cleanup_all EXIT
if [[ "${TEST_MYSQL_STABLE_PORT:-0}" == 1 ]]; then
 stable_port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
 mysql_publish="127.0.0.1:${stable_port}:3306"
else
 mysql_publish="127.0.0.1::3306"
fi
if [[ "${TEST_REDIS_STABLE_PORT:-0}" == 1 ]]; then
 redis_stable_port=$(python3 -c 'import socket; s=socket.socket(); s.bind(("127.0.0.1",0)); print(s.getsockname()[1]); s.close()')
 redis_publish="127.0.0.1:${redis_stable_port}:6379"
else
 redis_publish="127.0.0.1::6379"
fi
docker run -d --name "$mysql_name" -e MYSQL_ROOT_PASSWORD=isolated -e MYSQL_DATABASE=billing_compare -p "$mysql_publish" docker.m.daocloud.io/library/mysql:8.0.40 >/dev/null
docker run -d --name "$redis_name" -p "$redis_publish" docker.m.daocloud.io/library/redis:7-alpine >/dev/null
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
export COMPARISON_MYSQL_CONTAINER="$mysql_name"
export COMPARISON_REDIS_CONTAINER="$redis_name"
export SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX26.5.sdk GOPROXY=off GOTOOLCHAIN=local AUTH_CACHE_LOAD=1
printf 'head=%s\n' "$(git rev-parse HEAD)"
printf 'mysql_image=%s\n' "$(docker image inspect docker.m.daocloud.io/library/mysql:8.0.40 --format '{{.Id}}')"
printf 'redis_image=%s\n' "$(docker image inspect docker.m.daocloud.io/library/redis:7-alpine --format '{{.Id}}')"
if [[ "${TEST_ROUTER_SUCCESS_PARALLEL:-0}" == 1 || "${TEST_RECON_EARLY_USAGE:-0}" == 1 || "${TEST_PORTAL_MYSQL_SNAPSHOT:-0}" == 1 || "${TEST_MYSQL_READ_CURRENT:-0}" == 1 || "${TEST_RECON_PARALLEL_SWEEP:-0}" == 1 || "${TEST_ROUTER_REFRESH_SCOPE:-0}" == 1 || "${TEST_ROUTER_FRESH_SCOPE:-0}" == 1 || "${TEST_NONSTREAM_LIMIT:-0}" == 1 || "${TEST_AUTH_SINGLE:-0}" == 1 || "${TEST_USAGE_ATOMIC_OVERLAY:-0}" == 1 || "${TEST_LEDGER_SINGLE:-0}" == 1 || "${TEST_AUTH_POOL:-0}" == 1 || "${TEST_USAGE_RETRY:-0}" == 1 || "${TEST_USAGE_HTTP_FAILURE:-0}" == 1 || "${TEST_TENANT_IDEM:-0}" == 1 || "${TEST_ROUTER_LOCK_SCOPED:-0}" == 1 || "${TEST_ROUTER_DEADLINE:-0}" == 1 ]]; then
 overlay_dir=$(mktemp -d "${TMPDIR:-/tmp}/usage-atomic-overlay.XXXXXX")
 if [[ "${TEST_ROUTER_SUCCESS_PARALLEL:-0}" == 1 ]]; then
  python3 eval/billing-comparison-2026-11-02/build_router_success_parallel_overlay.py "$overlay_dir"
  shasum -a 256 "$overlay_dir/registry.go"
 elif [[ "${TEST_RECON_EARLY_USAGE:-0}" == 1 ]]; then
  python3 eval/billing-comparison-2026-11-02/build_recon_early_usage_overlay.py "$overlay_dir"
  shasum -a 256 "$overlay_dir/reconciler.go"
 elif [[ "${TEST_PORTAL_MYSQL_SNAPSHOT:-0}" == 1 ]]; then
  python3 eval/billing-comparison-2026-11-02/build_portal_snapshot_overlay.py "$overlay_dir"
  shasum -a 256 "$overlay_dir/plan.go"
 elif [[ "${TEST_MYSQL_READ_CURRENT:-0}" == 1 ]]; then
  python3 eval/billing-comparison-2026-11-02/build_mysql_balance_snapshot_current.py "$overlay_dir"
  shasum -a 256 "$overlay_dir/mysql.go"
 elif [[ "${TEST_RECON_PARALLEL_SWEEP:-0}" == 1 ]]; then
  python3 eval/billing-comparison-2026-11-02/build_reconciler_parallel_overlay.py "$overlay_dir"
  shasum -a 256 "$overlay_dir/reconciler.go"
  export TEST_RECON_OVERLAY_PATH="$overlay_dir/overlay.json"
 elif [[ "${TEST_ROUTER_REFRESH_SCOPE:-0}" == 1 ]]; then
  python3 eval/billing-comparison-2026-11-02/build_router_refresh_scope_overlay.py "$overlay_dir"
  shasum -a 256 "$overlay_dir/registry.go"
 elif [[ "${TEST_ROUTER_FRESH_SCOPE:-0}" == 1 ]]; then
  python3 eval/billing-comparison-2026-11-02/build_router_fresh_scope_overlay.py "$overlay_dir"
  shasum -a 256 "$overlay_dir/registry.go"
 elif [[ "${TEST_NONSTREAM_LIMIT:-0}" == 1 ]]; then
  python3 eval/billing-comparison-2026-11-02/build_nonstream_limit_overlay.py "$overlay_dir"
  shasum -a 256 "$overlay_dir/chat.go"
 elif [[ "${TEST_AUTH_SINGLE:-0}" == 1 ]]; then
  python3 eval/billing-comparison-2026-11-02/build_auth_single_overlay.py "$overlay_dir"
  shasum -a 256 "$overlay_dir/mysql.go"
 elif [[ "${TEST_ROUTER_DEADLINE:-0}" == 1 ]]; then
  python3 eval/billing-comparison-2026-11-02/build_router_deadline_overlay.py "$overlay_dir"
  shasum -a 256 "$overlay_dir/registry.go"
 elif [[ "${TEST_ROUTER_LOCK_SCOPED:-0}" == 1 ]]; then
  python3 - "$overlay_dir" <<'PYOVERLAY'
import json,pathlib,sys
root=pathlib.Path.cwd()
out=pathlib.Path(sys.argv[1])
source=root/'eval/project-load-2026-11-02/router_registry_lock_scope_current_20261105.go.txt'
(out/'overlay.json').write_text(json.dumps({'Replace':{str(root/'internal/router/registry.go'):str(source)}}))
PYOVERLAY
 elif [[ "${TEST_TENANT_IDEM:-0}" == 1 ]]; then
  python3 eval/billing-comparison-2026-11-02/build_tenant_idem_overlay.py "$overlay_dir"
 elif [[ "${TEST_USAGE_HTTP_FAILURE:-0}" == 1 ]]; then
  python3 eval/billing-comparison-2026-11-02/build_usage_failure_http_overlay.py "$overlay_dir"
 elif [[ "${TEST_USAGE_RETRY:-0}" == 1 ]]; then
  if [[ "${TEST_USAGE_RETRY_BOUNDED:-0}" == 1 ]]; then
   python3 eval/billing-comparison-2026-11-02/build_usage_bounded_overlay.py "$overlay_dir"
  elif [[ "${TEST_USAGE_RETRY_JITTER:-0}" == 1 ]]; then
   python3 eval/billing-comparison-2026-11-02/build_usage_jitter_overlay.py "$overlay_dir"
  else
   python3 eval/billing-comparison-2026-11-02/build_usage_retry_overlay.py "$overlay_dir"
  fi
 elif [[ "${TEST_AUTH_POOL:-0}" == 1 ]]; then
  python3 eval/billing-comparison-2026-11-02/build_auth_pool_overlay.py "$overlay_dir"
  cp "$overlay_dir/test-overlay.json" "$overlay_dir/overlay.json"
 elif [[ "${TEST_LEDGER_SINGLE:-0}" == 1 ]]; then
  python3 eval/billing-comparison-2026-11-02/build_ledger_single_overlay.py "$overlay_dir"
  shasum -a 256 "$overlay_dir/plan.go"
 elif [[ "${TEST_TERMINAL_REPAIR_ONLY:-0}" == 1 ]]; then
  python3 eval/billing-comparison-2026-11-02/build_terminal_repair_overlay.py "$overlay_dir"
  shasum -a 256 "$overlay_dir/mysql.go"
 else
  python3 eval/billing-comparison-2026-11-02/build_usage_atomic_overlay.py "$overlay_dir"
  shasum -a 256 "$overlay_dir/mysql.go"
 fi
 go test -race -overlay "$overlay_dir/overlay.json" ./eval/billing-comparison-2026-11-02 -run "${TEST_USAGE_RACE_PATTERN:-^TestExistingReconcilerReversesCommittedUsageAfterScan$}" -count=3 -v
else
 go test -race ./eval/billing-comparison-2026-11-02 -run "${TEST_USAGE_RACE_PATTERN:-^TestExistingReconcilerReversesCommittedUsageAfterScan$}" -count=3 -v
fi
