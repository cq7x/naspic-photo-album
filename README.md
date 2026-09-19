# Naspic 私有云相册

自建的私有云相册 / 照片视频库，对标飞牛相册、群晖 Photos。
**原始照片永远在你自己的磁盘上**——挂载目录默认只读索引，程序不拷、不改、不删原图。

- 后端：Go 1.22 + Gin + GORM（MySQL / SQLite 双支持）
- 前端：Vue 3 + Element Plus + Vite
- 移动端：Uni-app（Android / iOS），手机相册自动备份、指定文件夹独立同步
- 缩略图：libvips（低配设备可切纯 Go 后端）
- 部署：Docker Compose，一条命令起三个容器（应用 + MySQL + Redis）

当前版本：**v1.8.0**

---

## 核心特性

| 能力 | 说明 |
| --- | --- |
| **挂载目录（只读）** | 指向服务器上已有照片目录，只建索引不复制原图；inotify 实时监听，NFS/CIFS 自动降级为轮询 |
| **托管库（可写）** | 网页上传、手机备份写入托管目录，按 `年/月/` 归档，同名自动重命名绝不覆盖，相同内容秒传 |
| **时间轴浏览** | 按拍摄时间分组；图片读 EXIF，**视频读容器里的 `creation_time`**（不是文件修改时间，复制导出过也准） |
| **后台上传任务** | 网页选完文件先列清单，点「开始上传」转后台；弹窗可关、可切页面，右下角面板看总进度/速度/逐文件状态，支持取消与重试 |
| **Redis 缓存** | 媒体列表、统计、相册列表走缓存，60s TTL；Redis 挂了自动降级，不影响使用 |
| **相册** | 按主题/时间/关键词规则归类，规则可一键重算 |
| **手机端** | 相册自动增量备份；可单独勾选手机内任意文件夹，各自配置仅 WiFi / 原图 / 递归；断点续传、局域网优先 |
| **低配友好** | ARM64（树莓派、RK3588、ARM NAS）支持，可关 AI、降并发、缩资源 |

---

## 目录结构

```
naspic/
├─ server/          Go 后端（cmd/server 入口，internal/ 分层）
├─ web/             Vue3 Web 前端
├─ app/             Uni-app 移动端
├─ deploy/          Docker 部署相关
│  ├─ Dockerfile            一体化构建（前端 + 后端 → 单镜像）
│  ├─ docker-compose.yml    应用 + MySQL + Redis
│  ├─ build.sh              编译镜像
│  ├─ fresh-deploy.sh       一键全新部署（裸机 → 跑起来）
│  ├─ prepare.sh            生成 .env + 建数据目录
│  ├─ cleanup.sh            构建前磁盘清理
│  └─ .env.example          配置模板（复制成 .env 后改）
├─ docs/            设计与部署文档（部署教程见 12）
└─ VERSION
```

---

## 快速开始

### 全新部署（推荐，一条命令）

在一台装好 Docker 的 Linux 服务器上：

```bash
# 1) 拿代码
cd /home/ubuntu && git clone https://github.com/cq7x/naspic-photo-album.git naspic

# 2) 一条命令：建目录 → 生成 .env → 编译镜像 → 启动 → 健康检查
sudo bash /home/ubuntu/naspic/deploy/fresh-deploy.sh --mirror
```

- `--mirror`：apt + npm 走国内源，**国内服务器务必加**，否则可能卡几十分钟
- `--install-docker`：连 Docker 一起装
- `--no-build`：镜像已存在，只重启
- `--data-dir` / `--photos` / `--code-dir`：自定义路径

完成后访问 `http://<服务器IP>:8080`，默认账号 `admin` / `naspic123`（**登录后立即改密码**）。

### 手工分步

```bash
cd naspic/deploy
cp .env.example .env && bash prepare.sh      # 生成配置、建数据目录
export NPM_REGISTRY=https://registry.npmmirror.com
export APT_MIRROR=mirrors.aliyun.com
chmod +x *.sh && ./build.sh                  # 编译镜像（约 5~15 分钟）
docker compose up -d
docker compose ps                            # 三个容器都要 healthy
```

完整教程（目录规划、Nginx 反代 HTTPS、手机 APP 接入、备份、故障排查）
见 **[docs/12-服务器Docker编译与部署教程.md](docs/12-服务器Docker编译与部署教程.md)**。

---

## ⚠️ 重要：托管库的目录必须在持久卷里

新建「托管库」时那个**存储路径**输入框：

- **留空最安全** —— 自动落在数据卷内（`/data/managed/<库名>`），容器怎么重建数据都在
- **填了容器外的路径（如 `/mypic`）而 compose 没挂这个卷，会丢原图**：
  文件写进容器可写层，`docker compose up -d` 重建后全部消失，
  只剩缩略图缓存能显示（看着"照片都在"，点开大图 404）

程序已内置防护：自定义路径若目录不存在会直接拒绝并提示。
已有照片建议一律用**挂载目录**（只读索引，原图不动）。

---

## 几个设计取舍

- **挂载目录默认只读**：绝不修改宿主机原图；删除默认只删索引，物理删除要单独开高危开关
- **视频时间取容器 `creation_time`**：文件 mtime 在复制/导出/网盘下载后全乱，拿它排序必然错
- **Redis 是可选加速项**：连不上自动降级为不缓存，绝不因为缓存拖垮相册
- **上传并发 3**：再高会把上行带宽打满，影响同时浏览

---

## 文档索引

| 文档 | 内容 |
| --- | --- |
| [01-需求与方案设计](docs/01-需求与方案设计.md) | 整体方案、模块划分 |
| [02-数据库设计](docs/02-数据库设计.md) | 表结构与索引 |
| [03-存储驱动与上传接口设计](docs/03-存储驱动与上传接口设计.md) | StorageDriver 抽象、两种驱动 |
| [04-移动端同步设计](docs/04-移动端同步设计.md) | 手机多文件夹同步、断点续传 |
| [05-缩略图模块设计](docs/05-缩略图模块设计.md) | libvips、懒生成、缓存 |
| [06-Docker多架构打包](docs/06-Docker多架构打包.md) | amd64 + arm64 buildx |
| [09-Web存储管理页面](docs/09-Web存储管理页面.md) | 存储管理页 |
| [11-部署手册](docs/11-部署手册.md) | 面向家庭 NAS 用户 |
| **[12-服务器Docker编译与部署教程](docs/12-服务器Docker编译与部署教程.md)** | **部署主教程** |
| [15-MySQL部署与迁移](docs/15-MySQL部署与迁移.md) | SQLite → MySQL |
| [00-AI提示词文档](docs/00-AI提示词文档.md) | 早期驱动 AI 开发的提示词集合（历史归档） |

版本历史见 [CHANGELOG.md](CHANGELOG.md)。
