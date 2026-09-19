# Naspic 私有云相册 变更日志

版本规范：语义化版本 `MAJOR.MINOR.PATCH`，每个开发步骤递增 MINOR。

---

## v1.8.0 — Redis 缓存加速 · 视频按拍摄时间排序 · 上传转后台任务

### 新增

1. **Redis 缓存（Docker 内一并编排）**
   `deploy/docker-compose.yml` 增加 `redis:7-alpine` 服务（256MB 上限、LRU 淘汰、纯内存不落盘）。
   服务端封装了 `internal/cache`：媒体列表首页、媒体统计、相册库列表、相册列表接入缓存，
   TTL 默认 60 秒；上传、删除、批量操作、扫描完成、补采时间后按前缀失效。
   **Redis 挂了会自动降级成「不缓存」**，只是慢一点，不会拖垮相册（响应头 `X-Cache: HIT/MISS` 可验证）。

2. **视频按拍摄时间排序**
   新增 `internal/media/video.go`：用 `ffprobe` 读视频容器的 `creation_time`
   （iPhone/安卓/相机都会写这个），把它作为 `taken_at`（来源标记 4），同时补上时长与分辨率。
   之前视频一律用文件 mtime —— 复制、导出、网盘下载过的文件 mtime 全乱，排序自然不对。

3. **「补采视频拍摄时间」一键修复历史数据**
   存储管理 → 某个库的「扫描缓存」抽屉底部新增入口。
   后台任务只跑 ffprobe 读容器头，不重算哈希、不重建缩略图，比「忽略缓存重扫」快得多；
   前端每 2 秒轮询进度（`已处理 x/y · 更新 z 条`）。

4. **上传改为后台任务 + 右下角进度面板**
   - 选完文件**只列清单**（文件名、大小、缩略图、可单独移除），点「开始上传」才真正开始
   - 任务交给全局队列（`web/src/store/upload.js`），**弹窗可以立刻关掉、切页面也不中断**
   - 右下角常驻面板（新组件 `UploadDock.vue`）：总进度条、实时速度、逐文件进度/状态，
     支持最小化成胶囊、取消某一条、取消全部、重试失败项、清除已完成
   - 每成功一个就自动刷新照片列表（通过 `naspic:upload-progress` 事件），新照片随传随出现

5. **一键全新部署脚本 `deploy/fresh-deploy.sh`**
   在干净的 Linux 服务器上一条命令跑完：检查/安装 Docker → **镜像源体检/加速器配置** → 建数据目录
   → 生成 `.env`（MySQL 密码随机化）→ 编译镜像 → `compose up -d` → 轮询健康检查。
   支持 `--install-docker` / `--mirror`（国内源）/ `--no-build` / `--no-mirror` /
   `--registry-mirror <URL>` / `--code-dir` / `--data-dir` / `--photos`。

5.1 **镜像源体检与加速器脚本 `deploy/docker-mirror.sh`**
   专治 `docker build` / `compose up` 卡十几分钟后报
   `dial tcp [IPv6]:443: connection timed out`、`download failed after attempts=6`：
   ① 试拉 `hello-world`，能通就不动配置；② IPv4 可达但 IPv6 不通 → 给官方源域名固定 IPv4
   （写 `/etc/hosts`，最小改动）；③ IPv4 也不通 → 逐个测速十来个候选加速器，
   把可用的写进 `/etc/docker/daemon.json` 的 `registry-mirrors`，重启 docker 后复验；
   ④ 全不可用 → 打印离线 `docker save` / `docker load` 兜底命令。
   `build.sh` 与 `fresh-deploy.sh` 都会在编译前自动跑一遍自检。

6. **托管库路径防护（重要，防丢数据）**
   新建托管库时若自定义 `storage_root` **目录不存在，直接拒绝并提示原因**。
   之前 `NewLocalDriver` 会 `MkdirAll` 出一个容器内的空目录，文件被写进容器可写层，
   `docker compose up -d` 重建容器后原图全部消失，只剩缩略图缓存能显示（看着"照片都在"，点开大图 404）。
   前端建库表单也加了同样的提示：**路径留空最安全**，自动落在数据卷 `/data/managed/<库名>` 内。

### 修复

1. 视频时长（`duration_ms`）此前从未入库：扫描器只对图片解析元数据，视频被完全跳过，
   现在图片与视频都会走 `media.Parse`。
2. 上传接口返回的 `dedup`（秒传）标记取错了层级（`res.data.results` → `res.results`），
   导致"秒传"永远不显示。

### 变更

- 上传并发数从 5 降到 3：避免把上行带宽打满影响浏览。
- 清除了上传弹窗里已失效的进度/速度/失败重试逻辑（已全部下沉到任务面板）。

---

## v1.7.0 — 视频预览：弹窗内播放 + 右上角原生全屏

### 新增

1. **预览弹窗内显示封面帧，点画面就在弹窗里播**
   视频进来先显示 ffmpeg 抽的封面帧（`poster`），点画面才开始解码播放，
   再点暂停。**整个过程不触发浏览器全屏**——原生控件里的全屏按钮在弹窗内被
   `controlslist="nofullscreen"` 藏掉，避免"点一下画面就跳全屏"的割裂感。

2. **右上角「全屏播放」按钮（仅视频出现）**
   调用浏览器原生 Fullscreen API（`requestFullscreen`）对 `<video>` 元素本身全屏。
   全屏中按钮变成「退出全屏」，不用键盘也能退。
   iOS Safari 不支持元素全屏，自动改走 `webkitEnterFullscreen()` 原生播放器。

3. **全屏后用浏览器原生播放控件**
   进入全屏时放开 `controlslist`，音量/进度/播放速度/退出全屏全部交给浏览器原生控件；
   全屏时元素铺黑底、去掉圆角阴影，避免 letterbox 露出弹窗底色。

4. **全屏失败给的是人话**
   捕获 Promise reject 与 `fullscreenerror` 事件（有些浏览器只发事件不 reject），
   按错误类型分别提示：
   - `NotAllowedError` → 需要直接点按钮触发（不能脚本自动调），或检查站点全屏权限
   - `NotSupportedError` → 浏览器不支持，建议用系统播放器
   - `SecurityError` → 被嵌套页面拦截，建议新标签页打开

### 修复

