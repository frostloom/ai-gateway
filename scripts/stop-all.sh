#!/usr/bin/env bash
# 停掉 ai-gateway 全部本地进程（billing/router/mock×3/gateway/reconciler/agent）。
# 注意：Git Bash 的 kill 杀不掉 Windows exe，必须 taskkill。
set -uo pipefail
cd "$(dirname "$0")/.."

echo "==> 停掉服务进程"
for p in 9105 18080 9203 9202 9201 9102 9103 9101; do
  pid=$(netstat -ano 2>/dev/null | grep -E ":$p\s" | grep LISTENING | awk '{print $5}' | head -1)
  if [ -n "$pid" ]; then
    taskkill //F //PID "$pid" >/dev/null 2>&1 && echo "  ✅ :$p (pid $pid)"
  fi
done
for pid in $(tasklist 2>/dev/null | grep -i reconciler | awk '{print $2}'); do
  taskkill //F //PID "$pid" >/dev/null 2>&1 && echo "  ✅ reconciler (pid $pid)"
done

echo "==> 停掉 MySQL/Redis 容器（想保留数据就别加 -v）"
docker compose stop mysql redis 2>/dev/null && echo "  已 stop mysql/redis（数据保留在 volume）"

echo "完成。再启动：bash scripts/start-all.sh"
