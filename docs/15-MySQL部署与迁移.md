# 15 · MySQL 部署与 SQLite 迁移

v1.6.0 起，Naspic 的业务数据（用户、相册库、媒体索引、相册、扫描缓存……）
默认存放在 **MySQL 8.0** 里，不再用 SQLite 文件。
底层驱动早已支持 `database.driver: mysql`，这一版只是把它接进 Docker 部署。

---

## 一、目录规划（重要）

**数据卷绝对不能指向代码目录。** 代码目录会被反复删除重建（重新同步），
数据卷放里面会被一起删掉。

| 用途 | 推荐路径 | 说明 |
| --- | --- | --- |
| 应用数据 | `/home/ubuntu/naspic-data` | 缩略图缓存、临时文件、`config.yaml` |
| MySQL 数据 | `/home/ubuntu/naspic-mysql/data` | 全部业务表 |
| 宿主机照片 | `/photos` | 只读挂载 `:ro` |

这些路径通过 `deploy/.env` 覆盖（compose 会自动读取同目录 `.env`）：

```bash
cp deploy/.env.example deploy/.env
# 按需修改密码与目录
```

---

## 二、首次启动

```bash
cd 项目根目录
./deploy/build.sh                                   # 构建 naspic 镜像
docker compose -f deploy/docker-compose.yml up -d   # 起 mysql + naspic
docker compose -f deploy/docker-compose.yml logs -f naspic
```

`mysql` 服务启动时会执行 `deploy/mysql/init/01-init.sql`（**只在数据卷为空时执行一次**）：
建库 `naspic`（utf8mb4）、建业务账号并授权。

`naspic` 通过 `depends_on: mysql: condition: service_healthy` 等 MySQL 就绪后才启动，
不用担心"库还没起来就连"的启动竞争。

验证：

```bash
curl -s http://127.0.0.1:8080/api/v1/health          # version 应为当前版本
docker compose -f deploy/docker-compose.yml exec mysql \
  mysql -unaspic -p"$MYSQL_PASSWORD" -e "show tables;" naspic
```

---

## 三、从旧版 SQLite 迁移

索引数据是可以重建的（照片本身在宿主机上，不会丢），所以官方路线是 **重新挂载 + 重扫**：

1. **备份旧库**（可选但推荐）
   ```bash
   cp /home/ubuntu/naspic-data/naspic.db /home/ubuntu/naspic-data/naspic.db.bak
   ```
2. 按上面步骤启动 MySQL 版。
3. 登录 Web → **设置 → 存储管理 → 添加挂载目录**，把 `/photos` 挂回去
   （容器内路径与宿主机路径保持一致）。
4. 点 **重建索引**。照片、EXIF、缩略图、扫描指纹全部重建。
5. 缩略图缓存目录（`naspic-data/cache/thumbs`）如果保留，命中就直接复用；
   不保留也会懒加载重建。

> 旧 SQLite 文件不会被删除，确认新库没问题后再自行清理。

---

## 四、仍然想用 SQLite

适合低配 ARM / 单机小库场景。把 compose 里 `naspic` 的环境变量改成：

```yaml
environment:
  NASPIC_DB_DRIVER: sqlite
  NASPIC_DB_DSN: /data/naspic.db
```

并去掉 `depends_on.mysql`。其余不变。

---

## 五、踩过的坑

### 1. `Specified key was too long; max key length is 3072 bytes`（error 1071）

MySQL InnoDB 单列索引上限 **3072 字节**，`utf8mb4` 按 **4 字节/字符** 计算。
`media_files.relative_path` 与 `scan_cache.rel_path` 原本是 `size:1024`，
参与 `(library_id, relative_path)` 复合唯一索引时：
`8 + 1024×4 = 4104 > 3072` → 建表直接失败。

**修法**：两处统一改为 `size:512`（`8 + 512×4 = 2056 < 3072`）。
512 字符的相对路径对任何实际目录都够用。

> 以后新增"要进索引的字符串列"时，长度别超过 700（`700×4 = 2800`）。

### 2. 时区

DSN 用 `parseTime=True&loc=Local`，配合容器 `TZ=Asia/Shanghai` 与镜像内的 `tzdata`。
不要写 `loc=Asia%2FShanghai` —— 一旦运行镜像里没有 tzdata，
`time.LoadLocation` 会直接让连接池初始化失败。

### 3. 排序规则

服务端指定了 `utf8mb4_unicode_ci`，能正确存 emoji 文件名，
也避免 `utf8mb4_0900_ai_ci`（MySQL 8 默认）在部分低版本客户端上的兼容问题。