1. **视频在预览弹窗里超出屏幕、播控条看不见（v1.7.0 引入，已修）**
   之前用 `width/height: 100%` 想让视频撑满播放区，实测 `height: 100%`
   在 `.lb-stage`（grid + `place-items: center`）里会被按 `auto` 处理——
   于是视频按「宽度铺满 × 固有比例」排，直接把画面和播控条一起顶出屏幕。
   改为 **JS 精确计算适配矩形**（`fitVideo`）：
   元数据到位后读 `videoWidth/videoHeight`，在可用区域内按长边最大化算出
   精确的宽高（px）写进内联样式，窗口缩放 / 信息面板开合 / 切换视频都会重算。
   好处：播控条紧贴画面底边，画面完整且绝不溢出。
   全屏时由 `.is-fs`（`!important`）接管铺满，压过 JS 算的内联尺寸。

2. **缩略图失败被永久拉黑（重要）**
   原来生成失败就把 `thumb_status` 写成 2，之后无论环境怎么变都不再重试。
   可失败原因里一大半是可恢复的：**目录忘了挂进容器、移动硬盘临时拔掉、
   ffmpeg 是后装的**。一旦拉黑，环境修好了照片也永远没有缩略图。
   改为「失败后冷却 5 分钟」（内存表，重启即清空）：
   既不会每次浏览都反复解码坏文件拖慢列表，又能自愈。
   同时历史遗留的 `thumb_status=2` 记录一律放行重来。

2. **ESC 分两段**：第一次 ESC 只退出全屏、弹窗留着（回到相册预览），第二次才关闭弹窗。
   注意多数浏览器在全屏时会自己吃掉 ESC，keydown 根本不派发到页面，
   所以同时用 `fullscreenchange` 同步状态，两种情况最终行为一致。
2. **关闭/切换前先退全屏并暂停**：否则元素被移除后全屏状态还挂着，浏览器会退回一个空白全屏；
   切换上下一条时上一条视频也不会在后台继续响。
3. **点画面必须 `preventDefault`**：浏览器自带"点画面切换播放/暂停"，
   如果不阻止，我们 `pause()` 之后浏览器又会 toggle 一次，表现就是"点了暂停没反应、还在播"。

---

## v1.6.0 — 相册取代收藏、后台设置与管理员账号、数据迁至 MySQL、视频播放体验

### 一、视频播放与预览

1. **悬停视频只显示中间的播放圆圈**
   之前视频上同时叠着右上角小角标、底部三个胶囊和中间圆圈，图标打架。
   现在悬停时底部胶囊与角标让位，只留中间一个圆形播放按钮。

2. **灯箱视频按长边最大化播放**
   之前 `.lb-media` 只给了 `max-width/max-height`，而 `<video>` 作为替换元素
   不会超过自身原始分辨率——640×360 的视频在 2K 屏上只有中间一小块。
   现在视频单独走 `width/height: 100% + object-fit: contain`，
   长边顶到浏览器边界且画面完整不裁切；顺带把缩放按钮对视频隐藏（视频不参与缩放），
   光标也不再显示成"带加号的放大镜"。

3. **预览密度选择器由竖条图标改为文字**（紧凑 / 标准 / 大图）。

### 二、相册取代收藏

「收藏」只有"是/否"一个维度，装不下"按时间区间把照片归到一起"这类需求。

1. **数据模型**：新增 `albums`（名称、备注、封面、规则、自动纳入、排序、条目数）
   与 `album_items`（相册 ↔ 照片，唯一约束防重复加入）。
2. **四种归类方式**：手动挑选 / 拍摄时间区间 / 文件名关键词 / 整个相册库。
   规则只是"怎么把照片挑出来"，匹配完仍落成实体关联行，
   所以之后手动增删都不会被规则覆盖掉。
3. **新接口**：
   `GET|POST /albums`、`PATCH|DELETE /albums/:id`、`GET /albums/:id/media`、
   `POST|DELETE /albums/:id/items`、`POST /albums/:id/apply`（按规则重新匹配）、
   `GET /media/:id/albums`；`GET /media` 新增 `album_id` 过滤。
4. **前端**：侧边栏「收藏」→「相册」；新增相册列表页（封面、条目数、重跑规则、编辑、删除）
   与相册内容页（复用时间轴网格，支持"移出本相册"）；
   照片上的「♥ 收藏」改为「相册」，多选条改为「加入相册 / 移出本相册」。
   加入时可以直接新建相册并顺手按时间区间归类。
5. **旧收藏不丢**：首次启动若库里存在 `favorite=1` 的照片，
   自动生成一个「我喜欢的」相册把她们迁进去。`/favorites` 老入口重定向到 `/albums`。

### 三、后台设置（存储管理迁入 + 管理员账号）

1. 新增「设置」页，两个一级入口合并进去：
   - **存储管理**：原来的存储库表格、扫描、缓存、权限原样迁入
   - **账号设置**：账号列表、新增账号、改昵称/角色/启用禁用、重置他人密码、删除账号，
     以及「修改我的密码」
   - **关于**：版本、数据库类型、服务地址
   `/storage` 保留重定向到 `/settings?tab=storage`。
2. **新接口**：`GET|POST /admin/users`、`PUT|DELETE /admin/users/:id`、
   `PUT /admin/users/:id/password`、`POST /auth/password`、`GET /auth/me`。
3. **防呆**：不能删除/禁用/降权当前登录账号；至少保留一个管理员，
   避免出现"系统里没有任何管理员"的死锁。所有账号接口都校验 role=1。

### 四、数据全部迁到 MySQL

1. `docker-compose.yml` 默认启用 `mysql:8.0` 服务（utf8mb4 + 健康检查），
   naspic 通过 `depends_on: service_healthy` 等库就绪再启动。
2. 提供 `deploy/mysql/init/01-init.sql`（建库 + 建业务账号 + 授权），
   与 `deploy/.env.example`（数据目录、MySQL 账号密码）。
   **数据卷必须用环境变量指到代码目录之外**，否则重新同步代码会把数据一起删掉。
3. **索引长度坑**：MySQL InnoDB 单列索引上限 3072 字节，utf8mb4 按 4 字节/字符算，
   `relative_path`/`rel_path` 原本是 `size:1024`，唯一索引会直接建不出来（error 1071）。
   两处统一降到 `size:512`（8 + 512×4 = 2056 < 3072）。
4. 切库后需要重新添加挂载目录并扫描一次（照片本身在宿主机上，不会丢）；
   缩略图缓存与扫描指纹会自动重建。

---

## v1.5.0 — 上传提速（即选即传 / 渐进入列）、视频封面帧

### 新增

1. **上传即选即传，不用再点一次「开始上传」**
   选完文件立刻进入上传队列并开始传输，省掉一次点击的等待。上传途中继续添加文件，
   工作池会自动接上新任务（并发 5，原为 3）。

