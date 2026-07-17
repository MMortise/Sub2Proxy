#!/usr/bin/env bash
# Install (or upgrade) sub2proxy on a Linux server from a prebuilt GitHub Release
# binary — no compiling on the server. Run as root.
#
# Usage: sudo deploy/install.sh [vX.Y.Z]
#   no argument  -> installs the latest release; pass a tag to pin a version.
#   PREFIX=/path -> install location (default /opt/sub2proxy). Example:
#                   PREFIX=/home/sub2proxy sudo -E deploy/install.sh
set -euo pipefail

REPO="MMortise/Sub2Proxy"
PREFIX="${PREFIX:-/opt/sub2proxy}"
SERVICE_USER="sub2proxy"
VERSION="${1:-}"

# No version given: resolve the latest release tag from the GitHub API (no gh/jq
# needed). Pass a tag explicitly to pin/downgrade.
if [ -z "$VERSION" ]; then
  echo "==> Resolving latest release tag"
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name" *: *"([^"]+)".*/\1/')"
  [ -n "$VERSION" ] || { echo "could not resolve latest release tag" >&2; exit 1; }
fi
echo "==> Target: ${VERSION}  ->  ${PREFIX}"

case "$(uname -m)" in
  x86_64)          arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;;
esac
asset="sub2proxy-linux-${arch}"
url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"

echo "==> Ensuring service user + directories"
id "$SERVICE_USER" >/dev/null 2>&1 || useradd -r -s /usr/sbin/nologin "$SERVICE_USER"
mkdir -p "$PREFIX/data"

# Stop first so a running instance can't flush its in-memory config back over the
# file after we touch it (the app persists config.yaml on shutdown).
systemctl stop sub2proxy 2>/dev/null || true

echo "==> Downloading ${asset} (${VERSION})"
curl -fL --retry 3 -o "$PREFIX/sub2proxy.new" "$url"
chmod +x "$PREFIX/sub2proxy.new"
mv -f "$PREFIX/sub2proxy.new" "$PREFIX/sub2proxy"

# Seed config on first install with data_dir pointing at THIS install's data dir.
# The app's built-in default is /data, which won't exist here; leaving auth_key
# empty makes the app generate and persist one on first start (read it from the
# journal, see below). On upgrade the existing config is left untouched.
if [ ! -f "$PREFIX/data/config.yaml" ]; then
  cat > "$PREFIX/data/config.yaml" <<EOF
listen: 0.0.0.0:27000
auth_key: ""
port_range: [27001, 27999]
data_dir: ${PREFIX}/data
subscriptions: []
manual_nodes: []
mappings: []
EOF
  chmod 600 "$PREFIX/data/config.yaml"
fi
chown -R "$SERVICE_USER:$SERVICE_USER" "$PREFIX"

echo "==> Installing systemd unit"
# Generated with this install's paths. Kept intentionally minimal: filesystem
# hardening like ProtectSystem/ProtectHome tends to make the data dir read-only
# unless every path lines up, which is a common footgun — NoNewPrivileges is the
# safe, portable bit to keep.
cat > /etc/systemd/system/sub2proxy.service <<EOF
[Unit]
Description=sub2proxy - subscription to multi-port proxy
Documentation=https://github.com/${REPO}
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
WorkingDirectory=${PREFIX}
ExecStart=${PREFIX}/sub2proxy -config ${PREFIX}/data/config.yaml
Restart=on-failure
RestartSec=3
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable sub2proxy >/dev/null 2>&1 || true
systemctl restart sub2proxy

echo "==> Status"
sleep 1
systemctl --no-pager --lines=0 status sub2proxy | head -5
echo
echo "==> First run generates ${PREFIX}/data/config.yaml with a random auth_key:"
echo "    journalctl -u sub2proxy | grep auth_key"
echo "==> Web UI: http://<本机IP>:27000"
