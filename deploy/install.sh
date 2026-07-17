#!/usr/bin/env bash
# Install (or upgrade) sub2proxy on a Linux server from a prebuilt GitHub Release
# binary — no compiling on the server. Run as root from a repo checkout (it uses
# deploy/sub2proxy.service next to this script).
#
# Usage: sudo deploy/install.sh [vX.Y.Z]
#   no argument -> installs the latest release; pass a tag to pin a version.
set -euo pipefail

REPO="MMortise/Sub2Proxy"
PREFIX="/opt/sub2proxy"
VERSION="${1:-}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# No version given: resolve the latest release tag from the GitHub API (no gh/jq
# needed). Pass a tag explicitly to pin/downgrade.
if [ -z "$VERSION" ]; then
  echo "==> Resolving latest release tag"
  VERSION="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep -m1 '"tag_name"' | sed -E 's/.*"tag_name" *: *"([^"]+)".*/\1/')"
  [ -n "$VERSION" ] || { echo "could not resolve latest release tag" >&2; exit 1; }
fi
echo "==> Target version: ${VERSION}"

case "$(uname -m)" in
  x86_64)          arch=amd64 ;;
  aarch64 | arm64) arch=arm64 ;;
  *) echo "unsupported arch: $(uname -m)" >&2; exit 1 ;;
esac
asset="sub2proxy-linux-${arch}"
url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"

echo "==> Ensuring service user + directories"
id sub2proxy >/dev/null 2>&1 || useradd -r -s /usr/sbin/nologin -d "$PREFIX" sub2proxy
mkdir -p "$PREFIX/data"

echo "==> Downloading ${asset} (${VERSION})"
curl -fL --retry 3 -o "$PREFIX/sub2proxy.new" "$url"
chmod +x "$PREFIX/sub2proxy.new"
mv -f "$PREFIX/sub2proxy.new" "$PREFIX/sub2proxy"
chown -R sub2proxy:sub2proxy "$PREFIX"

echo "==> Installing systemd unit"
install -m 0644 "$SCRIPT_DIR/sub2proxy.service" /etc/systemd/system/sub2proxy.service
systemctl daemon-reload
systemctl enable --now sub2proxy
systemctl restart sub2proxy   # pick up a new binary on upgrade

echo "==> Status"
sleep 1
systemctl --no-pager --lines=0 status sub2proxy | head -5
echo "==> On first run the app writes ${PREFIX}/data/config.yaml with a random auth_key:"
echo "    journalctl -u sub2proxy | grep auth_key"
echo "==> Web UI: http://<本机IP>:27000"