2. **上传队列带本地预览图**
   队列每一项左侧显示缩略图：照片直接用本地 `objectURL`（零成本、同步出现）；
   视频在后台用 `canvas` 抽一帧回填。不用等服务器返回也能一眼看清选了哪些文件。

3. **边传边进列表（渐进刷新）**
   以前是「全部文件传完」才刷新一次列表，所以传几十张时感觉"传完半天才看到照片"。
   现在每成功一个就做一次去抖刷新（800ms），照片随传随出现。

4. **队列实时速度**
   上传对话框顶部显示「待上传 N 个 · 已完成 M 个 · 速度 xx/s」。

5. **视频封面帧（缩略图）**
   运行镜像内置 `ffmpeg`，视频缩略图改为抽第 1 秒的帧当封面（失败再退回第 0 秒），
   输出 JPEG 缓存。挂载目录里的视频同样生效。
   - 灯箱里的 `<video>` 现在带 `poster`，打开时先显示封面帧，点播放才开始解码
   - 装 ffmpeg 之后会自动把历史上「抽帧失败」的视频记录重新放行一次

6. **视频悬停显示播放图标**
   鼠标移到网格里的视频上，中间浮出圆形播放按钮；抽不到封面的视频显示占位块而不是破图。

### 修复

1. **灯箱里视频的光标是"带加号的放大镜"**
   `mediaStyle` 无条件给了 `zoom-in`，可视频根本不参与缩放。现在视频恒为 `default`。

2. **后台预生成缩略图的队列是空转的**
   `EnqueueAll` 只塞了 `mediaID` 和 `size`，没有 `srcPath` / `cacheAbs`，
   而 worker 开头就 `if t.srcPath == "" { continue }`——排进去的任务全被丢掉，
   一个缩略图都没预生成过。改为按驱动解析真实路径后再入队。

3. **上传后多了两次整文件读写**
   原来是「落临时文件 → 整文件读一遍算 SHA-256 → 再整文件读一遍写目标路径」。
   现在写临时文件的同时用 `TeeReader` 算哈希，一次读写搞定。
   另外上传成功后会后台预热缩略图（并发上限 2），前端刷新时直接命中缓存。

4. **视频缩略图失败后退回原图直链**
   `cellSrc` 出错时统一退回 `fileUrl`，而 `<img>` 放不了 mp4，只会得到一张破图。
   现在视频走占位块。

---

## v1.4.0 — 扫描缓存、删除源文件选项、侧边栏收缩、操作按钮图示

### 新增

1. **扫描指纹缓存（目录扫描提速）**
   新增 `scan_cache` 表，记录每个文件的 `大小 + 修改时间 + 哈希 + 尺寸 + 拍摄时间`。
   扫描时若指纹未变且索引里已有记录，就跳过该文件的哈希计算与元数据解析——
   这是扫描最大的一项开销。几万张照片的重复扫描可从分钟级降到秒级。
   - 缓存只影响速度，**任何时候清掉都安全**，下一轮扫描会自动重建
   - 全量扫描收尾会顺带清掉已失效的缓存行（文件被移走/改名后的残留），避免表无限膨胀
   - 缓存命中数会记入扫描任务的 `skipped` 字段，界面上能看到"省了多少事"

2. **缓存管理面板（存储管理 → 每个库 →「缓存」）**
   - 缩略图缓存：文件数 / 占用空间，可单独清理
   - 扫描指纹缓存：条目数，可单独清理
   - 最近一次扫描结果与缓存跳过数
   - 一键「增量扫描 / 忽略缓存重扫 / 全量重建（忽略缓存）/ 清空全部缓存」
   - 新增接口 `GET /libraries/:id/cache`、`POST /libraries/:id/cache/clear`
     （scope: `thumbs` | `scan` | `all`）；`POST /libraries/:id/scan` 新增 `force` 参数

3. **删除照片时可选择同时删除源文件**
   删除确认框新增「同时删除源文件（不可恢复）」勾选。
   **挂载目录一律不显示该选项**——平台对挂载目录只有索引，绝不碰宿主机原图，
   这类条目只移除索引并给出明确说明。批量删除会分别统计可删源文件的条数与挂载条目数。

4. **侧边栏收缩**
   顶栏最左侧新增折叠按钮（收起后侧边栏里的按钮会变窄不好找，顶栏这个永远可点），
   图标改为明确的左右箭头而非汉堡图标；收起态悬停导航项会浮出中文说明。

### 修复

1. **软删的索引永远扫不回来（"删错一张就永久丢失"）**
   scanner 的 upsert 用 `ON CONFLICT DO UPDATE`，而 `DoUpdates` 字段列表里没有 `deleted_at`。
   于是：某条记录一旦被软删，即使源文件还好好躺在磁盘上，后续任何扫描都只会更新它的其它字段，
   **它仍然处于删除态，从此从相册里彻底消失**——删错了没有任何补救手段。
   现改为批量入库前先把这批路径的 `deleted_at` 置空（文件确实存在，索引就该是活的）。
   实测：删掉 2 条后全量重扫，3 张照片全部恢复。
   副作用（符合预期）：只删索引而没删源文件时，重新扫描会"复活"——
   想彻底删除请勾「同时删除源文件」。

2. **扫描任务的"跳过数"没写进数据库**
   `finishJob` 的字段清单里漏了 `skipped`，导致界面上永远显示 0。已补上。

### 改进

- **照片浮层的三个操作按钮图示明确化**：由"纯图标圆钮"改为**图标 + 中文文字**的胶囊按钮
  （收藏 / 下载 / 详情），并加了悬停提示；收藏态改为实心红心。
  窄格子（< 230px）自动退化为纯图标，避免文字挤压。
- 收藏图标新增 `heartFill` 实心态（AppIcon 支持 `solid` 填充渲染）。

---

## v1.3.2 — 灯箱无法预览 & 照片浮层视觉

### 修复

1. **点开大图永远转圈、不显示图片（死锁）**
   灯箱的模板写成：
   ```html
   <div v-if="lb.loading" class="lb-spin">…</div>
   <img v-else :src="lgUrl" @load="lb.loading = false">
   ```
   `img` 挂在 `v-else` 上，而它前面 `v-if` 的条件恰恰就是"正在加载"。
   于是形成闭环：**加载中 → 不渲染 img → 图片永远不会开始加载 → `load` 事件永不触发 →
   永远停在加载中**。后端接口本身完全正常（`size=lg` 实测 146KB / 原图 3.4MB 均 200）。
   现改为媒体元素始终挂载，spinner 只作为叠在上层的覆盖层；视频同理。

