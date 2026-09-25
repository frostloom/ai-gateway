#!/usr/bin/env bash
# M5 场景验证：多 provider 故障切换 + 熔断 OPEN→HALF-OPEN→CLOSED + AutoBan。
#
# 演员表（mock-a 是「出故障的 provider」）：
#   mock-a = provider_id=1, 9201, weight=5（先打挂、后治愈、再打挂到 AutoBan）
#   mock-b = provider_id=2, 9202, weight=3（健康）
#   mock-c = provider_id=3, 9203, weight=1（健康）
#
# 验证链：
#   1) mock-a 100% 失败 → 网关靠 failover 仍全 200；mock-a 连败 ≥3 → OPEN 出候选集
#   2) cooldown 后 HALF-OPEN 放探测 → 探测失败 → 回 OPEN（consecutive_fail 涨）
#   3) 治愈 mock-a → HALF-OPEN 探测成功 → recover → CLOSED，回候选集
#   4) 再打挂 mock-a → 跨周期累计 6 连败 → AutoBan：providers.status=2，移出候选集
#   5) 人工解除 ban（SQL 复位）+ 重启 router → mock-a 回归
#
# 前置：billing/router/mock-provider×3/gateway(18080) 已启动；
#       router 需带 ROUTER_FAIL_THRESHOLD=3 ROUTER_AUTOBAN_AFTER=6 ROUTER_COOLDOWN=1 启动。
set -euo pipefail
cd "$(dirname "$0")/../.."

GATEWAY="http://127.0.0.1:18080"
ROUTER="http://127.0.0.1:9102"
KEY="sk-demo-8f3a2b1c9d4e5f60"
MYSQL="docker exec ai-gateway-mysql mysql -uroot -proot -N"
ROUTERLOG="logs/router.log"

pass=0
check() { if [ "$2" = "ok" ]; then echo "PASS  $1"; pass=$((pass+1)); else echo "FAIL  $1"; exit 1; fi; }

# 重置 router（熔断状态是进程内存，重启保证每轮从 CLOSED 开始）
restart_router() {
  kill_port 9102; sleep 1
  ROUTER_PORT=9102 ROUTER_FAIL_THRESHOLD=3 ROUTER_AUTOBAN_AFTER=6 ROUTER_COOLDOWN=1 \
    ./bin/router.exe > "$ROUTERLOG" 2>&1 &
  sleep 2
}

# 发一次非流式请求，输出 http_code
call() {
  curl -s -o /dev/null -w '%{http_code}' -X POST "$GATEWAY/v1/chat/completions" \
    -H "Authorization: Bearer $KEY" -H 'Content-Type: application/json' \
    -d '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello m5"}],"stream":false}'
}

# /breakers 里 provider 1 的状态；没有 Report 过则返回 no_report
p1_state() {
  curl -s "$ROUTER/breakers" | python -c "
import sys,json
s=[x for x in json.load(sys.stdin) if x['provider_id']==1]
print(s[0]['state'] if s else 'no_report')"
}
p1_fails() {
  curl -s "$ROUTER/breakers" | python -c "
import sys,json
s=[x for x in json.load(sys.stdin) if x['provider_id']==1]
print(s[0]['consecutive_fail'] if s else '-1')"
}

