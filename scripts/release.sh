#!/usr/bin/env bash
# Build the self-contained binary (embedded web UI) for Linux and publish it as a
# GitHub Release asset. Run locally where the Go + pnpm toolchain lives; the
# server then just downloads the binary — no compiling on the server.
#
# Usage: scripts/release.sh vX.Y.Z
set -euo pipefail

VERSION="${1:?usage: scripts/release.sh vX.Y.Z (e.g. v0.1.0)}"
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

echo "==> Building web UI (embedded into the binary)"
( cd web/frontend && pnpm install --frozen-lockfile && pnpm build )

OUT_DIR="dist-release"
rm -rf "$OUT_DIR"; mkdir -p "$OUT_DIR"

# CGO is disabled, so cross-compiling to Linux is a plain `go build` — the result
# is a static binary that runs anywhere with no libc dependency.
build() {
  goos="$1"; goarch="$2"
  name="sub2proxy-${goos}-${goarch}"
  echo "==> Building ${name}"
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o "${OUT_DIR}/${name}" ./cmd/sub2proxy
}

build linux amd64
build linux arm64

echo "==> Publishing GitHub Release ${VERSION}"
gh release create "${VERSION}" "${OUT_DIR}"/* \
  --title "${VERSION}" \
  --notes "sub2proxy ${VERSION} — 预编译二进制（已内嵌 Web UI）。Linux amd64 / arm64。

服务器部署（无需编译）：
    sudo deploy/install.sh            # 装最新版；或 deploy/install.sh ${VERSION} 指定版本
或手动下载对应架构的 sub2proxy-linux-<amd64|arm64> 直接运行。"

echo "==> Done: https://github.com/$(gh repo view --json nameWithOwner -q .nameWithOwner)/releases/tag/${VERSION}"