2. **图片命中缓存时可能漏掉 load 事件**
   缓存命中的图片可能在 Vue 绑定监听之前就完成加载，导致 `loading` 停在 true。
   新增 `armLoading()`：开加载态的同时挂一个 10 秒兜底定时器，
   最坏情况也只是多转一会儿，不会永久卡住。关闭灯箱时清理定时器。

3. **灯箱 spinner 没有样式**
   `.lb-spin` 只有模板引用、CSS 里根本没定义，且会参与 `lb-stage` 的 grid 布局顶掉图片位置。
   现改为绝对定位覆盖层，并换成深色背景下可见的白色旋转环。

### 改进

- 照片悬停浮层的 3 个操作按钮（收藏/下载/详情）原本是**纯白实心圆**，
  压在照片上非常突兀（就是反馈里的"三个白圈圈"）。改为深色半透明玻璃底 + 白色图标 +
  细描边，悬停到单个按钮时才转为白底；同时补 `visibility` 切换，确保非悬停态彻底不显示。

---

## v1.3.1 — 挂载目录排障 & 目录变更自动入库

### 修复

1. **访问日志形同虚设（403 查不到来源的根本原因）**
   `loggerMiddleware()` 是个空壳——`c.Next()` 之后什么都没记，所有 HTTP 请求零日志，
   线上报 403 时完全无法定位是哪条规则拦的。现已实现真正的访问日志：
   记录 method / path / 状态码 / 耗时，4xx、5xx 额外带上 UA；
   静态资源与缩略图仅在异常时记录，避免刷屏。

2. **添加挂载目录失败会留下僵尸库**
   `createMountDir` 先把 `library`、`mount_dir` 写库，再注册驱动；
   驱动注册失败（典型：路径在容器内不可见）时直接返回 500，
   **已写入的库记录不回滚** → 留下 `driver_kind` 为空的僵尸库，扫描必失败、界面还删不掉。
   现已改为：
   - 建库**之前**先做容器视角校验：必须绝对路径、存在、是目录、可读；
   - 路径不可见时返回 400 + 可操作提示（提示用 `docker -v` 挂载并重启容器）；
   - 驱动注册失败时连同库记录一起回滚。

3. **新增照片不会自动进入相册（inotify 事件被丢弃 + 事件不投递）**
   - `WatchScheduler.StartOne` 的 `for range ch` 只是空转 drain 通道，
     源码注释写着"事件已在 runJob 中处理"，但实际没有任何处理逻辑。
   - 修好投递后实测发现第二层问题：**Docker bind mount（`-v /photos:/photos:ro`）
     下宿主机侧的文件创建事件并不会传进容器**，inotify watcher 建了但收不到任何事件。
   - 因此改为双保险：事件 → **3 秒防抖**（整批拷照片只跑一次）→ 提交增量扫描；
     **同时**按 `poll_interval_sec`（最小 60 秒）周期兜底提交增量扫描。
     `WatchScheduler` 新增 `mgr *Manager` 依赖以完成投递。
   - `storage` 层新增 `inotify 捕获到 N 个变更` 日志，便于判断 inotify 在当前环境是否真的可用。

4. **挂载监听 goroutine 会被误杀**
   `StartOne` 传入的是 gin 请求的 `Context`，请求结束即 cancel，监听立刻退出。
   改用 `context.Background()`。

5. **前端把后端中文错误吞掉**
   axios 响应拦截器直接 `reject(err)`，`err.message` 恒为
   `Request failed with status code 403` 这类英文。现已提取后端 `data.msg`，
   并按状态码兜底中文文案（401/403/404/409/413/5xx/超时/断网）。

6. **源文件删掉后索引不回收（幽灵照片）**
   全量重扫只会 upsert 扫到的文件，磁盘上已删除的文件不会产出任何事件，
   于是索引里永远留着打不开的条目（实测：删掉 5 个文件后重扫，8 条记录一条没少）。
   全量扫描新增收尾步骤 `purgeGone()`：遍历了整个目录，所以"没扫到"即"文件已不存在"，
   把这些条目软删。安全策略（宁可不删也不错删）：一个都没扫到 → 跳过；
   待回收数 > 本次扫到数 → 判定异常并告警跳过。

### 改进

- 添加挂载目录成功后**自动触发一次全量扫描**（之前只提示"开始扫描"但实际没跑），
  并自动弹出扫描任务面板让用户看到进度。
- 挂载对话框：补充"路径必须在容器内可见"的引导、展示当前已挂载路径作为参考、
  递归开关说明"不限深度"。

### 验证

- 递归扫描：构造 `2026-test/deep/level2/level3/` 四层目录，
  6 张图全部正确入库，尺寸采集正常（800×600）。**子目录递归索引工作正常。**

---

## v1.3.0 — UI 重构 & 上传修复

### 修复：网页上传失败

1. **根因**：`web/src/views/Gallery.vue` 的 `submitUpload()` 调用了 `uploadWeb()`，
   但 `<script setup>` 的 import 列表里**漏了 `uploadWeb`**，运行时抛出
   `ReferenceError: uploadWeb is not defined`，被 `try/catch` 吞掉后表现为
   「全部失败（N 个），请查看后端日志」，而后端日志其实干干净净。
   本次已把 `uploadWeb` 补进 import，并在上传失败时展示后端返回的真实 `msg`。
2. **上传体积限制**：Gin 默认 `MaxMultipartMemory` 只有 32 MiB，手机原图 / 4K
   视频容易触发落临时盘甚至失败。已在 `Engine()` 中放宽到 **512 MiB**。
3. **上传体验重做**：
   - 自建文件队列（拖拽 / 点选），不再依赖 `el-upload` 的内部状态
   - **3 并发**上传，逐文件实时进度条 + 成功 / 失败状态
   - 失败项保留在队列里可「重试」或「移除」，并显示真实原因
     （只读库 403 / 登录过期 401 / 文件过大 413 / 网络错误）
   - 上传完成后自动刷新时间轴与侧边栏统计

### 修复：照片尺寸永远是 0×0

12. **根因**：`model.MediaFile` 有 `width` / `height` 字段，但**全代码库没有任何地方写入过**
    （`grep` 只在 model 定义里出现），而 `media.Parse()` 也只解 EXIF、不取尺寸。
    结果是详情页显示「0 × 0」，新的两端对齐排版只能退化成固定 1.5 比例。
13. **`media.Parse()` 新增尺寸采集**：用标准库 `image.DecodeConfig`（只读取文件头，
    不解码整图，成本可忽略），支持 jpeg / png / gif；webp、heic 暂返回 0。
    即使文件没有 EXIF 也照常返回尺寸（原逻辑在无 EXIF 时提前 return，已修正）。
