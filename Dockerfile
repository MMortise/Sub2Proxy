# syntax=docker/dockerfile:1

# --- Stage 1: build the web UI ---
FROM node:22-alpine AS frontend
WORKDIR /app/web/frontend
RUN corepack enable
# Disable pnpm's minimum-release-age supply-chain delay: deps are pinned and
# lockfile-verified, but some were published recently and would otherwise be
# rejected in a clean container build.
ENV npm_config_minimum_release_age=0
# Install deps first for layer caching. packageManager in package.json pins the
# exact pnpm version via corepack.
COPY web/frontend/package.json web/frontend/pnpm-lock.yaml* web/frontend/.npmrc* ./
RUN pnpm install --frozen-lockfile
COPY web/frontend/ ./
# Vite is configured to emit to ../dist -> /app/web/dist
RUN pnpm build

# --- Stage 2: compile the Go binary (frontend embedded) ---
FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Replace the placeholder dist with the freshly built UI before embedding.
COPY --from=frontend /app/web/dist ./web/dist
ARG VERSION=docker
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" -o /sub2proxy ./cmd/sub2proxy

# --- Stage 3: minimal runtime ---
FROM alpine:3.20
# busybox already provides wget for the healthcheck; su-exec lets the entrypoint
# drop to the non-root user after fixing the /data mount ownership.
RUN apk add --no-cache ca-certificates tzdata su-exec \
    && adduser -D -u 10001 s2p \
    && mkdir -p /data && chown s2p:s2p /data
COPY --from=build /sub2proxy /usr/local/bin/sub2proxy
COPY scripts/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
RUN chmod +x /usr/local/bin/docker-entrypoint.sh
VOLUME /data
# Web UI (27000) + proxy mapping ports (27001-27999).
EXPOSE 27000-27999
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://127.0.0.1:27000/api/health >/dev/null 2>&1 || exit 1
# Start as root only to chown the bind mount; the entrypoint su-exec's to s2p.
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
CMD ["/usr/local/bin/sub2proxy", "-config", "/data/config.yaml"]
