#!/usr/bin/env bash
# Build the sub2proxy image, (re)create the container, and restart it.
# Usage: scripts/build.sh
set -euo pipefail

# Resolve repo root (this script lives in scripts/).
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

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
    exit 0
  fi
  sleep 1
done

echo "!! Health check did not pass in time. Recent logs:" >&2
docker compose logs --tail 30 >&2
exit 1