14. **三个写入口全部补上尺寸**：
    - `upload_web.go`（网页上传）
    - `upload.go`（手机分片上传）
    - `scanner.go`（目录扫描）——顺带补采 EXIF 拍摄时间，
      时间轴不再只能用 mtime（挂载目录 mtime 常常不准）
15. **冲突更新字段补充** `width` / `height` / `taken_at` / `taken_at_source`，
    重新全量扫描即可给历史数据回填尺寸（无需重传）。

### 修复：缩略图偶发返回 0 字节（前端破图）

16. **根因**：`thumb.Service.claim()` 的单飞控制只记了 key、没留完成信号，
    并发请求同一张缩略图时后来者直接拿到 `ErrThumbPending`；更糟的是
    `os.ReadFile` 命中「刚被 `os.Create` 还没写完」的空文件时也当成命中缓存，
    于是返回 **HTTP 200 + 0 字节**，前端 `<img>` 渲染成破图。
17. `busy` 改为 `map[string]chan struct{}`，`release()` 关闭 channel 唤醒等待方；
    新增 `waitPending()`：后来者最多等 15s 再重读缓存，而不是立刻报错。
18. 缓存命中与生成后读取都加上 `len(b) > 0` 校验，空结果一律按未命中处理。
19. 前端兜底：缩略图 `onerror` 时自动退回原图直链（`cellSrc()`），只回退一次防死循环。

### 全新 UI

4. **设计系统 `web/src/styles/theme.css`**：抽出品牌色 / 背景 / 文字 / 描边 /
   阴影 / 圆角一整套 CSS 变量，**浅色 + 深色双主题**（`html.dark`），
   主题跟随系统并可在顶栏一键切换，选择持久化到 localStorage。
5. **`web/src/styles/element.css`**：把 Element Plus 的 CSS 变量桥接到自有 token，
   重设对话框 / 表单 / 表格 / 下拉 / 按钮的圆角与配色，第三方组件与自绘组件视觉统一。
6. **应用外壳 `App.vue`**：顶部横向菜单改为**左侧可折叠侧边栏**
   （照片 / 收藏 / 存储管理），带品牌渐变标识、收录统计卡、用户区与退出；
   顶栏显示页面标题、版本号、主题开关；页面切换带淡入动画。
7. **图标组件 `components/AppIcon.vue`**：内置 30+ 个 feather 风格线性 SVG 图标，
   不引入额外依赖，颜色跟随 `currentColor`。
8. **照片墙 `Gallery.vue` 重做**：
   - **Google Photos 式两端对齐布局**（`justify()` 算法）：按原始宽高比排版，
     行高自适应，配合 `ResizeObserver` 实时重排；提供紧凑 / 标准 / 大图三档密度
   - 日期分组吸顶标题（今天 / 昨天 / 月日 / 年月日）
   - 顶部概览条：媒体数、照片数、视频数、占用空间
   - 卡片悬停浮层：收藏 / 下载 / 详情；多选模式带勾选框与批量操作条
   - 骨架屏、空状态引导（一键清空筛选 / 去上传）
   - 灯箱改为毛玻璃暗底 + 胶囊工具条 + 右侧信息抽屉，支持
     `← →` 翻页、`+ - 0` 缩放、`F` 收藏、`I` 信息、`Esc` 关闭
9. **登录页 `Login.vue`**：左右分栏，左侧渐变品牌区（标语 + 三个卖点），
   右侧卡片式表单，聚焦态高亮、密码可见切换、内联错误提示。
10. **新增「收藏」页面** `/favorites`：复用照片墙组件，默认按收藏筛选。
11. **存储管理页**：统一新视觉，分区图标 + 新按钮样式 + 滚动容器。

---

## v1.2.0 — 网页端上传（托管库）

### 后端

1. **新增 `POST /api/v1/upload/web`**（`internal/api/upload_web.go`）：
   multipart 多文件（`files` 字段 + `library_id`），单次最多 200 个。
   - **只接受可写库**：挂载只读库直接 403，绝不往宿主机目录写文件
   - **同名自动重命名**（`Overwrite: false`），绝不覆盖已有文件
   - **秒传**：同库已存在相同 SHA-256 时直接复用记录
   - 上传后立即用 `media.Parse` 抽 EXIF（拍摄时间、设备、GPS、方向）并写入索引，
     不必等下一轮扫描
   - 默认按 `年/月` 归档，可用 `dir` 字段指定子目录
   - 文件名清洗 `sanitizeName()`：去除路径分隔符与 Windows 非法字符，防穿越
2. **Content-Type 兜底**：`mimeByExt()` 按扩展名推断，
   修复历史数据 `mime` 为空导致原图响应无类型的问题。

### 前端

3. 相册页工具栏新增「**上传**」按钮与上传对话框：
   - 目标库下拉只列**可写**的托管库；没有托管库时给出引导提示
   - 拖拽 / 点击多选，`image/*,video/*`
   - 逐文件上传并显示百分比与 `第 n / N` 进度，完成自动刷新时间轴
   - 结果统计：成功 / 失败分别提示

4. **统计修复**：`mediaStats` 的「占用空间」此前恒为 0 ——
   gorm 单值聚合 `Select("COALESCE(SUM(x),0)").Scan(&int64)` 拿不到值，改用 `Row().Scan()`；
   同时每次重新构造查询，避免复用链式 `Statement` 造成条件叠加。
5. 编译期：`mediaStats` 内闭包 `q` 与 `*gorm.DB` 变量 `q` 重名（`no new variables on left side of :=`），改名 `lq`。

### 安全约束不变

- 挂载目录（只读）永远不能被上传写入；
- 上传冲突走重命名，删除对挂载库只删索引。

> **运维提醒**：反复构建会在 containerd 里堆积镜像层（本次 20G 系统盘被撑到 100%，
> 导致 `go build` 报 `protocol error: received DATA after END_STREAM`）。
> 构建前先 `docker system df`，空间紧张时：
> `docker container prune -f` → `docker image prune -a -f` → `docker builder prune -a -f`
> （本次 `builder prune -a` 单次释放 10.6G）。详见 `docs/12` 新增的「磁盘清理」一节。

---

## v1.1.0 — 照片浏览主页（时间轴 + 灯箱预览）

主页由「存储管理」改为「照片浏览」，相册从"能存"走向"能看"。

### 后端（internal/api/media.go）

1. **列表接口增强**：`GET /api/v1/media` 支持
   `library_id` / `media_type` / `favorite` / `keyword` / `start` / `end` / `order` 筛选，
   游标分页改为多取一条判断 `has_more`，并返回 `next_cursor`，前端可精确无限滚动。
   `start/end` 兼容 `2006-01-02` 与 RFC3339，`end` 自动含当天 23:59:59。
