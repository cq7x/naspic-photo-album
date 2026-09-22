# Naspic 数据库安装向导实施方案

## Context

当前部署流程的痛点:`docker-compose.yml` 把 MySQL DSN 硬编码到 naspic 容器环境变量,用户改密码必须同时改 `.env` 和 `01-init.sql`,一旦 MySQL 数据卷残留旧密码就会 `Access denied` 反复失败。本次改造新增"首次部署 Web 安装向导":部署后浏览器打开 `http://<服务器>:8080` 自动跳到 `/setup` 页,用户填好 MySQL 主机/端口/库名/账号/密码 → 测试连接 → 保存 → 后端写 `/data/config.yaml` 并热加载 DB 与路由,然后跳到登录页用 `admin/naspic123` 登录。

## 触发条件

`cfg.Database.DSN == ""`(config.yaml 和环境变量都没给 DSN)。不引入新 `installed` 字段,避免双源真相。

## 后端改动

### 1. `server/cmd/server/main.go` —— setup 模式启动

把 `db, err := store.Init(cfg); err != nil { log.Fatalf(...) }` 改成条件分支:

- DSN 非空:走原有完整流程(Init → EnsureDefaultAdmin → bootstrapDrivers → api.New → 监听)
- DSN 为空:`log.Printf("[boot] 数据库未配置,进入安装模式")`,只构造 `api.NewSetupServer(cfg, *cfgPath, onInstall)`,挂载 setup 路由,监听端口

**热加载**:`httpSrv.Handler` 改为 `http.HandlerFunc` 包装,内部从 `atomic.Value` 取当前 `http.Handler`。`onInstall` 回调里执行完整启动序列(Init → EnsureDefaultAdmin → bootstrapDrivers → thumb.New → ai.New → scanMgr → watch → api.New),最后 `handler.Store(srv.Engine())` 原子切换,无需重启容器。

### 2. 新建 `server/internal/api/setup.go`

```go
type SetupServer struct {
    cfg       *config.Config
    cfgPath   string
    onInstall func(*config.Config) error
}
func NewSetupServer(cfg *config.Config, cfgPath string, cb func(*config.Config) error) *SetupServer
func (s *SetupServer) Engine() *gin.Engine
```

Engine 只挂 3 个接口 + health + SPA fallback,其余 NoRoute 返 503:

| 路径 | 方法 | 入参 | 出参 | 逻辑 |
|---|---|---|---|---|
| `/api/v1/setup/status` | GET | 无 | `{need_setup, defaults:{host:"mysql",port:3306,db:"naspic",user:"naspic"}}` | 读 `cfg.Database.DSN`,空则 need_setup=true |
| `/api/v1/setup/test` | POST | `{host,port,db,user,password}` | `{ok, latency_ms, version}` | 拼 DSN `gorm.Open(mysql.Open(...))` 不调 AutoMigrate,ping 后立即 Close |
| `/api/v1/setup/save` | POST | 同上 | `{ok, admin:{username:"admin",password:"naspic123"}}` | 1) 拼 DSN 2) `config.SaveDatabase(cfgPath,"mysql",dsn)` 3) 改 `cfg.Database` 4) 调 `s.onInstall(cfg)` 触发热加载 |

### 3. `server/internal/config/config.go` —— 新增 `SaveDatabase`

```go
func SaveDatabase(path, driver, dsn string) error
```

用 `yaml.Node` 读取现有 config.yaml(若不存在则创建 DocumentNode),定位或新建 `database` Map,更新 `driver`/`dsn` 两个 ScalarNode,保留其他段与注释原样,Marshal 写回。yaml.v3 已 import,无需新增依赖。

### 4. `server/internal/api/api.go` —— 不动

完整 engine 仍由 `api.New(...).Engine()` 产出,setup mode 只是不调用它,等 onInstall 时才构造。

## 前端改动

### 5. 新建 `web/src/views/Setup.vue`

`el-form` 字段:host(默认 mysql)、port(3306)、db(naspic)、user(naspic)、password(password)。布局模仿 `Login.vue` 风格(居中卡片)。两按钮:

- 「测试连接」:POST `/setup/test`,成功后 `testedOk=true` 启用保存按钮
- 「保存并初始化」:POST `/setup/save`,成功后 `router.replace('/login')`

加载时先 GET `/setup/status`,把 `defaults` 回填表单,方便用户直接点保存即可。

### 6. `web/src/router/index.js`

加路由:
```js
{ path: '/setup', name: 'setup',
  component: () => import('../views/Setup.vue'),
  meta: { title: '数据库安装', public: true } }
```

守卫改为:未登录时先 GET `/setup/status`,若 `need_setup=true` 则跳 `/setup`,否则原逻辑跳 `/login`。

### 7. `web/src/App.vue`

`isLogin` 改:
```js
const isLogin = computed(() => route.path === '/login' || route.path === '/setup')
```
让 Setup 页也走简化布局,不显示导航栏。

### 8. `web/src/views/Login.vue`

顶部加 `el-alert` 提示「默认账号 admin / naspic123,登录后请立即修改密码」。

### 9. `web/src/api/index.js`

新增 `setupStatus() / setupTest(form) / setupSave(form)`,直接走 axios baseURL(`/api/v1`),不要走需要 token 的拦截器。

## 部署文件改动

### 10. `deploy/docker-compose.yml`

naspic 服务的 environment 删掉两行:
```yaml
# NASPIC_DB_DRIVER: mysql     # 由 Web 安装向导写入 config.yaml
# NASPIC_DB_DSN: "..."        # 同上
```
MySQL 容器、`01-init.sql`、Redis 全部不动。naspic 容器首次启动时 DSN 为空,自动进入 setup mode。

### 11. `server/config.example.yaml`

`database.dsn` 默认值改成空字符串,保留注释说明"首次启动由 Web 安装向导填写"。

## 验证步骤

1. `docker compose up -d` 起三个容器,浏览器访问 `http://<host>:8080` 应自动跳到 `/#/setup`
2. 表单已预填 `mysql/3306/naspic/naspic`,点「测试连接」返回 ok + MySQL 版本号
3. 点「保存并初始化」,2~3 秒内返回成功;后端日志见 `[store] 数据库就绪 driver=mysql` + `提示：默认管理员 admin / naspic123`
4. 自动跳到 `/#/login`,用 `admin/naspic123` 登录成功,进入主界面
5. 进容器 `docker exec naspic cat /data/config.yaml`,`database.dsn` 已正确写入;`docker restart naspic` 后不再进入 setup mode,直接连库

## 关键文件

- `server/cmd/server/main.go` —— 启动模式分支 + atomic 热加载
- `server/internal/api/setup.go` —— 新建,3 个 setup 接口
- `server/internal/api/api.go` —— 现有 engine 不改,只在 setup 模式下延后调用
- `server/internal/config/config.go` —— 新增 `SaveDatabase` 函数
- `web/src/views/Setup.vue` —— 新建安装页
- `web/src/router/index.js` —— 加 `/setup` 路由 + 守卫
- `web/src/App.vue` —— `isLogin` 判断加 `/setup`
- `web/src/views/Login.vue` —— 加默认账号提示
- `web/src/api/index.js` —— 加 setup 三个 API 函数
- `deploy/docker-compose.yml` —— 删 naspic 的 DB 环境变量
- `server/config.example.yaml` —— `database.dsn` 默认改空