# 按端口杀进程 + 重启 mock-provider
kill_port() { local pid=$(netstat -ano 2>/dev/null | grep ":$1 " | grep LISTENING | awk '{print $5}' | head -1); [ -n "$pid" ] && taskkill //PID "$pid" //F >/dev/null 2>&1 || true; }
restart_mock_a() {  # $1 = fail_rate
  kill_port 9201; sleep 1
  MOCK_PROVIDER_PORT=9201 MOCK_PROVIDER_NAME=mock-a MOCK_FAIL_RATE="$1" MOCK_COMPLETION_TOKENS=100 \
    ./bin/mock-provider.exe > logs/mock-a.log 2>&1 &
  sleep 1
  echo "  mock-a restarted, fail_rate=$1"
}
restart_mock_bc() {  # failover 目标也切成小响应（默认 5000 token 大响应拖慢时序，熔断会提前进 HALF-OPEN）
  for p in "9202:mock-b" "9203:mock-c"; do
    port=${p%%:*}; name=${p##*:}
    kill_port "$port"; sleep 1
    MOCK_PROVIDER_PORT="$port" MOCK_PROVIDER_NAME="$name" MOCK_COMPLETION_TOKENS=100 \
      ./bin/mock-provider.exe > "logs/$name.log" 2>&1 &
    sleep 1
  done
  echo "  mock-b/mock-c restarted, completion=100"
}

# ================= 阶段 1：故障注入 → failover 保活 + mock-a OPEN =================
echo "[阶段1] mock-a 100% 失败 → failover 全 200，mock-a OPEN"
restart_router
restart_mock_a 100
restart_mock_bc
codes=""
for i in $(seq 1 12); do codes="$codes $(call)"; done
bad=$(echo "$codes" | tr ' ' '\n' | grep -v '^$' | grep -cv '^200$' || true)
echo "  12 个请求状态码:$codes"
check "failover：mock-a 全挂仍 12/12 全部 200" "$([ "$bad" -eq 0 ] && echo ok)"
check "熔断：mock-a 连败≥3 → OPEN" "$([ "$(p1_state)" = "open" ] && [ "$(p1_fails)" -ge 3 ] && echo ok)"

# ================= 阶段 2：HALF-OPEN 探测失败 → 回 OPEN =================
echo "[阶段2] cooldown 后 HALF-OPEN 探测失败 → 回 OPEN"
sleep 1.5
for i in $(seq 1 6); do call >/dev/null; done
echo "  探测后 state=$(p1_state) fails=$(p1_fails)"
check "探测失败 → 回 OPEN 且连败累计到≥4" "$([ "$(p1_state)" = "open" ] && [ "$(p1_fails)" -ge 4 ] && echo ok)"

# ================= 阶段 3：治愈 → 探测成功 → CLOSED 回候选集 =================
echo "[阶段3] 治愈 mock-a → HALF-OPEN 探测成功 → CLOSED"
restart_mock_a 0
sleep 1.5
for i in $(seq 1 10); do call >/dev/null; done
echo "  治愈后 state=$(p1_state) fails=$(p1_fails)"
check "探测成功 → recover → CLOSED 且连败清零" "$([ "$(p1_state)" = "closed" ] && [ "$(p1_fails)" -eq 0 ] && echo ok)"
a_picks=0
for i in $(seq 1 20); do
  pid=$(curl -s "$ROUTER/route?model=deepseek-v4-flash" | python -c "import sys,json;print(json.load(sys.stdin).get('provider_id',''))" 2>/dev/null)
  [ "$pid" = "1" ] && a_picks=$((a_picks+1))
done
echo "  /route 20 次选中 mock-a: $a_picks"
check "CLOSED 后 mock-a 回归候选集（被选中>0）" "$([ "$a_picks" -gt 0 ] && echo ok)"

# ================= 阶段 4：再打挂 → AutoBan（status=2 移出候选集） =================
echo "[阶段4] 再打挂 mock-a → 跨周期累计 6 连败 → AutoBan"
restart_mock_a 100
banned=0
for i in $(seq 1 80); do
  call >/dev/null
  st=$($MYSQL -e "USE ai_gateway; SELECT status FROM providers WHERE id=1;" 2>/dev/null)
  if [ "$st" = "2" ]; then banned=1; break; fi
done
st=$($MYSQL -e "USE ai_gateway; SELECT status,consecutive_fail FROM providers WHERE id=1;" 2>/dev/null)
cf=$(p1_fails)
echo "  providers id=1: status,consecutive_fail=$st ; breaker fails=$cf"
check "AutoBan：providers.status=2 持久化" "$([ "$banned" -eq 1 ] && echo ok)"
check "AutoBan：熔断连败累计≥6" "$([ "$cf" -ge 6 ] && echo ok)"
b_picks=0
for i in $(seq 1 20); do
  pid=$(curl -s "$ROUTER/route?model=deepseek-v4-flash" | python -c "import sys,json;print(json.load(sys.stdin).get('provider_id',''))" 2>/dev/null)
  [ "$pid" = "1" ] && b_picks=$((b_picks+1))
done
check "AutoBan 后 mock-a 完全移出候选集（20/20 无选中）" "$([ "$b_picks" -eq 0 ] && echo ok)"

# ================= 阶段 5：人工解除 ban → 恢复 =================
echo "[阶段5] 人工解除 ban（SQL 复位 + 重启 router）→ mock-a 回归"
$MYSQL -e "USE ai_gateway; UPDATE providers SET status=0, consecutive_fail=0 WHERE id=1;" 2>/dev/null
restart_mock_a 0
restart_router
c=$(call)
c_picks=0
for i in $(seq 1 20); do
  pid=$(curl -s "$ROUTER/route?model=deepseek-v4-flash" | python -c "import sys,json;print(json.load(sys.stdin).get('provider_id',''))" 2>/dev/null)
  [ "$pid" = "1" ] && c_picks=$((c_picks+1))
done
echo "  解除后请求=$c, mock-a 选中 $c_picks/20"
check "解除 ban 后请求成功 + mock-a 回归候选集" "$([ "$c" = "200" ] && [ "$c_picks" -gt 0 ] && echo ok)"

echo
echo "M5 scenario: $pass 项全部 PASS"