2. **新增 `GET /media/stats`**：总数 / 照片数 / 视频数 / 收藏数 / 占用空间 / 库列表。
3. **新增 `GET /media/:id/detail`**：基础信息 + EXIF（ExifJSON 解析为 map）+ 所属库名 + 标签。
4. **新增 `PATCH /media/:id`**：修改收藏标记、手工修正拍摄时间（`taken_at_source=3`）。
5. **新增 `POST /media/batch`**：批量收藏 / 取消收藏 / 移除索引，单次最多 2000 条。
6. **`GET /media/:id/file?download=1`** 增加 Content-Disposition，支持原图下载。
7. **`?token=` query 鉴权**：`<img src>` / `<video src>` 无法携带请求头，
   Auth Middleware 在 Authorization / X-Naspic-Token 之外回退读取 query token。
8. **`media_files` 新增 `favorite` 字段**（AutoMigrate 自动补列，无需手工迁移）。

### 前端

9. **新增 `views/Gallery.vue`** 作为主页：
   - 时间轴按天分组（今天 / 昨天 / 具体日期 + 星期），日期标题吸顶
   - 自适应网格、懒加载缩略图（sm/md/lg 三档）、视频角标
   - IntersectionObserver 无限滚动，接近末尾自动预加载
   - 灯箱预览：左右键切换、ESC 关闭、`+ - 0` 缩放、拖拽平移、
     收藏 / 下载原图 / 移除 / 信息面板（EXIF、路径、哈希、设备）
   - 多选批量：全选、收藏、取消收藏、移除索引
   - 工具栏：相册库筛选、类型筛选（全部/照片/视频/收藏）、日期范围、关键词搜索、统计条
10. **`App.vue`** 改为顶部导航（照片 / 存储管理）+ 版本 + 退出登录，全高布局。
11. **`router`** 增加登录守卫：无 token 一律回 `/login`，登录后回跳原地址。
12. **安全约束不变**：挂载库「移除」只删索引，宿主机原图不受影响。

---

## v1.0.2 — 真机部署验证与编译/运行时缺陷修复

在 Ubuntu 24.04（192.168.1.17，Docker 29.1.3）上完成**首次真机构建与部署**，
期间暴露并修复了 6 个此前无法发现的问题（本机无 Go/Docker，之前代码从未编译过）：

### 编译期修复（否则镜像根本构建不出来）

1. **`server/go.mod`：goexif 伪版本号不存在**
   `v0.0.0-20190405181648-9e8a8ae40bd4` 在 GitHub 上查无此 commit（API 返回 422），
   导致 `go mod tidy` 全量失败。改为真实版本 `v0.0.0-20190401172101-9e8deecbddbd`。
2. **`internal/storage/mount.go`：裸用 model 包常量**
   `d.cfg.WatchMode == WatchInotify` → `WatchInotify` 定义在 model 包，storage 未导入。
   在 storage 包本地定义 `watchInotify/watchPoll`，保持 storage 不反向依赖 model。
3. **`internal/thumb/generate_vips.go`：govips API 用错**
   `img.ThumbnailImage()` 在 govips v2 中不存在，改为 `img.Thumbnail(w, h, vips.InterestingNone)`。
4. **`internal/thumb/thumb.go`：类型不匹配**
   `int(size)`（size 为 `storage.ThumbSize` 字符串类型）无法转换；
   `cachePath()` 签名改为接收 `sizeKey string`，`EnqueueAll` 改用 `sizes[storage.ThumbMD]`。
5. **`internal/model/model.go`：嵌入结构体导致字面量失效**
   User/Group/Library/MountDir/MediaFile 均嵌入 `BaseModel`，
   而业务代码大量使用 `CreatedAt: &now` 直接赋值（Go 不允许在复合字面量中设置嵌入字段的提升字段）。
   将 `BaseModel` 展开为显式 `ID/CreatedAt/UpdatedAt` 字段，一次性修复全部调用点。
6. **`internal/api`：login 归属错误 + 未使用 import**
   `s.login` → `s.auth.login`（login 是 Auth 的方法）；
   移除 `media.go` 中未使用的 `net/http`。

### 运行时修复

7. **`media_files` 缺少复合唯一索引**（扫描 100% 失败的根因）
   scanner 的 upsert 使用 `ON CONFLICT (library_id, relative_path)`，
   但 model 的 `uniqueIndex` 只建在 `relative_path` 单列上，与迁移脚本
   `uk_media_lib_path ON media_files(library_id, relative_path)` 不一致，
   AutoMigrate 建表后报 `SQL logic error: ON CONFLICT clause does not match ...`。
   给 LibraryID/RelativePath 加上 `uniqueIndex:uk_media_lib_path,priority:1/2` 复合索引。

### 构建链路增强

- Dockerfile 新增 `APT_MIRROR` 构建参数：不加时容器内走 `deb.debian.org` 实测仅 **30 KB/s**，
  167MB 依赖需 90 分钟；指定 `mirrors.aliyun.com` 后大幅提速。
- `deploy/build.sh` 支持 `APT_MIRROR` 环境变量。
- Dockerfile 用 `go mod download` 替代 `go mod tidy`（不解析 test-only 依赖，更稳），
  构建时加 `GOFLAGS=-mod=mod` 自动补齐 go.sum。

### 部署验证结果（192.168.1.17）

- 镜像 `naspic:1.0.1` 358MB，容器 `healthy`
- 登录接口返回 64 位 token；只读挂载 `/photos:/photos:ro`，容器内 `writable=false`
- 扫描 3 张测试图：`scanned=3 added=3 failed=0`
- 缩略图接口返回 `200 / image/webp / 878B`，libvips 后端工作正常，缓存落盘
- 前端页面 HTTP 200，`/assets` 正常加载

---

## v1.0.1 — 服务端 Docker 编译与部署（构建链路修复）

修复了会让服务器构建**必然失败**的 4 个硬伤，并补齐前端托管：

- **修复 `deploy/Dockerfile` 构建上下文错位**：原写法 `COPY go.mod go.sum*` 假设上下文是 `server/`，
  但脚本传的是项目根目录，会直接 `COPY failed`。现改为三段构建，上下文统一为项目根目录：
  - 阶段1 `node:20-bookworm-slim` 构建 `web/` → 产出 `dist`
  - 阶段2 `golang:1.22-bookworm` 编译 `server/`（cgo + libvips）
  - 阶段3 `debian:bookworm-slim` 仅放二进制 + 前端静态文件
