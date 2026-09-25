#!/usr/bin/env bash
# M7/M9 领域场景验证：订阅 = GPT Plus 式（月费换每窗口 token 额度，不入账钱）。
# 依赖 gateway(18080)+router(9102)+billing(admin:9103)+mock-provider；不依赖 agent/LLM。
#   ① 订阅 REC1 → active 且余额不变（无 topup）；重复订阅被拒
#   ② 充值 ¥100 → 建单→支付→入账（10000 分）；重复支付幂等；退款 10000 分幂等（元口径照旧）
#   ③ 用 SK 走 gateway 打 deepseek-v4-flash（mock ~50 token/次）4 次 → 全 200，
#      元余额仍为 0，MeView allowance 文本已用 > 0（额度内免费，不碰钱）
#   ④ 超额：SQL 把 REC1 文本额度压到 150 → 再打 1 次 → 402（订阅额度不足，等刷新）
#   ⑤ 滚动刷新：把订阅账单 created_at 回拨滑出 5h 窗口 → 再打 → 200
#   ⑥ 改套餐 → pending_plan_id 记下；退订 → cancelled/auto_renew=0、余额不变
# 每步后：Redis 余额 == 账本求和（initial + Σtopups − Σsettled − Σpending）。
set -euo pipefail
cd "$(dirname "$0")/../.."

GATEWAY="http://127.0.0.1:18080"
ADMIN="http://127.0.0.1:9103"
MYSQL="docker exec ai-gateway-mysql mysql -uroot -proot -N"
REDIS="docker exec ai-gateway-redis redis-cli"
# 写库 SQL 经 stdin 传：Windows 下 -e 的中文 argv 会被打乱成 ??，且 JSON path 对非 ASCII 键必须加引号 $."文本"
run_sql() { docker exec -i ai-gateway-mysql mysql -uroot -proot --default-character-set=utf8mb4 ai_gateway 2>/dev/null; }

pass=0
check() { if [ "$2" = "ok" ]; then echo "  PASS  $1"; pass=$((pass+1)); else echo "  FAIL  $1"; exit 1; fi; }

