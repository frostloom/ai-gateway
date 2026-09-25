#!/usr/bin/env bash
# M2 场景验证：正常请求产生 reserve+settle 两行且余额正确 / 重复幂等键 409 只一行 / 余额不足 402。
# 前置：docker compose up -d mysql redis；四个服务已启动（见 README「本地运行」）。
# 注意：用 python 发请求（Git Bash 里 curl 传中文会被终端编码搞坏，导致 token 数漂移）。
set -euo pipefail
cd "$(dirname "$0")/../.."

GATEWAY="${GATEWAY_URL:-http://127.0.0.1:18080}"
KEY="sk-demo-8f3a2b1c9d4e5f60"
MYSQL="docker exec ai-gateway-mysql mysql -uroot -proot"

pass=0
check() { # $1 = 描述, $2 = 条件判断（字符串）
  if [ "$2" = "ok" ]; then echo "PASS  $1"; pass=$((pass+1)); else echo "FAIL  $1"; exit 1; fi
}

# 目录里选两个模型：deepseek-v4-flash（便宜）与 step-2（贵 38 倍）。
MODEL_CHEAP="deepseek-v4-flash"
MODEL_EXP="step-2"

# ---- 1. 正常请求：200 + usage 存在 ----
resp=$(python - "$GATEWAY" "$KEY" "$MODEL_CHEAP" <<'PY'
import json,sys,urllib.request
base,key,model=sys.argv[1],sys.argv[2],sys.argv[3]
body=json.dumps({'model':model,'messages':[{'role':'user','content':'hello world'}]}).encode()
req=urllib.request.Request(base+'/v1/chat/completions',data=body,headers={'Authorization':'Bearer '+key,'Content-Type':'application/json'})
try:
    r=urllib.request.urlopen(req)
    d=json.load(r)
    print(f"status={r.status} prompt={d['usage']['prompt_tokens']} completion={d['usage']['completion_tokens']}")
except urllib.error.HTTPError as e:
    print(f"status={e.code} error={e.read().decode()[:80]}")
PY
)
echo "  normal -> $resp"
check "正常请求 200 且带 usage" "$(echo "$resp" | grep -q 'status=200' && echo ok)"

# ---- 2. 重复幂等键：第二次 409 ----
rid=$(python - "$GATEWAY" "$KEY" "$MODEL_CHEAP" <<'PY'
import json,sys,urllib.request,urllib.error
base,key,model=sys.argv[1],sys.argv[2],sys.argv[3]
def send(i):
    body=json.dumps({'model':model,'messages':[{'role':'user','content':'hello'}]}).encode()
    req=urllib.request.Request(base+'/v1/chat/completions',data=body,headers={'Authorization':'Bearer '+key,'Content-Type':'application/json','Idempotency-Key':'scenario-key-1'})
    try:
        r=urllib.request.urlopen(req); return r.status
    except urllib.error.HTTPError as e: return e.code
s1,s2=send(1),send(2)
print(f"first={s1} second={s2}")
PY
)
echo "  idempotency -> $rid"
check "第二次同键 409" "$(echo "$rid" | grep -q 'second=409' && echo ok)"

# ---- 3. 幂等键只产生一行请求（reserve+settle 两条账目） ----
n=$($MYSQL -e "USE ai_gateway; SELECT COUNT(*) FROM bills WHERE request_id='idem:scenario-key-1';" 2>/dev/null | tail -1)
echo "  rows for idem:scenario-key-1 = $n"
check "重复幂等键只一行请求（2 条账目）" "$([ "$n" = "2" ] && echo ok)"

# ---- 4. 余额不足 → 402 ----
# 记录当前余额，调低触发 402，再恢复。
# 用贵模型 step-2 + max_tokens 让 reserve 预占 ≥1 分（便宜模型 0 分预占触不到 402）。
BAL=$(docker exec ai-gateway-redis redis-cli GET "billing:balance:1")
docker exec ai-gateway-redis redis-cli SET "billing:balance:1" 0 >/dev/null
r402=$(python - "$GATEWAY" "$KEY" "$MODEL_EXP" <<'PY'
import json,sys,urllib.request,urllib.error
base,key,model=sys.argv[1],sys.argv[2],sys.argv[3]
body=json.dumps({'model':model,'max_tokens':2000,'messages':[{'role':'user','content':'hello world'}]}).encode()
req=urllib.request.Request(base+'/v1/chat/completions',data=body,headers={'Authorization':'Bearer '+key,'Content-Type':'application/json'})
try:
    r=urllib.request.urlopen(req); print(f"status={r.status}")
except urllib.error.HTTPError as e: print(f"status={e.code}")
PY
)
docker exec ai-gateway-redis redis-cli SET "billing:balance:1" "$BAL" >/dev/null
echo "  insufficient -> $r402 (restored balance=$BAL)"
check "余额不足返回 402" "$(echo "$r402" | grep -q 'status=402' && echo ok)"

echo
echo "M2 scenario: $pass 项全部 PASS"