- **修复缺失 `go.sum` 导致构建中断**：仓库未提交 go.sum，改为在构建阶段在线 `go mod tidy` 生成，
  并内置 `GOPROXY=https://goproxy.cn,direct` 便于国内服务器
- **修复 healthcheck 必失败**：原使用 `wget`，但运行镜像未安装；改为安装 `curl` 并用 curl 探活
- **新增 Web 前端托管**：此前镜像只有 API，没有界面
  - `config.go` 新增 `Server.WebRoot`（默认 `/web`，支持 `NASPIC_WEB_ROOT` 覆盖）
  - `api.go` 新增 `mountWeb()`：`/assets` 静态 + SPA fallback；
    目录缺失时静默降级为纯 API 模式，不影响服务启动
- **新增 `deploy/build.sh`**：服务器本地一键构建脚本，支持
  `IMAGE` / `VERSION` / `TAGS` / `NPM_REGISTRY` / `GO_VERSION` / `NODE_VERSION`
- **新增 `docs/12-服务器Docker编译与部署教程.md`**：12 章完整教程
  - 30 秒速查版、架构与目录规划、服务器准备（Docker/防火墙/资源检查）
  - 三种构建方式（一键脚本 / 手动 build / compose --build）+ 构建参数表
  - 配置与数据目录准备、只读挂载启动、compose 编排
  - 首次初始化（改密码、添加挂载目录、**容器内路径 vs 宿主机路径**的坑）
  - Nginx 反代 + Let's Encrypt HTTPS、手机 APP 接入与后台保活
  - 升级/备份/crontab、14 条故障排查表、多架构 / 离线 / MySQL / AI 进阶
- **优化**：Dockerfile 与 buildx.sh 支持 `TAGS`（置空即纯 Go 缩略图后端，规避 libvips 版本不匹配）
  与 `NPM_REGISTRY`（国内 npm 加速）
- **更新** `deploy/docker-compose.yml`：内置 build 段、curl 健康检查、web_root 环境变量
- **更新** `server/config.example.yaml`：新增 `server.web_root`

---

## v1.0.0 — 部署手册（README 场景11）

- 新增 `docs/11-部署手册.md`：面向家庭 NAS 用户的完整部署教程
  - 硬件建议（入门/主流/进阶三档 + 存储介质建议）
  - Docker 部署：`docker run` 只读挂载示例、多目录挂载、compose、低配 ARM 资源限制
  - 初始配置：改默认密码、添加挂载目录、忽略规则、扫描
  - 手机 APP 接入：内网/公网地址选择、权限授予
  - 手机文件夹同步配置：系统相册 + 自定义文件夹独立策略
  - **挂载目录 10 条注意事项**（:ro 只读、路径一致、SQLite 不在只读点、群晖/绿联差异、忽略规则…）
  - 常见故障排查清单（16 条，含 SQLite readonly、inotify 降级、HEIC 缩略图、APP 后台被杀等）
  - 备份与升级、安全建议
- 项目完成 README 全部 11 个场景，进入 v1.0.0

---

## v0.9.0 — Uni-app 自动备份设置页（README 场景10）

- 新增 `docs/10-移动端自动备份设置页.md`：页面结构、交互逻辑、关键代码、平台差异、数据表映射
- 新增 `app/pages/backup/backup.vue`：
  - 总开关 + 系统相册备份（仅WiFi/允许流量、原图/压缩、照片/视频）
  - **自定义文件夹**：SAF 目录选择器 + 持久化授权；每个文件夹独立配置（启用、仅WiFi、原图、含子目录、目标库）
  - 冲突处理三选项（保留双方重命名 / 跳过 / 仅去重）
  - 同步状态与历史：立即同步 + 进度条 + 失败列表 + 同步日志 + 上次同步时间
  - 后台保活：Android 电池优化白名单/自启动引导、iOS 后台刷新引导
- 新增 `app/pages/index/index.vue`：服务器连接（登录）+ 立即同步 + 入口跳转
- 新增 `app/main.js`、`App.vue`、`pages.json`、`utils/device.js`、`utils/folderPicker.js`
- 所有配置变更即时上报服务端，Web 端「存储管理 → 手机设备」可见同一份配置与进度

---

## v0.8.0 — Vue3 Web 存储管理页面（README 场景9）

- 新增 `docs/09-Web存储管理页面.md`：页面结构、组件划分、功能对照、关键代码片段
- 新增 `web/`（Vue3 + Element Plus + Vite）：
  - `src/api/index.js`：全部后端接口封装（含权限、扫描、同步）
  - `src/views/StorageManage.vue`：**存储管理主页**
    - 驱动列表（类型/路径/读写/媒体数/inotify 降级提示）+ 增量扫描、重建索引、进度、权限、删除
    - 添加挂载目录 Dialog：路径、只读/读写 + 删除双开关、inotify/轮询、递归、忽略规则、扩展名白名单
    - 权限 Dialog：支持把库/挂载目录授权给**多用户或多群组**（只读/可上传/管理）
    - 手机设备 Collapse + 每设备文件夹同步任务表（仅WiFi、原图、含子目录、进度、状态、失败列表）
    - 扫描任务 Drawer（进度条 + 新增/缺失/失败统计 + 耗时）与错误日志 Dialog
  - `src/views/Login.vue`、`router`、`App.vue`、Vite 分包配置（低配设备首屏优化）
- 后端新增 `internal/api/permission.go`：权限增删改查 + 用户/群组列表与建组接口

---

## v0.7.0 — AI 人脸/场景识别模块（README 场景7）

- 新增 `docs/07-AI识别模块设计.md`：模型选型、量化裁剪方案、资源控制、ARM64 部署 8 项注意、隐私说明
- 新增 `server/internal/ai`：
  - `ai.go`：低优先级 feeder（每 30s 补 200 张）+ worker 池、状态机、人脸贪心聚类（余弦 ≥0.6）、向量序列化
  - `engine_onnx.go`（build tag `ai`）：ONNX Runtime 后端，UltraFace 检测 + MobileFaceNet 特征 + MobileNetV3 场景分类
  - `engine_stub.go`（build tag `!ai`）：未启用时自动停用，服务正常启动
- 只存 `embedding` 特征向量，不保存任何裁剪人脸图；全程离线零网络请求
- `main.go` 接入：注册路径解析器，AI 未启用时自动跳过且不影响启动

---

## v0.6.0 — Docker 多架构打包（README 场景6）

