# Naspic 服务端 Docker 编译与部署教程

> 适用：一台已安装最新版 Docker 的 Linux 服务器（x86_64 或 ARM64 均可）
> 目标：在服务器上从源码编译出镜像 → 启动容器 → 挂载已有照片目录 → 手机 APP 接入
> 版本：v1.2.0

---

## 目录

- [0. 三十秒速查版（一键脚本 / 手工分步）](#0-三十秒速查版)
- [1. 部署架构与目录规划](#1-部署架构与目录规划)
- [2. 服务器准备](#2-服务器准备)
- [3. 把代码弄到服务器上](#3-把代码弄到服务器上)
- [4. 编译镜像](#4-编译镜像)
- [5. 准备配置与数据目录](#5-准备配置与数据目录)
- [6. 启动容器](#6-启动容器)
- [7. 初始化配置（首次访问）](#7-初始化配置首次访问)
- [8. Nginx 反向代理 + HTTPS](#8-nginx-反向代理--https)
- [9. 手机 APP 接入](#9-手机-app-接入)
- [10. 日常运维](#10-日常运维)
- [11. 故障排查](#11-故障排查)
- [12. 进阶：多架构镜像 / 离线构建 / MySQL](#12-进阶多架构镜像--离线构建--mysql)
- [13. 磁盘清理（构建前必看）](#13-磁盘清理构建前必看)

---

## 0. 三十秒速查版

> v1.8.0 起正式环境是三容器：应用 + MySQL + Redis（见 `deploy/docker-compose.yml`）。

给熟悉 Docker 的人看的极简版，细节看后面章节。

### 0.1 一键脚本（推荐，全新部署走这条）

```bash
# 1) 装 Docker（已装可跳过）
curl -fsSL https://get.docker.com | sh
sudo systemctl enable --now docker

# 2) 把代码弄到服务器上（git 克隆，或从本地打包上传，见第 3 章）
cd /home/ubuntu && git clone <你的仓库地址> naspic

# 3) 一条命令搞定：建目录 → 生成 .env → 编译镜像 → 启动 → 健康检查
sudo bash /home/ubuntu/naspic/deploy/fresh-deploy.sh --mirror
#   国内服务器务必加 --mirror（apt + npm 走国内源，否则可能卡几十分钟）
#   连 Docker 都要一起装就加 --install-docker
#   镜像已编译过，只想重启就加 --no-build
#   已配好加速器、不想再体检镜像源就加 --no-mirror
#   拉镜像报 dial tcp / i/o timeout 时，先跑：sudo bash deploy/docker-mirror.sh

# 4) 浏览器打开 http://<服务器IP>:8080  默认账号 admin / naspic123
```

脚本会启动三个容器（见 `deploy/docker-compose.yml`）：

| 容器 | 作用 | 数据落在哪 |
| --- | --- | --- |
| `naspic` | 应用（Go 后端 + 内置前端） | `${NASPIC_DATA_DIR}` → `/data` |
| `naspic-mysql` | 业务数据库 | `${MYSQL_DATA_DIR}` → `/var/lib/mysql` |
| `naspic-redis` | 列表/统计缓存（可选但推荐） | 纯内存，不落盘 |

Redis 挂了也没关系：服务端会自动降级成"不缓存"，只是慢一点，不会拖垮相册。
想关掉就把 `docker-compose.yml` 里 `NASPIC_REDIS_ENABLED` 改成 `"false"`。

### 0.2 手工分步（想自己掌控每一步）

```bash
# 1) 装 Docker（已装可跳过）
curl -fsSL https://get.docker.com | sh
systemctl enable --now docker

# 2) 拿代码
cd /home/ubuntu && git clone <你的仓库地址> naspic && cd naspic

# 3) 生成配置 + 建数据目录
cd deploy && cp .env.example .env && bash prepare.sh

# 4) 编译镜像（约 5~15 分钟）
export NPM_REGISTRY=https://registry.npmmirror.com   # 国内服务器建议
export APT_MIRROR=mirrors.aliyun.com                 # 国内服务器建议
chmod +x *.sh && ./build.sh

# 5) 启动（三个容器一起起）
docker compose up -d
docker compose ps          # 三个都要是 healthy

# 6) 浏览器打开 http://<服务器IP>:8080  默认账号 admin / naspic123
```

> 老版本文档里的 `docker run` 单容器 + SQLite 方式仍然能跑（适合临时试玩），
> 但**正式用请一律走 compose**：MySQL 存业务数据、Redis 加速、数据目录与代码目录分离。

---

## 1. 部署架构与目录规划

```
┌──────────────── Linux 服务器 ────────────────┐
│                                              │
│   /opt/naspic/            ← 项目代码 + compose│
│   /opt/naspic/data/       ← 数据库/缓存/配置  │
│   /photos/                ← 你的原图（只读）  │
│                                              │
│   ┌──────────── Docker ────────────┐         │
│   │  naspic 容器                    │         │
│   │  ├─ /usr/local/bin/naspic-server│         │
│   │  ├─ /web        前端静态文件     │         │
│   │  ├─ /data  ←→  /opt/naspic/data │         │
│   │  └─ /photos ←→  /photos (:ro)   │         │
│   │        监听 0.0.0.0:8080         │         │
│   └─────────────────────────────────┘         │
└──────────────────────────────────────────────┘
         ↑ 手机 APP / 浏览器
```

**两条铁律（务必遵守）：**

1. **原图目录一律 `:ro` 只读挂载**。容器内程序对挂载库永远只读，删除操作需要「读写模式 + 显式开关」双条件，只读挂载下物理删除直接被拒绝。
2. **照片目录左右路径保持一致**。例如宿主机 `/photos` 就挂到容器 `/photos`。这样在 Web 端填挂载路径时不用做路径换算，不容易搞错。

**端口占用：**
- `8080` — Web 管理与 API（手机同步也走这个端口）

---

## 2. 服务器准备

### 2.1 安装 Docker

```bash
# 一键安装（官方脚本，自动识别发行版）
curl -fsSL https://get.docker.com | sh

# 启动并设置开机自启
systemctl enable --now docker

# 验证
docker version
docker compose version
```

Ubuntu / Debian 也可以用系统源，速度更快（国内镜像源）：

```bash
sudo apt-get update
sudo apt-get install -y docker.io docker-compose-v2
sudo usermod -aG docker $USER     # 退出重登录生效
sudo systemctl enable --now docker
```

> ⚠️ **国内服务器必须先配镜像加速器，否则 `docker pull` 会卡死。**
> 现象是：`docker pull` 报 `dial tcp [2a03:2880:...]:443: i/o timeout`（走 IPv6 连 Docker Hub 超时）。
>
> 创建 `/etc/docker/daemon.json`：
> ```json
> {
>   "registry-mirrors": [
>     "https://docker.m.daocloud.io",
>     "https://hub-mirror.c.163.com"
>   ]
> }
> ```
> 然后 `sudo systemctl restart docker`，用 `docker pull alpine` 验证（正常应 10~20 秒）。

### 2.2 让普通用户免 sudo 用 docker（可选）

```bash
usermod -aG docker $USER
# 退出重登录生效
```

### 2.3 开放防火墙端口

```bash
# firewalld（CentOS / Rocky / Alma）
firewall-cmd --permanent --add-port=8080/tcp && firewall-cmd --reload

# ufw（Ubuntu / Debian）
ufw allow 8080/tcp && ufw reload

# 云服务器还要去控制台的安全组放行 8080
```

> **安全提示**：如果服务器有公网 IP，强烈建议按第 8 章配 Nginx + HTTPS，并只暴露 443，不要直接把 8080 暴露到公网。

### 2.4 检查硬件资源

```bash
free -h      # 内存：≤1GB 会自动降为 low 档，1~4GB 为 mid，>4GB 为 high
nproc        # CPU 核数
df -h /opt   # 剩余空间，缩略图缓存会占用额外空间
```

程序启动时会按内存自动判定档位并调整并发，也可以用 `NASPIC_TIER=low` 环境变量强制降档。

### 2.5 镜像源体检与加速器（拉不动镜像先看这里）

国内服务器最常踩的坑：前面都顺利，一到 `docker build` / `docker compose up` 就卡住，最后报：

```
error pulling image configuration: download failed after attempts=6:
dial tcp [2a03:2880:...]:443: connect: connection timed out
```

报错里出现 `[IPv6 地址]` 基本就是两种原因之一：**走了不可达的 IPv6 路由**，或者**官方源整个不通**。
项目自带了体检脚本，一条命令自动判断并修好：

```bash
sudo bash deploy/docker-mirror.sh          # 自动体检 + 自动修
sudo bash deploy/docker-mirror.sh --check  # 只体检，不改任何配置
sudo bash deploy/docker-mirror.sh --mirror https://你的加速器  # 手工指定
sudo bash deploy/docker-mirror.sh --reset  # 恢复官方源
```

脚本的判断顺序：

1. 试拉 `hello-world`，能通 → 官方源正常，**不动任何配置**直接退出
2. 不通但 IPv4 能直连官方源 → 只是 IPv6 路由坏了，往 `/etc/hosts` 固定 IPv4（最小改动）
3. IPv4 也不通 → 逐个测速候选加速器，把可用的写进 `/etc/docker/daemon.json` 的 `registry-mirrors`
4. 重启 docker 后再拉一次验证；全部失败则给出**离线导入镜像**的兜底命令

手工配置（不想跑脚本时）：

```bash
sudo mkdir -p /etc/docker
sudo tee /etc/docker/daemon.json <<'EOF'
{
  "registry-mirrors": ["https://docker.m.daocloud.io", "https://docker.1ms.run"],
  "ipv6": false,
  "dns": ["223.5.5.5", "114.114.114.114"]
}
EOF
sudo systemctl restart docker
docker info | grep -A5 'Registry Mirrors'   # 确认已生效
```

> 加速器是第三方公益服务，随时可能变动。脚本内置了十来个候选地址逐个测速，
> 都不可用时按脚本最后打印的命令，在有网的机器上 `docker save` 打包镜像再 `docker load` 离线导入。

---

## 3. 把代码弄到服务器上

### 方式 A：Git 克隆（推荐）

```bash
cd /opt
git clone <你的仓库地址> naspic
cd naspic
```

### 方式 B：从本地打包上传

在本机（Windows）项目根目录执行：

```bash
# 排除掉已有的数据目录和依赖
tar --exclude='node_modules' --exclude='.git' --exclude='data' \
    -czf naspic-src.tar.gz .
scp naspic-src.tar.gz root@<服务器IP>:/opt/
```

服务器端：

```bash
mkdir -p /opt/naspic && cd /opt/naspic
tar -xzf /opt/naspic-src.tar.gz
```

### 方式 C：只有 SSH 通道（无 scp / 端口受限）

本机是 Windows、服务器只放通了 SSH 时，用 base64 管道直接塞过去：

```bash
# 本机（Git Bash）打包
tar czf /tmp/naspic.tar.gz --exclude=node_modules --exclude=.git --exclude=dist .
base64 -w0 /tmp/naspic.tar.gz > /tmp/naspic.b64

# 传过去（ssh 后面直接跟命令，读 stdin）
cat /tmp/naspic.b64 | ssh ubuntu@<服务器IP> "cat > /tmp/naspic.b64"

# 服务器端解开
ssh ubuntu@<服务器IP> "base64 -d /tmp/naspic.b64 > /tmp/naspic.tar.gz && \
  sudo rm -rf /home/ubuntu/naspic && mkdir -p /home/ubuntu/naspic && \
  tar xzf /tmp/naspic.tar.gz -C /home/ubuntu/naspic"
```

> **更新代码时务必先备份 `deploy/.env`** —— 上面的 `rm -rf` 会连它一起删掉。
> 数据目录（`NASPIC_DATA_DIR`、`MYSQL_DATA_DIR`）在代码目录之外，不受影响。
> 稳妥做法：
> ```bash
> cp /home/ubuntu/naspic/deploy/.env /tmp/naspic.env      # 删除前先备份
> # ……同步、解压……
> cp /tmp/naspic.env /home/ubuntu/naspic/deploy/.env      # 解压后恢复
> ```

### 检查目录结构是否正确

```bash
ls /opt/naspic
# 应当看到： deploy/  server/  web/  docs/  VERSION  README.md
ls /opt/naspic/server/go.mod   # 必须存在
ls /opt/naspic/web/package.json # 必须存在
```

---

## 4. 编译镜像

### 4.1 用一键脚本（推荐）

```bash
cd /opt/naspic
chmod +x deploy/*.sh

# 国内服务器务必设置 npm 镜像，否则前端依赖会卡很久
export NPM_REGISTRY=https://registry.npmmirror.com

./deploy/build.sh
```

脚本会读取 `VERSION` 文件作为版本号，产出两个标签：`naspic:<版本>` 和 `naspic:latest`。

自定义镜像名和版本：

```bash
VERSION=1.0.1 ./deploy/build.sh myrepo/naspic:1.0.1
```

### 4.2 手动 docker build

```bash
cd /opt/naspic
docker build \
  --build-arg VERSION=$(cat VERSION) \
  --build-arg TAGS=vips \
  --build-arg NPM_REGISTRY=https://registry.npmmirror.com \
  -f deploy/Dockerfile \
  -t naspic:latest \
  .
```

> ⚠️ **构建上下文必须是项目根目录**（命令最后的那个 `.`）。
> 因为 Dockerfile 要同时读取 `server/` 和 `web/`，写成 `server` 或 `deploy` 都会报 `COPY failed`。

### 4.3 构建参数说明

| 参数 | 默认值 | 说明 |
|---|---|---|
| `VERSION` | `dev` | 写入二进制的版本号，健康检查接口会返回 |
| `TAGS` | `vips` | `vips`=启用 libvips 缩略图（快，需编译 C 依赖）；置空=纯 Go 后端 |
| `GO_VERSION` | `1.22` | Go 编译镜像版本 |
| `NODE_VERSION` | `20` | Node 前端构建版本 |
| `NPM_REGISTRY` | 空 | 国内填 `https://registry.npmmirror.com` |
| `APT_MIRROR` | 空 | 国内填 `mirrors.aliyun.com`。不加的话容器内走 `deb.debian.org`，实测可能只有 **30 KB/s**，167MB 依赖要下一个多小时 |

**国内服务器一次到位的构建命令：**

```bash
export NPM_REGISTRY=https://registry.npmmirror.com
export APT_MIRROR=mirrors.aliyun.com
./deploy/build.sh
```

**libvips 编译失败时怎么办？**

某些发行版或 ARM 设备上 `libvips-dev` 与 `govips` 版本可能不匹配。此时改用纯 Go 缩略图后端，功能一样，只是生成速度慢些：

```bash
TAGS= ./deploy/build.sh
```

同时在配置文件里把 `thumb.backend` 改成 `go`（见 5.2）。

### 4.4 确认镜像构建成功

```bash
docker images naspic

# 看镜像里是否带了前端和二进制
docker run --rm naspic:latest ls /web
docker run --rm naspic:latest naspic-server --help 2>&1 | head -3
```

看到 `/web/index.html` 和 `/web/assets/` 说明前端已经打包进去了。

---

## 5. 准备配置与数据目录

> ### ⚠️ 先读这段：托管库的目录必须落在持久卷里
>
> 这是**最容易丢数据**的一个坑，趟过一次，代价是整库原图。
>
> 页面上「新建托管库」时有个**目录路径**（`storage_root`）输入框：
>
> - **留空最安全** —— 程序自动用 `<NASPIC_DATA_DIR>/managed/<库名>`，
>   也就是 `/data` 卷内部，**容器怎么重建数据都在**。
> - **填了容器外的路径（比如 `/mypic`）而 compose 里没挂这个卷，就会出事**：
>   文件被写进容器的可写层，`docker compose up -d` 重建容器后原图全部消失，
>   只剩缩略图缓存还能显示（看起来"照片都在"，一点开大图却 404）。
>
> 判断方法（在服务器上）：
>
> ```bash
> docker exec naspic ls -la /mypic          # 空目录 = 文件已丢
>
> # 看每个库登记的目录路径（type=1 托管库 / type=2 挂载库）
> docker exec naspic-mysql mysql -unaspic -p'你的密码' naspic \
>   -e "select id,name,type,storage_root from libraries"
> ```
>
> 已有库想改路径，直接改数据库里 `libraries.storage_root`，然后重启容器生效。
>
> 结论：**已有照片一律用「挂载目录」（只读索引，原图不动）；
> 只有手机备份、网页上传才需要托管库，而托管库的路径留空即可。**


### 5.1 创建数据目录

```bash
mkdir -p /opt/naspic/data
```

这个目录会存放：SQLite 数据库、缩略图缓存、分片上传临时文件、`config.yaml`。

### 5.2 放配置文件

```bash
cp /opt/naspic/server/config.example.yaml /opt/naspic/data/config.yaml
```

首次部署其实**不给配置文件也能跑**（程序会用内置默认值），但建议显式放一份，方便后续调整。

几个常见的改动点：

```yaml
server:
  data_dir: /data
  web_root: /web      # 镜像内置前端目录，不要改

database:
  driver: sqlite      # 低配/单机用 sqlite 就好
  dsn: /data/naspic.db

thumb:
  backend: vips       # 若构建时 TAGS 置空了，这里必须改成 go
  workers: 2          # 低配 ARM 改 1

scan:
  workers: 2          # 低配 ARM 改 1
  enable_watch: true  # inotify 监听；NFS/CIFS 网络盘上会自动降级为轮询
```

**注意**：配置文件里 `data_dir` 写的是**容器内路径** `/data`，不要写成宿主机的 `/opt/naspic/data`。

### 5.3 确认照片目录

假设你的照片在 `/photos`：

```bash
ls /photos
# 确认目录可读
sudo -u root test -r /photos && echo "可读"

# 如果容器以非 root 运行，需要确保 others 有读+执行权限
chmod -R a+rX /photos
```

**权限坑（很重要）**：容器内进程默认以 root 运行，一般没问题。但如果你用 `user:` 指定了非 root 用户，需要保证该用户对照片目录有 `r-x` 权限，否则扫描会全部报 `permission denied`。

---

## 6. 启动容器

### 方式 A：docker run（简单直接）

```bash
docker run -d \
  --name naspic \
  --restart unless-stopped \
  -p 8080:8080 \
  -e TZ=Asia/Shanghai \
  -e NASPIC_LOG_LEVEL=info \
  -v /opt/naspic/data:/data \
  -v /photos:/photos:ro \
  naspic:latest
```

参数说明：

| 参数 | 作用 |
|---|---|
| `-v /opt/naspic/data:/data` | 平台数据持久化（数据库、缩略图缓存、配置） |
| `-v /photos:/photos:ro` | **只读**挂载原图目录，务必带 `:ro` |
| `-e TZ=Asia/Shanghai` | 容器时区，影响时间轴展示 |
| `--restart unless-stopped` | 开机自启、异常退出自动拉起 |

需要索引多个目录就加多行 `-v`：

```bash
  -v /photos:/photos:ro \
  -v /mnt/usb/2024旅行:/mnt/usb/2024旅行:ro \
  -v /mnt/nas/家庭影像:/mnt/nas/家庭影像:ro
```

低配设备建议加资源限制：

```bash
  --memory=1g --cpus=1.5
```

### 方式 B：docker compose（推荐，好维护）

编辑 `deploy/docker-compose.yml`，把 `- /photos:/photos:ro` 改成你的实际路径：

```bash
cd /opt/naspic
vi deploy/docker-compose.yml
```

然后：

```bash
# 若第 4 步已经构建过镜像，直接启动
docker compose -f deploy/docker-compose.yml up -d

# 或者让 compose 顺带构建
docker compose -f deploy/docker-compose.yml up -d --build
```

### 6.1 检查是否启动成功

```bash
# 看状态（healthy 才算正常）
docker ps --filter name=naspic

# 看日志
docker logs -f naspic

# 手动探活
curl http://127.0.0.1:8080/api/v1/health
# 返回类似：{"ok":true,"version":"1.0.1","time":"..."}
```

正常日志长这样：

```
Naspic 启动中… 版本=1.0.1 档位=high 数据库=sqlite
提示：默认管理员 admin / naspic123，请首次登录后立即修改密码
[web] 前端已挂载 /web
HTTP 监听 0.0.0.0:8080
```

如果看到 `[web] 前端目录不可用`，说明 `/web` 没打包进去，检查第 4 步是否用了正确的构建上下文。

---

## 7. 初始化配置（首次访问）

### 7.1 登录

浏览器打开 `http://<服务器IP>:8080`，用默认账号登录：

- 账号：`admin`
- 密码：`naspic123`

**登录后第一件事：改密码。** 这个默认密码是公开的，暴露到公网等于裸奔。

### 7.2 添加挂载目录（索引已有照片）

1. 进入 **存储管理** 页面
2. 点 **添加挂载目录**
3. 填写：
   - **宿主机路径**：填**容器内**看到的路径，也就是你 `-v` 冒号右边的那个。例如挂载写的是 `-v /photos:/photos:ro`，这里就填 `/photos`
   - **模式**：保持**只读**（默认，强烈建议）
   - **递归扫描**：开
   - **忽略规则**：可填 `@eaDir,.DS_Store,#recycle,node_modules` 之类的目录
   - **包含扩展名**：留空=全部支持的类型，或填 `jpg,jpeg,png,heic,webp,mp4,mov`
4. 保存后点 **立即扫描**

> ⚠️ **最容易踩的坑**：这里填的是**容器内路径**，不是宿主机路径。
> 因为左右两侧挂载路径保持一致，所以大多数情况下两者恰好相同。
> 如果你挂载时写的是 `-v /photos:/media:ro`，那这里必须填 `/media`。

### 7.3 观察扫描进度

存储管理页面能看到扫描任务的状态、已处理文件数、失败文件列表。

首次全量扫描几万张照片在低配 ARM 上可能要跑几个小时，属于正常现象。扫描是限流+可退避的，不会把机器拖死。

命令行也能看：

```bash
docker logs -f naspic | grep -i scan
```

### 7.4 权限分配（多用户场景）

如果要给家人开独立账号：

1. **用户管理** 里创建用户
2. **存储管理 → 权限** 里把某个挂载目录授权给该用户或群组
3. 未授权的目录对该用户完全不可见

---

## 8. Nginx 反向代理 + HTTPS

有公网 IP 或域名的建议必做。

### 8.1 安装 Nginx

```bash
# Ubuntu / Debian
apt install -y nginx

# CentOS / Rocky
yum install -y nginx
```

### 8.2 配置反向代理

`vi /etc/nginx/conf.d/naspic.conf`：

```nginx
server {
    listen 80;
    server_name photo.example.com;   # 换成你的域名
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name photo.example.com;

    ssl_certificate     /etc/letsencrypt/live/photo.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/photo.example.com/privkey.pem;

    # 上传的照片可能很大，务必放开
    client_max_body_size 0;      # 0 = 不限制
    proxy_read_timeout  300s;
    proxy_send_timeout  300s;

    location / {
        proxy_pass http://127.0.0.1:8080;
        proxy_set_header Host              $host;
        proxy_set_header X-Real-IP         $remote_addr;
        proxy_set_header X-Forwarded-For   $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # 手机上传大图需要
        proxy_request_buffering off;
    }
}
```

```bash
nginx -t && systemctl reload nginx
```

### 8.3 申请免费证书

```bash
# 安装 certbot
apt install -y certbot python3-certbot-nginx   # Debian/Ubuntu
# yum install -y certbot python3-certbot-nginx # CentOS/Rocky

certbot --nginx -d photo.example.com
```

证书会自动续期。

### 8.4 只监听内网（更安全）

如果已经用 Nginx 反代，容器就不该再监听公网：

```bash
-p 127.0.0.1:8080:8080
```

这样外部只能通过 Nginx 的 443 访问。

---

## 9. 手机 APP 接入

APP 是 Uni-app 工程，需要先打包成 APK / IPA。打包前的服务端地址配置在 `app/utils/api.js` 里。

### 9.1 配置服务器地址

编辑 `app/utils/api.js`，把 baseURL 改成你的实际地址：

```js
// 局域网内
const BASE_URL = 'http://192.168.1.10:8080'

// 或有域名的外网
// const BASE_URL = 'https://photo.example.com'
```

### 9.2 打包

```bash
cd app
npm install
# 用 HBuilderX 或 CLI 打包成 APK
```

### 9.3 首次连接

1. 手机装好 APP，打开 **自动备份** 页面
2. 填服务器地址 → 用第 7.1 步的账号登录
3. 打开 **总开关**
4. 选择策略：
   - **系统相册**：开启后自动增量同步新拍的照片
   - **自定义文件夹**：可以单独给某个文件夹设置不同策略（比如工作截图只在 WiFi 下同步）
5. 建议先只开 **WiFi 下同步**，跑通再开流量同步

### 9.4 后台保活（否则备份会停）

- **Android**：设置 → 电池 → 应用管理 → 找到 APP → 设为「不受限制 / 允许后台运行」，并关闭电池优化。不同厂商位置不同（小米叫「自启动 + 神隐模式」，华为叫「启动管理」）。
- **iOS**：设置 → 通用 → 后台 App 刷新 → 打开。iOS 后台限制严格，只能在充电 + WiFi 时被动触发，属于系统限制。

---

## 10. 日常运维

### 10.1 常用命令

```bash
docker ps --filter name=naspic          # 状态
docker logs -f naspic                   # 实时日志
docker logs --tail 200 naspic           # 最近 200 行
docker restart naspic                   # 重启
docker stop naspic && docker start naspic
docker stats naspic                     # 资源占用
docker exec -it naspic sh               # 进容器
```

### 10.2 升级

```bash
cd /opt/naspic
git pull                                # 或重新上传源码

export NPM_REGISTRY=https://registry.npmmirror.com
./deploy/build.sh                       # 重新构建

docker compose -f deploy/docker-compose.yml up -d   # 用新镜像重建容器
# 或 docker rm -f naspic && docker run ...（用第 6 章的命令）
```

**数据不会丢** —— 数据库和缩略图缓存都在 `/opt/naspic/data`，不在镜像里。

> 缩略图缓存可以安全删除，程序会在下次访问时按需重新生成：
> `rm -rf /opt/naspic/data/cache/thumbs`

### 10.3 备份

**必须备份的：**

```bash
# SQLite 数据库（先停容器再拷，避免写一半）
docker stop naspic
cp /opt/naspic/data/naspic.db /backup/naspic-$(date +%F).db
docker start naspic
```

或者不停机用 sqlite 的备份命令：

```bash
docker exec naspic sh -c 'sqlite3 /data/naspic.db ".backup /data/backup.db"'
docker cp naspic:/data/backup.db /backup/naspic-$(date +%F).db
```

**不用备份的：**
- 缩略图缓存 `/opt/naspic/data/cache` — 可重建
- 临时文件 `/opt/naspic/data/tmp` — 可清理
- 原图 — 本来就是只读挂载，平台从不动它

**建议定期清理分片上传残留：**

```bash
docker exec naspic sh -c 'find /data/tmp -type f -mtime +7 -delete'
```

### 10.4 加个定时备份（crontab）

```bash
crontab -e
# 每天凌晨 3 点备份
0 3 * * * docker exec naspic sh -c 'sqlite3 /data/naspic.db ".backup /data/backup.db"' && cp /opt/naspic/data/backup.db /backup/naspic-$(date +\%F).db
```

---

## 11. 故障排查

| 现象 | 原因 | 处理 |
|---|---|---|
| `COPY failed: file not found` | 构建上下文不是项目根目录 | 确认在项目根目录执行，命令末尾是 `.`，且带 `-f deploy/Dockerfile` |
| `dial tcp [IPv6]:443: connection timed out` / `download failed after attempts=6` | 拉不到基础镜像：IPv6 路由不通或官方源被限 | `sudo bash deploy/docker-mirror.sh`（自动体检+配加速器），详见 [2.5](#25-镜像源体检与加速器拉不动镜像先看这里) |
| `go mod tidy` 卡住/超时 | 服务器访问不到 Go 代理 | `docker build --build-arg GOPROXY=https://goproxy.cn,direct ...`（Dockerfile 已内置，检查网络或换 `https://proxy.golang.org`） |
| `npm install` 极慢 | npm 官方源 | 加 `--build-arg NPM_REGISTRY=https://registry.npmmirror.com` |
| `pkg-config: vips not found` | libvips 版本不匹配 | 改用纯 Go 后端：`TAGS= ./deploy/build.sh`，配置里 `thumb.backend: go` |
| 容器一直 `unhealthy` | healthcheck 请求失败 | `docker logs naspic` 看是否启动异常；确认镜像内装了 curl |
| 扫描 0 个文件 | 路径填错或没权限 | ① 填的是**容器内**路径 ② `docker exec naspic ls /photos` 验证 ③ 检查宿主机目录权限 |
| `permission denied` 扫描报错 | 容器用户无权读照片目录 | `chmod -R a+rX /photos` |
| 页面 404 / 打不开 | 前端没打包进镜像 | `docker run --rm naspic:latest ls /web` 确认有 index.html |
| 能打开但接口 401 | 未登录或 token 过期 | 重新登录 |
| 手机传图失败 | 上传大小限制 / 超时 | Nginx 加 `client_max_body_size 0` 和 `proxy_request_buffering off` |
| 扫描时机器卡死 | 并发太高 | 配置里 `scan.workers: 1`、`rate_per_sec: 2`，或加 `NASPIC_TIER=low` |
| 网络盘（NFS/CIFS）不自动更新 | inotify 在网络盘不生效 | 程序会自动降级为轮询，调小 `scan.poll_interval` 即可 |
| 缩略图不显示 | 缓存目录不可写 / 格式不支持 | 检查 `/data/cache` 权限；RAW/HEIC 需要 libvips 后端 |
| 数据库锁死（SQLite） | 并发写入过高 | 换 MySQL，或降低 `scan.workers` |

**万能排查三板斧：**

```bash
docker logs --tail 100 naspic          # 1. 看日志
docker exec naspic ls -la /photos      # 2. 确认容器内能看到照片
curl -s http://127.0.0.1:8080/api/v1/health   # 3. 确认服务活着
```

---

## 12. 进阶

### 12.1 构建多架构镜像（amd64 + arm64）

需要在一台机器上同时产出两种架构的镜像（比如要分发给不同设备）：

```bash
cd /opt/naspic
chmod +x deploy/buildx.sh

# 创建支持 QEMU 模拟的 builder
docker run --rm --privileged multiarch/qemu-user-static --reset -p yes
docker buildx create --name naspic-builder --driver docker-container --use
docker buildx inspect naspic-builder --bootstrap

# 构建并推送到仓库
./deploy/buildx.sh <你的仓库>/naspic:1.0.1

# 或只导入本地（单架构，用于本机测试）
./deploy/buildx.sh naspic:1.0.1 --load
```

> arm64 走 QEMU 模拟编译 libvips 会非常慢（可能 30 分钟以上）。
> 建议直接在 ARM 设备上原生构建（即本教程的主流程），快得多。

### 12.2 完全离线的服务器

如果服务器不能访问外网，需要：

1. 在有网的机器上构建：`./deploy/build.sh naspic:1.0.1`
2. 导出：`docker save naspic:1.0.1 | gzip > naspic-1.0.1.tar.gz`
3. 拷贝到服务器：`scp naspic-1.0.1.tar.gz root@<IP>:/opt/`
4. 导入：`docker load -i /opt/naspic-1.0.1.tar.gz`
5. 直接按第 6 章启动

### 12.3 切换到 MySQL

单机和小规模场景 SQLite 完全够用（几万张照片没问题）。以下情况建议换 MySQL：
- 多用户并发访问
- 照片量超过 20 万
- 出现 SQLite 锁竞争

步骤：

1. compose 里取消 `mysql` 服务的注释，或者用已有的 MySQL
2. 建库：`CREATE DATABASE naspic CHARACTER SET utf8mb4;`
3. 配置环境变量：
   ```bash
   -e NASPIC_DB_DRIVER=mysql \
   -e NASPIC_DB_DSN="naspic:password@tcp(127.0.0.1:3306)/naspic?charset=utf8mb4&parseTime=True&loc=UTC"
   ```
4. 首次启动会自动建表（迁移脚本见 `server/migrations/001_init.mysql.sql`）

### 12.4 开启 AI 识别（人脸聚类 / 场景分类）

AI 默认关闭，纯本地离线推理（ONNX Runtime），不依赖任何云服务。

1. 构建时带 `ai` 标签：
   ```bash
   TAGS="vips,ai" ./deploy/build.sh
   ```
2. 把模型文件放到 `/opt/naspic/data/models/`
3. 启动加 `-e NASPIC_AI=1`，或在配置文件里 `ai.enabled: true`

> 低配 ARM 设备不建议开启，会很吃资源。中高档设备开启后，AI 任务走低优先级队列，空闲时才跑。

---

## 13. 磁盘清理（构建前必看）

**构建镜像很吃磁盘**：一次完整构建（golang 基础镜像 1.2G + node 293M + libvips 依赖链 + go module 缓存）
轻松产生 10G 以上的层。反复迭代时这些层会堆在 containerd 里不释放。

> 真实案例：20G 系统盘连续构建 4 次后被撑到 **100%**，
> `go build` 直接报 `protocol error: received DATA after END_STREAM` —— 看起来像网络故障，实际是磁盘满了。

### 构建前先看一眼

```bash
df -h /
docker system df
```

### 三步清理（按影响从小到大，可逐步执行）

```bash
# 1) 删除已停止的容器（通常能释放 1G+）
docker container prune -f

# 2) 删除所有未被容器引用的镜像（会清掉 golang/node 基础镜像，下次构建需重新拉取）
docker image prune -a -f

# 3) 清空 BuildKit 构建缓存 —— 这一步释放最多（实测单次 10.6G）
docker builder prune -a -f
```

清理后确认：

```bash
df -h /
docker ps          # 确认 naspic 容器仍在运行
curl 127.0.0.1:8080/api/v1/health
```

> 注意：`prune -a` **不会**删除正在运行的容器及其镜像。
> 若空间实在紧张，系统盘建议 ≥ 40G，或把 `/var/lib/docker` 迁到大盘。

---

## 附：本次部署涉及的文件

| 文件 | 作用 |
|---|---|
| `deploy/Dockerfile` | 三段构建（前端 → 后端 → 运行镜像） |
| `deploy/build.sh` | 服务器本地一键构建脚本（含镜像源自检） |
| `deploy/docker-mirror.sh` | Docker 镜像源体检 + 加速器自动配置 |
| `deploy/fresh-deploy.sh` | 全新部署一键脚本（装 Docker→建目录→编译→启动→探活） |
| `deploy/buildx.sh` | 多架构构建 + 推送 |
| `deploy/docker-compose.yml` | Compose 编排（含只读挂载示例） |
| `server/config.example.yaml` | 配置文件模板，复制到 `/opt/naspic/data/config.yaml` |
| `server/migrations/` | 数据库初始化脚本（SQLite / MySQL） |
| `docs/11-部署手册.md` | 面向家庭 NAS 用户的 ARM 部署手册 |
