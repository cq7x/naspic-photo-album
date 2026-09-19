# Naspic 私有云相册 — Web 存储管理页面（Vue3 + Element Plus）

> 对应 README 场景9 ｜ 版本：v0.8.0 ｜ 代码：`web/`

---

## 1. 页面结构

```
App.vue
└─ router
   ├─ /login     Login.vue
   └─ /storage   StorageManage.vue   ← 存储管理主页
        ├─ 存储驱动列表（表格）
        │    行操作：增量扫描 / 重建索引 / 进度 / 权限 / 删除
        ├─ 手机设备与文件夹同步（Collapse + 内嵌表格）
        │    行操作：失败列表 / 删除任务
        ├─ 添加挂载目录 Dialog
        ├─ 新建托管库 Dialog
        ├─ 扫描任务 Drawer（进度 + 耗时）
        ├─ 扫描日志 Dialog
        ├─ 权限配置 Dialog（多用户 / 多群组）
        └─ 同步失败列表 Dialog
```

## 2. 组件划分

| 组件 | 职责 | 复用于 |
|---|---|---|
| `StorageManage.vue` | 页面容器与数据编排 | — |
| （可拆）`LibraryTable.vue` | 驱动列表 + 行操作 | 首页 |
| （可拆）`MountDirDialog.vue` | 挂载目录表单 | 新增 / 编辑 |
| （可拆）`ScanJobsDrawer.vue` | 扫描进度与日志 | 各库 |
| （可拆）`DeviceSyncPanel.vue` | 设备 + 同步任务树 | 首页 |
| （可拆）`PermissionDialog.vue` | 授权主体选择 | 库 / 挂载目录 |

> 当前版本为减少首屏请求合并为单文件组件；设备多、任务多时建议按上表拆分并配合 `defineAsyncComponent` 懒加载。

## 3. 功能对照

| 需求 | 实现位置 |
|---|---|
| ① 查看存储驱动列表 | 主表格，展示类型、路径、读写、媒体数、监听方式（含 inotify 降级提示） |
| ② 添加挂载本地目录 | 「添加挂载目录」Dialog：路径、只读/读写、inotify/轮询、递归、忽略规则、扩展名白名单 |
| ③ 权限与忽略配置 | 「权限」Dialog（多用户/多群组 + 三种权限级别）；忽略规则在创建表单中配置 |
| ④ 手机设备与文件夹同步任务 | Collapse 按设备分组，内嵌表格展示每个文件夹的独立策略与进度 |
| ⑤ 手动扫描 / 重建索引 / 暂停 | 行操作「增量扫描」（`full:false`）与「重建索引」（`full:true`） |
| ⑥ 扫描进度 / 同步进度 / 错误日志 | Drawer 展示 job 进度条与统计；「日志」Dialog 展示 error/warn 明细；同步任务展示成功/跳过/失败计数 |
| ⑦ 低配设备适配 | Vite 分包、组件懒加载、表格 `size="small"`、媒体分页默认 60 条游标分页（后端 `(taken_at, id)` 游标） |

## 4. 关键代码片段

### 4.1 触发扫描

```js
async function scan(row, full) {
  await triggerScan(row.id, full)     // POST /libraries/:id/scan  { full }
  ElMessage.success(full ? '已提交全量重建索引任务' : '已提交增量扫描任务')
  setTimeout(() => showJobs(row), 300)
}
```

### 4.2 挂载目录表单（只读优先）

```js
const mountForm = reactive({
  library_name: '', host_path: '',
  mode: 1,                       // 1=只读（默认）2=读写（高危）
  allow_delete: false,           // 仅 mode=2 时可开
  watch_mode: 2,                 // 1=inotify 2=轮询，失败服务端自动降级
  poll_interval_sec: 300,
  recursive: true,
  ignore_rules: ['**/@eaDir/**', '**/.thumbnails/**'],
  include_exts: [],
})
```

### 4.3 同步进度计算

```js
function progressOf(row) {
  const total = (row.synced_count || 0) + (row.failed_count || 0) + (row.skipped_count || 0)
  if (!row.total_count) return 0
  return Math.min(100, Math.floor((total / row.total_count) * 100))
}
```

### 4.4 后端游标分页（低配友好）

```go
// 前端传 cursor_time + cursor_id，避免深翻页 OFFSET 性能崩塌
if cursorTime != "" && cursorID > 0 {
    q = q.Where("(taken_at < ? OR (taken_at = ? AND id < ?))", t, t, cursorID)
}
q.Order("taken_at DESC, id DESC").Limit(limit).Find(&list)
```

## 5. 运行

```bash
cd web
npm install
npm run dev      # http://localhost:5173（已配置 /api 代理到 127.0.0.1:8080）
npm run build    # 产物 dist/，可直接挂到后端静态目录或 Nginx
```

Nginx 反代注意（分片上传）：

```nginx
client_max_body_size 0;
proxy_request_buffering off;
proxy_read_timeout 300s;
```
