#!/bin/sh
# Fix bind-mount ownership, then drop to the non-root runtime user.
#
# The image creates /data owned by s2p (uid 10001), but a host bind mount
# (./data:/data) shadows that with the host directory's ownership — typically
# root — so the non-root process can't write its config.yaml and crash-loops on
# first run. Running chown here (as root) makes the mount writable regardless of
# host ownership, then su-exec drops privileges so the app never runs as root.
set -e
if [ "$(id -u)" = "0" ]; then
  chown -R s2p:s2p /data 2>/dev/null || true
  exec su-exec s2p "$@"
fi
exec "$@"
