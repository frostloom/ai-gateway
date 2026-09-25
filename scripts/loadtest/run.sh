#!/usr/bin/env bash
# M5 压测：默认并发 300 × 60s 打非流式，压测期间同时对账常驻（证明并发安全）。
# 结束打印 P50/P95/P99/吞吐 + 三类一致性断言（余额==账本 / 无错误 / settle 无重复）。
# 用法：bash scripts/loadtest/run.sh [并发] [时长]，如 `bash scripts/loadtest/run.sh 500 60s`
set -euo pipefail
cd "$(dirname "$0")/../.."

C="${1:-300}"
D="${2:-60s}"

echo "==> 用高限流阈值重启 gateway（600/min 撑不住高并发）"
GW_PID=$(netstat -ano 2>/dev/null | grep ":18080" | grep LISTENING | awk '{print $5}' | head -1)
[ -n "$GW_PID" ] && taskkill //PID "$GW_PID" //F >/dev/null 2>&1 || true
sleep 1
GATEWAY_PORT=18080 RATE_LIMIT_PER_MIN=100000 ./bin/gateway.exe > logs/gateway.log 2>&1 &
sleep 2

echo "==> 清理可能残留的对账进程（Git Bash 的 kill 杀不掉 Windows exe，必须 taskkill）"
for pid in $(tasklist | grep -i reconciler | awk '{print $2}'); do
  taskkill //PID "$pid" //F >/dev/null 2>&1 || true
done

echo "==> 启动对账常驻（interval=5s 只跑 sweep；rebuild 是维护操作，压测后由 loadtest 跑）"
./bin/reconciler.exe -interval 5 -grace 600 -no-rebuild > logs/reconciler.log 2>&1 &
sleep 1

echo "==> 压测 ${C} 并发 × ${D}"
go run ./scripts/loadtest -c "$C" -d "$D"
RC=$?

# Git Bash 的 $! 不是 Windows PID（杀不掉 exe），收尾按进程名统一清
for pid in $(tasklist | grep -i reconciler | awk '{print $2}'); do
  taskkill //PID "$pid" //F >/dev/null 2>&1 || true
done
echo "==> 对账已停止"
exit $RC
