# Naspic 私有云相册 — Docker 多架构打包

> 对应 README 场景6 ｜ 版本：v0.6.0 ｜ 代码：`deploy/`

---

## 1. Dockerfile 设计

```dockerfile
# 构建阶段：golang:1.22-bookworm + libvips-dev
#   关键：不写 --platform=$BUILDPLATFORM，让 buildx/QEMU 在目标架构下原生（模拟）编译
#   → cgo 直接走系统 pkg-config，彻底绕开 C 依赖交叉编译
FROM golang:1.22-bookworm AS builder
RUN apt-get install -y build-essential pkg-config libvips-dev
RUN CGO_ENABLED=1 go build -tags vips -o /out/naspic-server ./cmd/server

# 运行阶段：debian:bookworm-slim + libvips42（运行时库）
FROM debian:bookworm-slim
RUN apt-get install -y ca-certificates tzdata libvips42
ENTRYPOINT ["/usr/local/bin/naspic-server"]
```

| 选择 | 理由 |
|---|---|
| 构建基础镜像用 **bookworm**（Debian 12） | arm64 源里 `libvips-dev` 版本足够新（8.14+），省去自编译 |
| 运行镜像用 **slim** | 镜像体积约 120MB，ARM 设备拉取快 |
| 只装 `libvips42`（运行时）不装 `-dev` | 运行镜像不背编译工具链 |
| `CGO_ENABLED=1 -tags vips` | 启用 libvips；不加 tag 则自动降级纯 Go 后端 |

---

## 2. buildx 构建命令

### 2.1 首次准备（启用 QEMU 模拟 arm64）

```bash
docker run --rm --privileged multiarch/qemu-user-static --reset -p yes
docker buildx create --name naspic-builder --driver docker-container --use
docker buildx inspect naspic-builder --bootstrap
```

### 2.2 构建并推送双架构镜像

```bash
VERSION=$(cat VERSION)
docker buildx build \
  --builder naspic-builder \
  --platform linux/amd64,linux/arm64 \
  --build-arg VERSION=${VERSION} \
  -f deploy/Dockerfile \
  -t naspic/naspic:${VERSION} \
  -t naspic/naspic:latest \
  --push \
  .
```

或直接执行脚本：

```bash
chmod +x deploy/buildx.sh
./deploy/buildx.sh naspic/naspic:0.6.0 --push
```

### 2.3 仅构建单架构（本地调试）

```bash
docker buildx build --platform linux/arm64 -f deploy/Dockerfile -t naspic:arm64 --load .
```

### 2.4 校验

```bash
docker buildx imagetools inspect naspic/naspic:latest
# 应看到 linux/amd64 与 linux/arm64 两个 manifest
```

---

## 3. 容器启动示例（挂载本地照片目录）

### 3.1 docker run（**只读挂载**）

```bash
docker run -d \
  --name naspic \
  --restart unless-stopped \
  -p 8080:8080 \
  -e TZ=Asia/Shanghai \
  -v $(pwd)/data:/data \
  -v /photos:/photos:ro \
  naspic/naspic:latest
```

> `:ro` 是**硬性要求**：容器只读挂载，即使代码有 bug 也写不动宿主机原图。

### 3.2 docker compose

见 `deploy/docker-compose.yml`，重点：

```yaml
volumes:
  - ./data:/data
  - /photos:/photos:ro        # 只读！左右路径保持一致
```

### 3.3 读写模式（**高危，仅在明确需要时使用**）

```bash
-v /photos:/photos:rw
```

并且必须同时在 Web 端把该挂载目录的 `mode` 设为「读写」、打开「允许删除」双开关，
否则服务端仍会拒绝任何删除操作。

---

## 4. CI 流水线思路

```
GitHub Actions / GitLab CI：

stage build:
  1. checkout
  2. docker/setup-qemu-action       ← 注册 arm64 模拟
  3. docker/setup-buildx-action
  4. docker/login-action            ← 登录镜像仓库
  5. docker/build-push-action
       platforms: linux/amd64,linux/arm64
       cache-from: type=gha          ← ← 关键：缓存 Go 构建与 apt 层
       cache-to:   type=gha,mode=max
       push: true
       tags: ${{ secrets.REGISTRY }}/naspic:${{ github.ref_name }}

stage test:
  - go vet ./...
  - go test ./...
  - 在 arm64 runner（或模拟环境）跑一次冒烟：启动容器 → health → 挂载只读目录扫描

stage release:
  - 生成 CHANGELOG
  - 打 Git Tag → 推送 latest
```

**加速要点**

| 手段 | 效果 |
|---|---|
| `cache-from/to: type=gha` | Go 依赖与编译缓存复用，二次构建从 15min → 3min |
| 自建 arm64 runner（如树莓派/Oracle ARM 实例） | 比 QEMU 模拟快 5~10 倍 |
| 分离 `go mod download` 层 | 依赖不变时跳过下载 |

---

## 5. ARM 平台镜像构建常见坑

| # | 坑 | 现象 | 解决 |
|---|---|---|---|
| 1 | **在 amd64 上交叉编译 cgo** | `cannot find -lvips`、`wrong ELF class` | 用 buildx + QEMU 在目标架构下构建，**不要** `GOARCH=arm64` + 宿主机 gcc |
| 2 | **QEMU 未注册** | `exec format error` | `docker run --privileged multiarch/qemu-user-static --reset -p yes` |
| 3 | **arm64 构建极慢** | 首次 20~40 分钟 | 用 GHA 缓存；或改用原生 arm64 runner |
| 4 | **运行时缺 libvips.so** | `libvips.so.42: cannot open shared object` | 运行镜像必须装 `libvips42`（不是 `-dev`） |
| 5 | **apt 源架构不匹配** | `no matching manifest` | buildx 会自动切到 arm64 基础镜像，不要在 Dockerfile 里硬编码 amd64 包 |
| 6 | **内存不足被 OOM kill** | QEMU 编译 libvips 时崩 | 增大 Docker 内存上限（建议 ≥4GB），或减少 `-j` 并发 |
| 7 | **镜像体积过大** | arm64 设备拉取慢 | 运行阶段用 slim + 只装运行时库 + `-ldflags "-s -w"` |
| 8 | **时间不同步** | 照片时间轴错乱 | 挂载 `/etc/localtime` 或设 `TZ` 环境变量 |
| 9 | **只读卷上 SQLite 报错** | `attempt to write a readonly database` | SQLite 库文件必须在 `/data`（读写卷），**不能**放在 `/photos` |
| 10 | **inotify 在网络盘不生效** | 新照片不出现 | 自动降级轮询；NFS/CIFS 场景建议直接设 `watch_mode=轮询` |

---

## 6. 后端接收移动端分片上传

容器已暴露 `8080`，APP 侧接口（均需 `Authorization: Bearer <token>`）：

| 步骤 | 接口 | 说明 |
|---|---|---|
| 1 | `POST /api/v1/upload/check` | 传 `library_id + hash + size`，命中则秒传 |
| 2 | `POST /api/v1/upload/init` | 返回 `session_id / chunk_size / chunk_total` |
| 3 | `PUT /api/v1/upload/chunk?session_id=&index=` | 请求体为分片原始字节，支持乱序与断点续传 |
| 4 | `POST /api/v1/upload/complete` | 服务端合并 + SHA-256 校验 + 入库 |

**反向代理注意**（若用 Nginx）：

```nginx
client_max_body_size 0;          # 分片大小由 APP 决定，不要在网关限制整体大小
proxy_read_timeout 300s;
proxy_request_buffering off;     # 避免大分片被先落盘，ARM 设备 IO 扛不住
```