# ---- 前置：独立场景租户 + 套餐目录 + 场景 key；mock 用小 token 便于额度数学确定 ----
eval "$(go run ./scripts/m7/setup.go)"
echo "==> 场景租户 TID=$TID，套餐 REC1=$REC1 REC2=$REC2 TEST=$TEST，SK=$SK"
kill_port() { local pid=$(netstat -ano 2>/dev/null | grep ":$1 " | grep LISTENING | awk '{print $5}' | head -1); [ -n "$pid" ] && taskkill //PID "$pid" //F >/dev/null 2>&1 || true; }
for p in 9201 9202; do kill_port $p; sleep 1; MOCK_PROVIDER_PORT=$p MOCK_PROVIDER_NAME=mock-${p#92} MOCK_COMPLETION_TOKENS=50 \
  ./bin/mock-provider.exe > "logs/mock-$p.log" 2>&1 & sleep 1; done
echo "  mock 9201/9202 重启完成（completion=50 token）"

bal() { $REDIS GET "billing:balance:$TID" 2>/dev/null; }
ledger() {
  local initial topups settled pending
  initial=$($MYSQL -e "USE ai_gateway; SELECT initial_quota FROM tenants WHERE id=$TID;" 2>/dev/null)
  topups=$($MYSQL -e "USE ai_gateway; SELECT COALESCE(SUM(amount),0) FROM topups WHERE tenant_id=$TID;" 2>/dev/null)
  settled=$($MYSQL -e "USE ai_gateway; SELECT COALESCE(SUM(actual_quota),0) FROM bills WHERE tenant_id=$TID AND phase='settle' AND status='settled';" 2>/dev/null)
  pending=$($MYSQL -e "USE ai_gateway; SELECT COALESCE(SUM(pre_quota),0) FROM bills WHERE tenant_id=$TID AND phase='reserve' AND status='pending';" 2>/dev/null)
  echo $((initial + topups - settled - pending))
}
assert_balance() { # $1=期望余额（分）
  local expect=$1 got; got=$(bal)
  echo "  [余额] redis=$got 账本=$(ledger)（期望 $expect 分）"
  check "Redis 余额 == $expect 且 == 账本求和" "$([ "$got" = "$expect" ] && [ "$got" = "$(ledger)" ] && echo ok)"
}
topup_sum() { $MYSQL -e "USE ai_gateway; SELECT COALESCE(SUM(amount),0) FROM topups WHERE tenant_id=$TID;" 2>/dev/null; }
# gateway 真实调用（deepseek-v4-flash → 文本档），输出 http_code
call_gw() {
  curl -s -o /dev/null -w '%{http_code}' -X POST "$GATEWAY/v1/chat/completions" \
    -H "Authorization: Bearer $SK" -H 'Content-Type: application/json' \
    -d '{"model":"deepseek-v4-flash","messages":[{"role":"user","content":"hello m7"}],"stream":false}'
}
# /admin/subscription 里某档次的 consumed（token；窗口内已用）
allowance_consumed() {
  curl -s "$ADMIN/admin/subscription?tenant_id=$TID" | jq -r --arg t "$1" '.allowance[] | select(.tier==$t) | .consumed' 2>/dev/null || echo 0
}

echo
echo "==> ① 订阅定期套餐 REC1=$REC1 → active、余额不变（无入账）、重复订阅被拒"
R=$(curl -s -X POST $ADMIN/admin/subscribe -d "{\"tenant_id\":$TID,\"plan_id\":$REC1}")
echo "  subscribe: $R"
check "① 订阅成功（active）" "$(echo "$R" | jq -e '.Status=="active"' >/dev/null && echo ok)"
check "① 订阅不入账钱（topups 合计仍 0）" "$([ "$(topup_sum)" = "0" ] && echo ok)"
assert_balance 0
R=$(curl -s -X POST $ADMIN/admin/subscribe -d "{\"tenant_id\":$TID,\"plan_id\":$REC1}")
echo "  重复订阅: $R"
check "① 重复订阅被拒（已有生效中的定期订阅）" "$(echo "$R" | jq -e 'has("error")' >/dev/null && echo ok)"

echo
echo "==> ② 充值 ¥100 → 10000 分；重复支付幂等；退款 10000 分幂等（元口径照旧）"
R=$(curl -s -X POST $ADMIN/admin/recharge -d "{\"tenant_id\":$TID,\"amount_money\":100}")
ORDER=$(echo "$R" | jq -r '.OrderNo')
echo "  建单: $R"
R=$(curl -s -X POST $ADMIN/admin/recharge/pay -d "{\"order_no\":\"$ORDER\"}")
echo "  支付: $R"
check "② 支付后订单 paid" "$(echo "$R" | jq -e '.Status=="paid"' >/dev/null && echo ok)"
assert_balance 10000
R=$(curl -s -X POST $ADMIN/admin/recharge/pay -d "{\"order_no\":\"$ORDER\"}")
check "② 重复支付幂等（余额不变）" "$([ "$(bal)" = "10000" ] && echo ok)"
R=$(curl -s -X POST $ADMIN/admin/recharge/refund -d "{\"tenant_id\":$TID,\"order_no\":\"$ORDER\"}")
echo "  退款: $R"
check "② 退款量 = 10000 分" "$(echo "$R" | jq -e '.refund_cent==10000' >/dev/null && echo ok)"
assert_balance 0
R=$(curl -s -X POST $ADMIN/admin/recharge/refund -d "{\"tenant_id\":$TID,\"order_no\":\"$ORDER\"}")
check "② 重复退款幂等（余额不变）" "$([ "$(bal)" = "0" ] && echo ok)"

echo
echo "==> ③ gateway 真实调用（订阅额度内，不扣余额）：4 次全 200，元余额仍 0，文本档已用 > 0"
codes=""
for i in 1 2 3 4; do codes="$codes $(call_gw)"; done
echo "  4 次状态码:$codes"
check "③ 4/4 全部 200" "$([ "$(echo "$codes" | tr ' ' '\n' | grep -c '^200$')" = "4" ] && echo ok)"
assert_balance 0
TXT_USED=$(allowance_consumed 文本)
echo "  文本档窗口已用 = $TXT_USED token"
check "③ 额度内不扣钱（余额仍 0）且 allowance 文本已用 > 0" "$([ "$TXT_USED" -gt 0 ] && [ "$(bal)" = "0" ] && echo ok)"

echo
echo "==> ④ 超额：SQL 把 REC1 文本额度压到 150 → 再打 1 次 → 402（额度不足）"
echo "UPDATE plans SET tier_quota = JSON_SET(tier_quota, '\$.\"文本\"', 150) WHERE id=$REC1;" | run_sql
C=$(call_gw)
echo "  超额请求状态码: $C"
check "④ 超额返回 402（订阅额度不足）" "$([ "$C" = "402" ] && echo ok)"

echo
echo "==> ⑤ 滚动刷新：回拨订阅账单 created_at 滑出 5h 窗口 → 再打 → 200"
echo "UPDATE bills SET created_at = DATE_SUB(NOW(), INTERVAL 6 HOUR) WHERE tenant_id=$TID AND funding='subscription';" | run_sql
TXT_USED2=$(allowance_consumed 文本)
echo "  回拨后文本档窗口已用 = $TXT_USED2 token"
C=$(call_gw)
echo "  刷新后请求状态码: $C"
check "⑤ 窗口滑过额度恢复 → 200" "$([ "$C" = "200" ] && echo ok)"
# 恢复 REC1 额度，避免污染后续运行/portal 展示
echo "UPDATE plans SET tier_quota = JSON_SET(tier_quota, '\$.\"文本\"', 1000000) WHERE id=$REC1;" | run_sql

echo
echo "==> ⑥ 改套餐 REC1→REC2 → pending_plan_id 下期生效；退订 → cancelled/auto_renew=0、余额不变"
R=$(curl -s -X POST $ADMIN/admin/change-plan -d "{\"tenant_id\":$TID,\"plan_id\":$REC2}")
echo "  change: $R"
PP=$($MYSQL -e "USE ai_gateway; SELECT pending_plan_id FROM subscriptions WHERE tenant_id=$TID AND status='active';" 2>/dev/null)
echo "  pending_plan_id=$PP（期望 $REC2）"
check "⑥ pending_plan_id 记为 $REC2 且余额不变" "$([ "$PP" = "$REC2" ] && [ "$(bal)" = "0" ] && echo ok)"
R=$(curl -s -X POST $ADMIN/admin/cancel-subscription -d "{\"tenant_id\":$TID}")
echo "  退订: $R"
SUB_ST=$($MYSQL -e "USE ai_gateway; SELECT CONCAT(status,'/',auto_renew) FROM subscriptions WHERE tenant_id=$TID ORDER BY id DESC LIMIT 1;" 2>/dev/null)
echo "  订阅状态: $SUB_ST（期望 cancelled/0）"
check "⑥ 退订后 status=cancelled auto_renew=0 且无退款" "$([ "$SUB_ST" = "cancelled/0" ] && [ "$(bal)" = "0" ] && echo ok)"
assert_balance 0

echo
echo "==> 最终余额一致（Redis == 账本）"
assert_balance "$(bal)"
echo
echo "M7 领域场景：$pass 项全部 PASS"
