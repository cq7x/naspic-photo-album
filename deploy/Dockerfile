# ============================================================
# Naspic 私有云相册 —— 服务端一体化构建
#
# 构建上下文必须是【项目根目录】（因为要同时读取 server/ 与 web/）：
#   docker build -f deploy/Dockerfile -t naspic:1.0.1 .
#
# 常用构建参数：
#   --build-arg TAGS=vips   启用 libvips 缩略图后端（默认，缩略图快）
#   --build-arg TAGS=       留空则用纯 Go 后端（无需 libvips，体积小、更稳）
#   --build-arg VERSION=1.0.1
# ============================================================

ARG GO_VERSION=1.22
ARG NODE_VERSION=20

# ---------- 阶段 1：构建 Web 前端 ----------
FROM node:${NODE_VERSION}-bookworm-slim AS web

# 国内服务器可传 --build-arg NPM_REGISTRY=https://registry.npmmirror.com 提速
ARG NPM_REGISTRY=""
RUN if [ -n "${NPM_REGISTRY}" ]; then npm config set registry "${NPM_REGISTRY}"; fi

WORKDIR /web
COPY web/package.json ./
RUN npm install --no-audit --no-fund
COPY web/ ./
RUN npm run build

# ---------- 阶段 2：构建 Go 后端 ----------
FROM golang:${GO_VERSION}-bookworm AS builder

ARG VERSION=dev
ARG TAGS=vips

ENV DEBIAN_FRONTEND=noninteractive \
    GOPROXY=https://goproxy.cn,direct \
    CGO_ENABLED=1

# 国内服务器可传 --build-arg APT_MIRROR=mirrors.aliyun.com 避免 deb.debian.org 龟速
ARG APT_MIRROR=""
RUN if [ -n "${APT_MIRROR}" ]; then \
        for f in /etc/apt/sources.list /etc/apt/sources.list.d/debian.sources; do \
            [ -f "$f" ] && sed -i "s|deb.debian.org|${APT_MIRROR}|g" "$f"; \
        done; \
    fi

# libvips-dev 提供 vips 头文件与 pkg-config；build-essential 供 cgo 使用
RUN apt-get update && apt-get install -y --no-install-recommends \
        build-essential pkg-config ca-certificates \
        libvips-dev \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /src

# 仓库未提交 go.sum，这里在线生成（构建机需能访问 GOPROXY）
# 用 go mod download 而非 tidy：不解析测试依赖，更快也更不容易因第三方
# test-only 依赖（testify 等）解析失败而中断整个构建
COPY server/ ./
RUN go mod download

# CGO_ENABLED=1 + -tags vips：启用 libvips 缩略图后端
# -mod=mod 允许构建时自动补齐 go.sum 中缺失的条目
RUN GOFLAGS=-mod=mod CGO_ENABLED=1 go build -tags "${TAGS}" \
        -ldflags "-s -w -X github.com/naspic/naspic/internal/api.Version=${VERSION}" \
        -o /out/naspic-server ./cmd/server

# ---------- 阶段 3：运行镜像 ----------
FROM debian:bookworm-slim

ARG APT_MIRROR=""
RUN if [ -n "${APT_MIRROR}" ]; then \
        for f in /etc/apt/sources.list /etc/apt/sources.list.d/debian.sources; do \
            [ -f "$f" ] && sed -i "s|deb.debian.org|${APT_MIRROR}|g" "$f"; \
        done; \
    fi

ENV DEBIAN_FRONTEND=noninteractive \
    TZ=Asia/Shanghai \
    NASPIC_DATA_DIR=/data \
    NASPIC_WEB_ROOT=/web

RUN apt-get update && apt-get install -y --no-install-recommends \
        ca-certificates tzdata curl \
        libvips42 \
        ffmpeg \
    && rm -rf /var/lib/apt/lists/* \
    && mkdir -p /data /photos /web

COPY --from=builder /out/naspic-server /usr/local/bin/naspic-server
COPY --from=web /web/dist /web

VOLUME ["/data", "/photos"]
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=20s --retries=3 \
    CMD curl -fsS http://127.0.0.1:8080/api/v1/health || exit 1

ENTRYPOINT ["/usr/local/bin/naspic-server"]
CMD ["-c", "/data/config.yaml"]
