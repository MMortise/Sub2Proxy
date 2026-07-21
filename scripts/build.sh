#!/usr/bin/env bash
# Build the sub2proxy image, (re)create the container, and restart it.
# Usage: scripts/build.sh
set -euo pipefail

# Resolve repo root (this script lives in scripts/).
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

# Optionally pull latest code first. Defaults to NO, so an empty answer or an
# unattended run (no TTY) never changes the working tree unexpectedly.
reply=""
read -rp "执行前是否先 git pull 拉取最新代码？[y/N] " reply || reply=""
case "$reply" in
  [yY] | [yY][eE][sS])
    echo "==> git pull"
    git pull
    ;;
  *)
    echo "==> 跳过 git pull（使用当前代码）"
    ;;
esac

echo "==> Building image and starting container (docker compose up -d --build)"
docker compose up -d --build

echo "==> Restarting container to pick up config/data changes"
docker compose restart

echo "==> Container status"
docker compose ps

echo "==> Waiting for health endpoint"
for i in $(seq 1 20); do
  if curl -fsS --max-time 3 http://127.0.0.1:27000/api/health >/dev/null 2>&1; then
    echo "==> Healthy: $(curl -fsS http://127.0.0.1:27000/api/health)"
    lan_ip="$(hostname -I 2>/dev/null | awk '{print $1}')"
    echo "==> 本机访问: http://127.0.0.1:27000"
    [ -n "$lan_ip" ] && echo "==> 局域网访问: http://${lan_ip}:27000  (Web UI；代理端口 27001-27020)"
    exit 0
  fi
  sleep 1
done

echo "!! Health check did not pass in time. Recent logs:" >&2
docker compose logs --tail 30 >&2
exit 1
