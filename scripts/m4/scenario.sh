#!/usr/bin/env bash
# M4 场景验证：对账收敛。
# 验证 3 件事：
#   1) 三种「崩溃残留」pending 账单被对账统一收敛（无 marker→全退，有 marker→补结算，kill 网关→全退）
#   2) 收敛后 Redis 余额 == MySQL 账本求和（独立 SQL 计算，不是程序自说自话）
#   3) 故意污染 Redis 余额后，reconciler -once 的 rebuild 能从账本重建回来
# 前置：billing/router/mock-provider×2/gateway 已启动（修复版 billing），docker compose 正常。
set -euo pipefail
cd "$(dirname "$0")/../.."

GATEWAY="${GATEWAY_URL:-http://127.0.0.1:18080}"
KEY="sk-demo-8f3a2b1c9d4e5f60"
MYSQL="docker exec ai-gateway-mysql mysql -uroot -proot -N"
REDIS="docker exec ai-gateway-redis redis-cli"

pass=0
check() { if [ "$2" = "ok" ]; then echo "PASS  $1"; pass=$((pass+1)); else echo "FAIL  $1"; exit 1; fi; }

# ---- 前置断言：对账前两条 m4-recon 状态 ----
go run ./scripts/m4/make_state.go
nomark_status=$($MYSQL -e "USE ai_gateway; SELECT status FROM bills WHERE request_id='m4-recon-nomark' AND phase='reserve';" 2>/dev/null)
settle_status=$($MYSQL -e "USE ai_gateway; SELECT status FROM bills WHERE request_id='m4-recon-settle' AND phase='reserve';" 2>/dev/null)
settle_marker=$($MYSQL -e "USE ai_gateway; SELECT IF(usage_reported_at IS NULL,'NULL','SET') FROM bills WHERE request_id='m4-recon-settle' AND phase='reserve';" 2>/dev/null)
echo "  before recon: nomark=$nomark_status(无marker), settle=$settle_status(marker=$settle_marker)"
check "崩溃残留①：Reserve后无marker → pending" "$([ "$nomark_status" = "pending" ] && echo ok)"
check "崩溃残留②：ReportUsage后有marker → pending" "$([ "$settle_status" = "pending" ] && [ "$settle_marker" = "SET" ] && echo ok)"

# ---- 真实场景：kill -9 gateway 打在流式请求中途（usage 帧之前）----
before_rid=$($MYSQL -e "USE ai_gateway; SELECT COALESCE(MAX(id),0) FROM bills;" 2>/dev/null)
MARKER="$(mktemp)"
python - "$GATEWAY" "$KEY" "$MARKER" <<'PY' &
import json,sys,http.client,time
base,key,marker=sys.argv[1],sys.argv[2],sys.argv[3]
host,port=base.replace('http://','').split(':')
conn=http.client.HTTPConnection(host,int(port))
body=json.dumps({'model':'mock-model','stream':True,'messages':[{'role':'user','content':'hello'}]}).encode()
conn.request('POST','/v1/chat/completions',body,{'Authorization':'Bearer '+key,'Content-Type':'application/json'})
resp=conn.getresponse()
n=0
for _ in range(6):
    line=resp.readline().decode('utf-8','replace').strip()
    if line.startswith('data:') and not line.startswith('data: [DONE]'): n+=1
    if n>=2: break
