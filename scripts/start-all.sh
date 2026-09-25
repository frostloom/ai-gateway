#!/usr/bin/env bash
# ai-gateway 一键启动（Windows / Git Bash）。
#
#   bash scripts/start-all.sh              # 启动（幂等：自动杀掉已在跑的再拉起）
#   bash scripts/start-all.sh --rebuild    # 先 go build 再启动
#   bash scripts/start-all.sh --seed       # 启动后造演示数据（4 租户 + 5000 条账单）
#
# 自动做：docker 起 MySQL/Redis/Kafka(+建topic) → 载入 .env → 起 billing/router/mock×3/
#         gateway/reconciler/event-consumer/agent → 等端口就绪 → 打印状态。
# agent 需要 .env 里的 AGENT_LLM_API_KEY，否则 /portal/chat 会返回明确错误。
set -uo pipefail
cd "$(dirname "$0")/.."
mkdir -p logs bin

REBUILD=0; SEED=0
for a in "$@"; do
  case "$a" in
    --rebuild) REBUILD=1 ;;
    --seed) SEED=1 ;;
    *) echo "未知参数: $a（支持 --rebuild / --seed）"; exit 1 ;;
  esac
done

# 0) 载入 .env（用户填的 AGENT_LLM_API_KEY 等）
if [ -f .env ]; then
  echo "==> 载入 .env"
  set -a; source .env; set +a
fi

# 1) 基础设施：MySQL :3307 / Redis :6381 / Kafka :9092（首次会经 initdb.d 自动应用 schema.sql；
#    kafka-init 一次性幂等建 billing.events + billing.events.dlq）
if ! docker ps --format '{{.Names}}' 2>/dev/null | grep -q '^ai-gateway-mysql$'; then
  echo "==> docker compose up -d mysql redis kafka kafka-init"
  docker compose up -d mysql redis kafka kafka-init
else
  echo "==> MySQL/Redis 容器已在跑（Kafka 若未起则补起）"
  docker compose up -d kafka kafka-init
fi
echo -n "==> 等 MySQL 就绪"
for i in $(seq 1 30); do
  if docker exec ai-gateway-mysql mysqladmin ping -uroot -proot --silent >/dev/null 2>&1; then
    echo " ok"; break
  fi
  [ "$i" = 30 ] && { echo " 超时：MySQL 起不来"; exit 1; }
  sleep 2
done
# 兜底：若 providers 表为空（volume 重建过），补应用 schema.sql 种子
if [ "$(docker exec ai-gateway-mysql mysql -uroot -proot -N -e "USE ai_gateway; SELECT COUNT(*) FROM providers;" 2>/dev/null)" = "0" ]; then
  echo "==> providers 为空，补应用 scripts/schema.sql 种子数据"
  docker exec -i ai-gateway-mysql mysql -uroot -proot ai_gateway < scripts/schema.sql
fi

# 2) 构建（可选）
if [ "$REBUILD" = "1" ]; then
  echo "==> go build -o bin/ ./cmd/..."
  go build -o bin/ ./cmd/...
fi

# 3) 杀掉已在跑的旧进程（Git Bash 的 kill 杀不掉 Windows exe，必须 taskkill）
kill_port() {
  local pid
  pid=$(netstat -ano 2>/dev/null | grep -E ":$1\s" | grep LISTENING | awk '{print $5}' | head -1)
  [ -n "$pid" ] && taskkill //F //PID "$pid" >/dev/null 2>&1
}
echo "==> 清理已在跑的旧服务"
for p in 9101 9103 9102 9201 9202 9203 18080 9105 9106 9107; do kill_port "$p"; done
for pid in $(tasklist 2>/dev/null | grep -i reconciler | awk '{print $2}'); do
  taskkill //F //PID "$pid" >/dev/null 2>&1 || true
done
sleep 1

# 4) 依次启动（billing/router 先于 gateway；agent 最后）
echo "==> 启动服务（日志在 logs/*.log）"
./bin/billing.exe      > logs/billing.log     2>&1 &
./bin/router.exe       > logs/router.log      2>&1 &
MOCK_PROVIDER_PORT=9201 MOCK_PROVIDER_NAME=mock-a ./bin/mock-provider.exe > logs/mock-a.log 2>&1 &
MOCK_PROVIDER_PORT=9202 MOCK_PROVIDER_NAME=mock-b ./bin/mock-provider.exe > logs/mock-b.log 2>&1 &
MOCK_PROVIDER_PORT=9203 MOCK_PROVIDER_NAME=mock-c ./bin/mock-provider.exe > logs/mock-c.log 2>&1 &
GATEWAY_PORT=${GATEWAY_PORT:-18080} ./bin/gateway.exe > logs/gateway.log 2>&1 &
./bin/reconciler.exe -interval 30 -grace 600 > logs/reconciler.log 2>&1 &
./bin/event-consumer.exe > logs/event-consumer.log 2>&1 &
AGENT_PORT=${AGENT_PORT:-9105} ./bin/agent.exe > logs/agent.log 2>&1 &

# 5) 等端口就绪并打印状态
wait_port() { # $1=port $2=名称
  for i in $(seq 1 20); do
    if netstat -ano 2>/dev/null | grep -E ":$1\s" | grep -q LISTENING; then
      echo "  ✅ $2 :$1"
      return 0
    fi
    sleep 1
  done
  echo "  ❌ $2 :$1 未就绪（看 logs/ 下对应日志）"
  return 1
}
echo "==> 服务状态"
wait_port 9101 "billing(gRPC)";   wait_port 9103 "billing(admin)"
wait_port 9102 "router";          wait_port 9201 "mock-a"
wait_port 9202 "mock-b";          wait_port 9203 "mock-c"
wait_port 18080 "gateway";        wait_port 9105 "agent"
wait_port 9106 "reconciler(http)"; wait_port 9107 "event-consumer(http)"

# 6) 演示数据（可选）
if [ "$SEED" = "1" ]; then
  echo "==> go run ./scripts/seed -n 5000 -reset（清掉旧 seed-* 再造演示账单 + M7 套餐/订阅）"
  go run ./scripts/seed -n 5000 -reset | tail -2
fi

echo
echo "完成。打开："
echo "  管理面板  http://localhost:${GATEWAY_PORT:-18080}/        （X-Admin-Token: ${ADMIN_TOKEN:-admin-demo}，无 AI 客服）"
echo "  用户自助页 http://localhost:${GATEWAY_PORT:-18080}/portal  （key: sk-demo-8f3a2b1c9d4e5f60，含 AI 客服）"
[ "$(grep -c '^AGENT_LLM_API_KEY=.\+' .env 2>/dev/null)" = "0" ] && \
  echo "  提示：.env 里 AGENT_LLM_API_KEY 还没填，AI 客服返回明确错误；填后重新 bash scripts/start-all.sh 即可。"
