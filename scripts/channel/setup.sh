#!/usr/bin/env bash
# M10 真实上游渠道演示：照国内聚合 API 的做法接入一个真实 DeepSeek 渠道。
# 网关用「渠道自己的 key」转发到 api.deepseek.com，仍按目录价对租户计量计费，
# 渠道列表给出累计售价/上游成本/毛利（毛利 = 售价 − 成本）——定价权与差价模型落地。
#
# 前置：billing(9103)/router(9102)/gateway(18080) 已用 M10 最新代码重建重启
#       （改了 Provider 模型 / router /route / gateway 转发，须重新 build）。
#       .env 需有 AGENT_LLM_API_KEY（真实 DeepSeek key，与 AI 客服共用）。
# 真实转发依赖外网可达；不可达时调用失败会走熔断，机制仍可用 mock 渠道演示。
set -euo pipefail
cd "$(dirname "$0")/../.."

GATEWAY="http://127.0.0.1:18080"
ADMIN="http://127.0.0.1:9103"

# 1) 真实 DeepSeek key（先看环境变量，再读 .env）
KEY="${AGENT_LLM_API_KEY:-}"
if [ -z "$KEY" ]; then
  KEY=$(grep -E '^AGENT_LLM_API_KEY=' .env | head -1 | cut -d= -f2- | tr -d '\r')
fi
if [ -z "$KEY" ]; then
  echo "!! .env 缺 AGENT_LLM_API_KEY（真实 DeepSeek key），无法接真实上游"
  exit 1
fi

# 2) 目录补模型 deepseek-chat（同名校对真实上游；售价 in=100 / out=200 分每百万）
echo "==> 目录 upsert 模型 deepseek-chat（售价 in=100 / out=200 分每百万）"
docker exec -i ai-gateway-mysql mysql -uroot -proot --default-character-set=utf8mb4 ai_gateway 2>/dev/null <<'SQL'
INSERT INTO models (vendor, model_id, name, input_price_cent, output_price_cent, context_len, tags, status)
VALUES ('DeepSeek','deepseek-chat','DeepSeek Chat',100,200,128000,JSON_ARRAY('文本','轻量'),0)
ON DUPLICATE KEY UPDATE
  input_price_cent=VALUES(input_price_cent), output_price_cent=VALUES(output_price_cent),
  context_len=VALUES(context_len), tags=VALUES(tags), status=0;
SQL

# 3) 注册真实渠道 deepseek-official（权重 1000 压倒 mock 通配渠道，让 deepseek-chat 走真实上游）
#    上游成本示例：in=27 / out=110 分每百万（以 DeepSeek 官方当前价为准，可改；仅影响毛利展示）
echo "==> 注册真实渠道 deepseek-official -> https://api.deepseek.com"
curl -s -X POST "$ADMIN/admin/providers" -H 'Content-Type: application/json' -d "{
  \"name\": \"deepseek-official\",
  \"base_url\": \"https://api.deepseek.com\",
  \"upstream_key\": \"$KEY\",
  \"models\": \"[\\\"deepseek-chat\\\"]\",
  \"weight\": 1000,
  \"status\": 0,
  \"cost_in_cent\": 27,
  \"cost_out_cent\": 110
}" | jq '{id, name, base_url, models, key_set:(if .upstream_key=="" then false else true end), cost_in_cent, cost_out_cent, status}'

# 4) 真实调用：demo 租户 key 打 deepseek-chat（demo 余额 ¥100 足够）
#    mock 渠道是 ["*"] 通配会竞走，用「回复含 mock 固定串」判定并重试（权重 1000 基本一次命中真实）。
echo "==> 真实调用 deepseek-chat（网关转发 api.deepseek.com，按目录价对 demo 计费）"
RESP=""
for i in 1 2 3 4 5; do
  RESP=$(curl -s -m 60 "$GATEWAY/v1/chat/completions" \
    -H "Authorization: Bearer sk-demo-8f3a2b1c9d4e5f60" -H 'Content-Type: application/json' \
    -d '{"model":"deepseek-chat","messages":[{"role":"user","content":"你好，用一句话介绍你自己"}],"stream":false}')
  if echo "$RESP" | jq -re '.choices[0].message.content' 2>/dev/null | grep -q "mock provider"; then
    echo "  第 $i 次命中 mock 通配回退，重试…"
    sleep 1
    continue
  fi
  break
done
echo "$RESP" | jq -r '.usage | "  真实 usage: in=\(.prompt_tokens) out=\(.completion_tokens) token"' 2>/dev/null \
  || echo "  （真实调用未成功：检查网络/上游日志；失败会计入渠道熔断，机制不受影响）"
echo "$RESP" | jq -r '.choices[0].message.content' 2>/dev/null | head -3

# 5) 每百万 token 毛利（售价 − 成本，聚合 API 的定价权/差价模型）
echo "==> 每百万 token 毛利（deepseek-chat 售价 in=100/out=200 分 vs 渠道成本 in=27/out=110 分）"
docker exec -i ai-gateway-mysql mysql -uroot -proot -N --default-character-set=utf8mb4 ai_gateway 2>/dev/null <<'SQL'
SELECT CONCAT(model_id,'  售价 in/out=',input_price_cent,'/',output_price_cent,
              '  成本 in/out=',p.cost_in_cent,'/',p.cost_out_cent,
              '  每百万毛利=',(input_price_cent-p.cost_in_cent)+(output_price_cent-p.cost_out_cent),' 分')
FROM models m JOIN providers p ON p.name='deepseek-official'
WHERE m.model_id='deepseek-chat';
SQL

# 6) 渠道列表 + 累计售价/成本/毛利（单位：分；分=0.01 元，几十 token 的调用不足 1 分，毛利随用量累积）
echo "==> 渠道列表 + 累计售价/成本/毛利（单位：分）"
curl -s "$ADMIN/admin/providers" | jq '.items[] | {id, name, status, models, cost_in_cent, cost_out_cent, selling_cent, cost_cent, margin_cent}'