- 新增 `docs/06-Docker多架构打包.md`：Dockerfile 设计取舍、buildx 命令、CI 思路、ARM 构建 10 大坑、只读挂载示例、Nginx 反代注意
- 新增 `deploy/Dockerfile`：golang:1.22-bookworm 构建（libvips-dev）+ debian:bookworm-slim 运行（libvips42），内置健康检查
- 新增 `deploy/buildx.sh`：一键创建 builder、注册 QEMU、构建并推送 amd64+arm64
- 新增 `deploy/docker-compose.yml`：`/photos:/photos:ro` 只读挂载示例，含资源限制与 MySQL 可选配置
- 关键决策：构建阶段不使用 `--platform=$BUILDPLATFORM`，由 buildx/QEMU 在目标架构下原生（模拟）编译，绕开 cgo + 交叉 C 依赖

---

## v0.5.0 — libvips 缩略图模块（README 场景5）

- 新增 `docs/05-缩略图模块设计.md`：模块设计、格式矩阵、ARM 编译 libvips 注意事项与常见坑
- 新增 `server/internal/thumb`：
  - `thumb.go`：懒加载生成 + 独立缓存目录 + worker 池 + 单飞锁（同文件并发只生成一次）
  - `generate_vips.go`（build tag `vips`）：libvips 后端，`thumbnail_image` 利用 DCT 缩放，内部线程压到 1
  - `generate_go.go`（build tag `!vips`）：纯 Go 降级后端（JPEG/PNG/GIF/WebP 解码，输出 JPEG）
- 缓存路径：`<cache>/thumbs/<lib>/<hash[:2]>/<hash>/<size>_<px>.webp`，含原图 hash 天然失效
- 损坏文件标记 `thumb_status=2` 不再重试，不阻塞其它图片
- `StorageDriver` 新增 `LibraryID()`，供缩略图按库定位媒体记录
- 启动注册：`storage.RegisterThumbProvider(thumb.New(db, cfg))`

---

## v0.4.0 — Uni-app 移动端同步模块（README 场景4）

- 新增 `docs/04-移动端同步设计.md`：模块架构、双层状态机、三级增量识别、冲突策略、断点续传、局域网优先、Android/iOS 权限坑
- 新增 `app/` 同步引擎实现：
  - `utils/sha256.js`：纯 JS SHA-256，无第三方依赖
  - `utils/hash.js`：文件哈希（<20MB 全量，≥20MB 头1MB+尾1MB+size 采样）
  - `utils/net.js`：网络类型判定与 WiFi 等待
  - `utils/api.js`：接口封装 + **局域网优先探测**（LAN/公网并行探活 800ms，取最快可达者）
  - `utils/store.js`：任务/记录/日志本地持久化（防抖落盘，预留 SQLite 切换）
  - `sync/scanner.js`：系统相册（Android MediaStore 增量游标）+ 自定义文件夹递归遍历
  - `sync/queue.js`：并发队列 + 指数退避重试
  - `sync/uploader.js`：秒传 → init → 分片（跳过已传分片）→ complete
  - `sync/engine.js`：策略决策（WiFi/原图/子目录/文件类型）+ 状态流转 + 结果上报
  - `store/sync.js`：Vue3 reactive 全局同步状态
- 安全策略落地：APP 侧无任何删除手机本地文件的调用；冲突由服务端重命名，双方保留

---

## v0.3.0 — StorageDriver 存储驱动 + 后端骨架（README 场景3）

- 新增 `docs/03-存储驱动与上传接口设计.md`
- 后端骨架 `server/`（Golang，Gin + GORM）：
  - `internal/config`：配置加载 + 按内存自动推导 low/mid/high 档位（SQLite 默认、并发限流、AI 默认关）
  - `internal/model`：与 001_init 一一对应的 19 个 GORM 模型
  - `internal/store`：SQLite（纯 Go，免 cgo）/ MySQL 双驱动初始化与自动迁移
  - `internal/storage`：`StorageDriver` 抽象 + `LocalDriver`（托管）+ `MountLocalDirDriver`（挂载只读优先）
  - `internal/scanner`：有界队列 + worker 池 + 令牌桶 + CPU 守护的扫描调度，inotify/轮询双模式监听
  - `internal/media`：EXIF 解析（失败静默降级为 mtime，不中断扫描）
  - `internal/api`：认证、相册库/挂载目录、扫描、媒体浏览、分片上传、手机同步任务全量接口
  - `cmd/server`：服务入口（驱动自举、定时增量扫描兜底、优雅退出）
- 关键实现：
  - 只读挂载双保险：`Writable()` 拦截 + `Delete(physical)` 需读写模式与 `allow_delete` 双开关
  - 挂载库 `UploadFile` 直接返回 `ErrReadOnly`
  - 路径穿越防护 `SafeJoin`；Upsert 不覆盖 `taken_at`
  - 分片上传：check 秒传 → init → chunk（断点续传）→ complete（合并 + SHA-256 校验 + 二次查重）
  - 同名冲突自动重命名，绝不覆盖

---

## v0.2.0 — 数据库表结构设计（README 场景2）

- 新增 `docs/02-数据库设计.md`：14 张表字段说明 + 索引设计 + 性能要点
- 新增 `server/migrations/001_init.sqlite.sql`（SQLite 3.35+ 默认）
- 新增 `server/migrations/001_init.mysql.sql`（MySQL 5.7+）
- 关键设计：
  - `media_files.source_type` 区分「托管存储文件」与「挂载目录索引文件」
  - `media_files` 建 `uk_lib_path` / `idx_lib_taken` / `idx_hash` 等 7 个索引，支撑百万级时间轴分页
  - `mount_dirs.mode` 默认只读 + `allow_delete` 高危开关，双保险保护宿主机原图
  - `permissions` 支持挂载目录授权给多用户/多群组
  - `sync_tasks` 每设备每文件夹一条配置（含 `wifi_only`/`upload_original`/`include_subdir`/`sync_cursor`）
  - `sync_records` 用 `local_path_hash` 做唯一键，规避 MySQL 3072 bytes 索引限制
  - 预留 `faces` / `face_clusters` / `media_scenes` 供 v0.7.0 AI 模块使用

---

## v0.1.0 — 需求与方案设计（README 场景1）

- 新增 `docs/01-需求与方案设计.md`
- 明确 MVP 必选功能 14 项 + 可选功能 10 项
- 定义模块分层与 `StorageDriver` 抽象接口
- 定义手机端同步三级增量识别、单文件状态机、冲突永不覆盖策略
- 梳理 10 条风险点（libvips ARM 编译、inotify 失效、OOM、只读误写等）与应对
- 给出 ARM 三档资源默认配置（low/mid/high）与 10 阶段开发优先级

---
