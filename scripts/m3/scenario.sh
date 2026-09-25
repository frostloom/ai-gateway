#!/usr/bin/env bash
# M3 场景验证：SSE 流式全量结算按实际（非预占）；kill 客户端 → 账单保持 pending（M4 对账再冲正）。
# 前置：docker compose up -d mysql redis；billing/router/mock-provider×2/gateway 已启动。
set -euo pipefail
cd "$(dirname "$0")/../.."

GATEWAY="${GATEWAY_URL:-http://127.0.0.1:18080}"
KEY="sk-demo-8f3a2b1c9d4e5f60"
MYSQL="docker exec ai-gateway-mysql mysql -uroot -proot -N"

pass=0
check() { if [ "$2" = "ok" ]; then echo "PASS  $1"; pass=$((pass+1)); else echo "FAIL  $1"; exit 1; fi; }

# ---- 1. 流式完整跑完：有增量 + usage 帧 + [DONE]，结算按实际 ----
frames=$(python - "$GATEWAY" "$KEY" <<'PY'
import json,sys,urllib.request
base,key=sys.argv[1],sys.argv[2]
body=json.dumps({'model':'mock-model','stream':True,'messages':[{'role':'user','content':'hello streaming'}]}).encode()
req=urllib.request.Request(base+'/v1/chat/completions',data=body,headers={'Authorization':'Bearer '+key,'Content-Type':'application/json'})
resp=urllib.request.urlopen(req)
deltas=0; done=0; usage=None
for raw in resp:
    line=raw.decode('utf-8','replace').strip()
    if not line.startswith('data:'): continue
    payload=line[5:].strip()
    if payload=='[DONE]': done=1; continue
    d=json.loads(payload)
    if d.get('usage'): usage=d['usage']
    elif d.get('choices') and d['choices'][0].get('delta',{}).get('content'): deltas+=1
print(f"deltas={deltas} done={done} usage={usage}")
PY
)
echo "  stream -> $frames"
check "流式有增量帧" "$(echo "$frames" | grep -q 'deltas=[1-9]' && echo ok)"
check "收到 [DONE]" "$(echo "$frames" | grep -q 'done=1' && echo ok)"
check "usage 帧带 completion>0" "$(echo "$frames" | grep -q "completion_tokens': [1-9]" && echo ok)"

# 断言该请求已按实际结算：最新 settle 行 actual = prompt+completion（非 pre）
actual_row=$($MYSQL -e "USE ai_gateway; SELECT actual_quota,delta_quota,prompt_tokens,completion_tokens FROM bills WHERE phase='settle' ORDER BY id DESC LIMIT 1;" 2>/dev/null)
echo "  latest settle row: $actual_row"
act=$(echo "$actual_row" | awk '{print $1}')
pro=$(echo "$actual_row" | awk '{print $3}')
com=$(echo "$actual_row" | awk '{print $4}')
check "结算按实际（actual=prompt+completion）" "$([ $((pro+com)) -eq "$act" ] && echo ok)"

# ---- 2. kill 客户端 → 账单保持 pending（结算没发生，等 M4 对账冲正） ----
# 先看当前最新 reserve 行的 request_id/状态，读 2 帧后掐断连接，再查新 pending
before_rid=$($MYSQL -e "USE ai_gateway; SELECT COALESCE(MAX(id),0) FROM bills;" 2>/dev/null)
kill_info=$(python - "$GATEWAY" "$KEY" <<'PY'
import json,sys,http.client
base,key=sys.argv[1],sys.argv[2]
host,port=base.replace('http://','').split(':')
conn=http.client.HTTPConnection(host,int(port))
body=json.dumps({'model':'mock-model','stream':True,'messages':[{'role':'user','content':'hello'}]}).encode()
conn.request('POST','/v1/chat/completions',body,{'Authorization':'Bearer '+key,'Content-Type':'application/json'})
resp=conn.getresponse()
for i in range(2):
    line=resp.readline().decode('utf-8','replace').strip()
    if not line: continue
conn.close()  # 暴力掐断
print('killed after 2 frames')
PY
)
sleep 1  # 等网关处理
new_rid=$($MYSQL -e "USE ai_gateway; SELECT MAX(id) FROM bills;" 2>/dev/null)
status=$($MYSQL -e "USE ai_gateway; SELECT status FROM bills WHERE id=$new_rid AND phase='reserve';" 2>/dev/null)
echo "  killed -> $kill_info ; new bill id=$new_rid status=$status (before id=$before_rid)"
check "掐断后 reserve 行保持 pending" "$([ "$status" = "pending" ] && echo ok)"

echo
echo "M3 scenario: $pass 项全部 PASS"