open(marker,'w').write('2')          # 已读 2 帧，通知脚本可以 kill 了
time.sleep(6)                        # 挂住连接，给脚本时间 kill gateway
conn.close()
PY
PY_PID=$!
for i in $(seq 1 30); do [ -f "$MARKER" ] && break; sleep 0.1; done
GW_PID=$(netstat -ano 2>/dev/null | grep ":18080" | grep LISTENING | awk '{print $5}' | head -1)
echo "  killing gateway pid=$GW_PID mid-stream (usage 帧前)"
taskkill //PID "$GW_PID" //F >/dev/null 2>&1 || true
wait $PY_PID 2>/dev/null || true
rm -f "$MARKER"
sleep 1
killed_rid=$($MYSQL -e "USE ai_gateway; SELECT MAX(id) FROM bills;" 2>/dev/null)
killed_row=$($MYSQL -e "USE ai_gateway; SELECT request_id,status,IF(usage_reported_at IS NULL,'NULL','SET') FROM bills WHERE id=$killed_rid;" 2>/dev/null)
echo "  killed-stream bill id=$killed_rid : $killed_row"
check "kill 网关中途 → 新 bill 为 pending 无 marker" "$(echo "$killed_row" | grep -q 'pending	NULL' && echo ok)"

# 注：gateway 已在上面被杀，调用方需在脚本外重启（本脚本其余步骤只依赖 billing）。

# ---- 跑对账（grace=1s 模拟「已过宽限期」；写操作走 billing 幂等 RPC）----
recon_out=$(./bin/reconciler.exe -once -grace 1 2>&1 | grep -E "one-shot|recon " | tail -8)
echo "  $recon_out" | head -6

# ---- 断言 1：三类账单全收敛 ----
nomark_status=$($MYSQL -e "USE ai_gateway; SELECT status FROM bills WHERE request_id='m4-recon-nomark' AND phase='reserve';" 2>/dev/null)
settle_status=$($MYSQL -e "USE ai_gateway; SELECT status FROM bills WHERE request_id='m4-recon-settle' AND phase='reserve';" 2>/dev/null)
killed_status=$($MYSQL -e "USE ai_gateway; SELECT status FROM bills WHERE id=$killed_rid;" 2>/dev/null)
settle_row=$($MYSQL -e "USE ai_gateway; SELECT actual_quota,delta_quota FROM bills WHERE request_id='m4-recon-settle' AND phase='settle';" 2>/dev/null)
echo "  after recon: nomark=$nomark_status, settle=$settle_status, killed=$killed_status"
echo "  settle row: $settle_row"
check "① 无marker → 冲正全退" "$([ "$nomark_status" = "reversed" ] && echo ok)"
check "② 有marker → 补结算(actual=150,delta=50)" "$([ "$settle_status" = "settled" ] && [ "$settle_row" = "150	50" ] && echo ok)"
check "③ kill网关中途 → 冲正全退" "$([ "$killed_status" = "reversed" ] && echo ok)"

# ---- 断言 2：Redis 余额 == 账本求和（独立 SQL 计算）----
initial=$($MYSQL -e "USE ai_gateway; SELECT initial_quota FROM tenants WHERE id=1;" 2>/dev/null)
settled_sum=$($MYSQL -e "USE ai_gateway; SELECT COALESCE(SUM(actual_quota),0) FROM bills WHERE phase='settle' AND status='settled';" 2>/dev/null)
pending_sum=$($MYSQL -e "USE ai_gateway; SELECT COALESCE(SUM(pre_quota),0) FROM bills WHERE phase='reserve' AND status='pending';" 2>/dev/null)
expected=$((initial - settled_sum - pending_sum))
redis_bal=$($REDIS GET billing:balance:1 2>/dev/null)
echo "  ledger: initial=$initial - settled=$settled_sum - pending=$pending_sum = $expected ; redis=$redis_bal"
check "收敛后 Redis 余额 == 账本求和" "$([ "$redis_bal" = "$expected" ] && echo ok)"

# ---- 断言 3：污染 Redis → rebuild 从账本重建回来 ----
$REDIS SET billing:balance:1 424242 >/dev/null
./bin/reconciler.exe -once -grace 1 >/dev/null 2>&1 || true   # 常驻/单轮都带 rebuild
redis_bal=$($REDIS GET billing:balance:1 2>/dev/null)
echo "  after rebuild: redis=$redis_bal (期望 $expected)"
check "污染后 rebuild 重建投影" "$([ "$redis_bal" = "$expected" ] && echo ok)"

echo
echo "M4 scenario: $pass 项全部 PASS"
