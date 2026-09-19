-- Naspic 私有云相册 初始迁移（SQLite）
-- 版本: v0.2.0  适用: SQLite 3.35+
-- MySQL 版本见 001_init.mysql.sql

PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
PRAGMA foreign_keys = ON;

-- ===================== 用户与群组 =====================
CREATE TABLE IF NOT EXISTS users (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    username       TEXT    NOT NULL UNIQUE,
    password_hash  TEXT    NOT NULL,
    nickname       TEXT    DEFAULT '',
    avatar         TEXT    DEFAULT '',
    role           INTEGER NOT NULL DEFAULT 2,      -- 1=管理员 2=普通用户
    status         INTEGER NOT NULL DEFAULT 1,      -- 1=正常 2=禁用
    last_login_at  DATETIME,
    created_at     DATETIME,
    updated_at     DATETIME,
    deleted_at     DATETIME
);

CREATE TABLE IF NOT EXISTS groups (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL UNIQUE,
    remark      TEXT    DEFAULT '',
    created_at  DATETIME,
    updated_at  DATETIME,
    deleted_at  DATETIME
);

CREATE TABLE IF NOT EXISTS user_groups (
    user_id  INTEGER NOT NULL,
    group_id INTEGER NOT NULL,
    PRIMARY KEY (user_id, group_id)
);

-- ===================== 相册库 / 挂载目录 =====================
CREATE TABLE IF NOT EXISTS libraries (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    name               TEXT    NOT NULL,
    type               INTEGER NOT NULL,             -- 1=托管存储 2=挂载外部目录
    owner_id           INTEGER NOT NULL DEFAULT 0,
    storage_root       TEXT    DEFAULT '',
    quota_bytes        INTEGER NOT NULL DEFAULT 0,   -- 0=不限
    used_bytes         INTEGER NOT NULL DEFAULT 0,
    default_visibility INTEGER NOT NULL DEFAULT 1,   -- 1=私有 2=家庭可见
    status             INTEGER NOT NULL DEFAULT 1,   -- 1=正常 2=暂停扫描 3=异常
    media_count        INTEGER NOT NULL DEFAULT 0,
    created_at         DATETIME,
    updated_at         DATETIME,
    deleted_at         DATETIME
);
CREATE INDEX IF NOT EXISTS idx_libraries_owner ON libraries(owner_id, deleted_at);

CREATE TABLE IF NOT EXISTS mount_dirs (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    library_id         INTEGER NOT NULL UNIQUE,
    host_path          TEXT    NOT NULL,
    mode               INTEGER NOT NULL DEFAULT 1,   -- 1=只读(默认) 2=读写(高危)
    allow_delete       INTEGER NOT NULL DEFAULT 0,   -- 读写模式下删除开关
    watch_mode         INTEGER NOT NULL DEFAULT 2,   -- 1=inotify 2=轮询
    poll_interval_sec  INTEGER NOT NULL DEFAULT 300,
    recursive          INTEGER NOT NULL DEFAULT 1,
    ignore_rules       TEXT    DEFAULT '[]',         -- JSON array glob
    include_exts       TEXT    DEFAULT '[]',         -- JSON array, 空=全部
    scan_status        INTEGER NOT NULL DEFAULT 1,   -- 1=空闲 2=扫描中 3=暂停
    last_scan_at       DATETIME,
    last_scan_cost_ms  INTEGER NOT NULL DEFAULT 0,
    file_count         INTEGER NOT NULL DEFAULT 0,
    error_message      TEXT    DEFAULT '',
    created_at         DATETIME,
    updated_at         DATETIME,
    deleted_at         DATETIME
);
CREATE INDEX IF NOT EXISTS idx_mount_dirs_status ON mount_dirs(scan_status);

-- ===================== 媒体元数据（核心大表） =====================
CREATE TABLE IF NOT EXISTS media_files (
    id                     INTEGER PRIMARY KEY AUTOINCREMENT,
    library_id             INTEGER NOT NULL,
    source_type            INTEGER NOT NULL,          -- 1=托管存储 2=挂载目录索引
    relative_path          TEXT    NOT NULL,
    filename               TEXT    NOT NULL DEFAULT '',
    ext                    TEXT    NOT NULL DEFAULT '',
    mime                   TEXT    NOT NULL DEFAULT '',
    size_bytes             INTEGER NOT NULL DEFAULT 0,
    hash                   TEXT    NOT NULL DEFAULT '',
    hash_algo              TEXT    NOT NULL DEFAULT 'sha256',
    media_type             INTEGER NOT NULL DEFAULT 1, -- 1=图片 2=视频 3=其他
    width                  INTEGER NOT NULL DEFAULT 0,
    height                 INTEGER NOT NULL DEFAULT 0,
    duration_ms            INTEGER NOT NULL DEFAULT 0,
    orientation            INTEGER NOT NULL DEFAULT 0,
    taken_at               DATETIME,
    taken_at_source        INTEGER NOT NULL DEFAULT 2, -- 1=exif 2=mtime 3=manual
    camera_make            TEXT    DEFAULT '',
    camera_model           TEXT    DEFAULT '',
    gps_lat                REAL,
    gps_lon                REAL,
    exif_json              TEXT    DEFAULT '',
    index_status           INTEGER NOT NULL DEFAULT 1, -- 1=正常 2=源文件缺失 3=损坏
    thumb_status           INTEGER NOT NULL DEFAULT 0, -- 0=未生成 1=已生成 2=失败
    ai_status              INTEGER NOT NULL DEFAULT 0, -- 0=未处理 1=已处理 2=跳过 3=失败
    uploaded_by_device_id  INTEGER,
    original_local_path    TEXT    DEFAULT '',
    created_at             DATETIME,
    updated_at             DATETIME,
    deleted_at             DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_media_lib_path ON media_files(library_id, relative_path);
CREATE INDEX IF NOT EXISTS idx_media_lib_taken    ON media_files(library_id, taken_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_media_hash         ON media_files(hash);
CREATE INDEX IF NOT EXISTS idx_media_lib_status   ON media_files(library_id, index_status, media_type);
CREATE INDEX IF NOT EXISTS idx_media_thumb        ON media_files(thumb_status, library_id);
CREATE INDEX IF NOT EXISTS idx_media_ai           ON media_files(ai_status, library_id);
CREATE INDEX IF NOT EXISTS idx_media_device       ON media_files(uploaded_by_device_id);

-- ===================== 标签 =====================
CREATE TABLE IF NOT EXISTS tags (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    name       TEXT NOT NULL UNIQUE,
    kind       INTEGER NOT NULL DEFAULT 1,  -- 1=手动 2=AI 3=地点
    color      TEXT DEFAULT '',
    created_at DATETIME
);

CREATE TABLE IF NOT EXISTS media_tags (
    media_id INTEGER NOT NULL,
    tag_id   INTEGER NOT NULL,
    PRIMARY KEY (media_id, tag_id)
);
CREATE INDEX IF NOT EXISTS idx_media_tags_tag ON media_tags(tag_id);

-- ===================== 缩略图缓存 =====================
CREATE TABLE IF NOT EXISTS thumbnails (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    media_id    INTEGER NOT NULL,
    size_key    TEXT    NOT NULL,           -- sm/md/lg/sq
    rel_path    TEXT    NOT NULL,           -- 相对独立缓存目录
    width       INTEGER NOT NULL DEFAULT 0,
    height      INTEGER NOT NULL DEFAULT 0,
    bytes       INTEGER NOT NULL DEFAULT 0,
    source_hash TEXT    NOT NULL DEFAULT '',
    status      INTEGER NOT NULL DEFAULT 0, -- 0=生成中 1=就绪 2=失败
    created_at  DATETIME,
    updated_at  DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_thumb_media_size ON thumbnails(media_id, size_key);
CREATE INDEX IF NOT EXISTS idx_thumb_status ON thumbnails(status);

-- ===================== 权限 =====================
CREATE TABLE IF NOT EXISTS permissions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    subject_type  INTEGER NOT NULL,  -- 1=用户 2=群组
    subject_id    INTEGER NOT NULL,
    resource_type INTEGER NOT NULL,  -- 1=相册库 2=挂载目录
    resource_id   INTEGER NOT NULL,
    permission    INTEGER NOT NULL DEFAULT 1, -- 1=只读 2=上传 3=管理
    created_at    DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_perm ON permissions(subject_type, subject_id, resource_type, resource_id);
CREATE INDEX IF NOT EXISTS idx_perm_res ON permissions(resource_type, resource_id);

-- ===================== 手机同步 =====================
CREATE TABLE IF NOT EXISTS sync_devices (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL,
    device_uuid   TEXT NOT NULL UNIQUE,
    device_name   TEXT DEFAULT '',
    platform      INTEGER NOT NULL DEFAULT 1,  -- 1=Android 2=iOS
    app_version   TEXT DEFAULT '',
    last_ip       TEXT DEFAULT '',
    last_lan_ip   TEXT DEFAULT '',
    last_seen_at  DATETIME,
    status        INTEGER NOT NULL DEFAULT 1,  -- 1=正常 2=已吊销
    created_at    DATETIME,
    updated_at    DATETIME
);

CREATE TABLE IF NOT EXISTS sync_tasks (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    device_id         INTEGER NOT NULL,
    user_id           INTEGER NOT NULL DEFAULT 0,
    target_library_id INTEGER NOT NULL,
    folder_uri        TEXT NOT NULL,           -- 手机端文件夹标识
    folder_path       TEXT DEFAULT '',
    folder_type       INTEGER NOT NULL DEFAULT 2, -- 1=系统相册 2=自定义文件夹
    enabled           INTEGER NOT NULL DEFAULT 1,
    wifi_only         INTEGER NOT NULL DEFAULT 1,
    upload_original   INTEGER NOT NULL DEFAULT 1,
    include_subdir    INTEGER NOT NULL DEFAULT 1,
    file_types        TEXT DEFAULT '["image","video"]',
    sync_cursor       TEXT DEFAULT '',
    last_sync_at      DATETIME,
    status            INTEGER NOT NULL DEFAULT 1, -- 1=空闲 2=扫描中 3=同步中 4=暂停 5=异常
    total_count       INTEGER NOT NULL DEFAULT 0,
    synced_count      INTEGER NOT NULL DEFAULT 0,
    failed_count      INTEGER NOT NULL DEFAULT 0,
    skipped_count     INTEGER NOT NULL DEFAULT 0,
    last_error        TEXT DEFAULT '',
    created_at        DATETIME,
    updated_at        DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_sync_task_folder ON sync_tasks(device_id, folder_uri);
CREATE INDEX IF NOT EXISTS idx_sync_task_device ON sync_tasks(device_id, enabled);

CREATE TABLE IF NOT EXISTS sync_records (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    task_id           INTEGER NOT NULL,
    device_id         INTEGER NOT NULL DEFAULT 0,
    local_id          TEXT DEFAULT '',
    local_path        TEXT DEFAULT '',
    local_path_hash   TEXT NOT NULL DEFAULT '',
    file_hash         TEXT DEFAULT '',
    size_bytes        INTEGER NOT NULL DEFAULT 0,
    local_mtime       INTEGER NOT NULL DEFAULT 0,
    media_id          INTEGER,
    state             INTEGER NOT NULL DEFAULT 0, -- 0=待传 1=哈希 2=上传中 3=完成 4=失败 5=跳过 6=放弃
    upload_session_id TEXT DEFAULT '',
    chunk_total       INTEGER NOT NULL DEFAULT 0,
    chunk_done        INTEGER NOT NULL DEFAULT 0,
    retry_count       INTEGER NOT NULL DEFAULT 0,
    last_error        TEXT DEFAULT '',
    created_at        DATETIME,
    updated_at        DATETIME
);
CREATE UNIQUE INDEX IF NOT EXISTS uk_sync_record ON sync_records(task_id, local_path_hash);
CREATE INDEX IF NOT EXISTS idx_sync_record_state ON sync_records(task_id, state);

-- ===================== 分片上传会话 =====================
CREATE TABLE IF NOT EXISTS upload_sessions (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    session_id       TEXT NOT NULL UNIQUE,
    user_id          INTEGER NOT NULL DEFAULT 0,
    device_id        INTEGER,
    library_id       INTEGER NOT NULL,
    file_hash        TEXT NOT NULL DEFAULT '',
    size_bytes       INTEGER NOT NULL DEFAULT 0,
    chunk_size       INTEGER NOT NULL DEFAULT 0,
    chunk_total      INTEGER NOT NULL DEFAULT 0,
    uploaded_chunks  TEXT NOT NULL DEFAULT '[]',
    tmp_dir          TEXT DEFAULT '',
    relative_path    TEXT DEFAULT '',
    status           INTEGER NOT NULL DEFAULT 0, -- 0=进行中 1=完成 2=过期 3=取消
    expires_at       DATETIME,
    created_at       DATETIME,
    updated_at       DATETIME
);
CREATE INDEX IF NOT EXISTS idx_upload_status ON upload_sessions(status, expires_at);

-- ===================== 扫描任务与日志 =====================
CREATE TABLE IF NOT EXISTS scan_jobs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    library_id  INTEGER NOT NULL,
    type        INTEGER NOT NULL DEFAULT 1,  -- 1=增量 2=全量重建
    trigger_src INTEGER NOT NULL DEFAULT 1,  -- 1=手动 2=定时 3=inotify
    status      INTEGER NOT NULL DEFAULT 1,  -- 1=排队 2=运行中 3=完成 4=失败 5=取消
    total       INTEGER NOT NULL DEFAULT 0,
    scanned     INTEGER NOT NULL DEFAULT 0,
    added       INTEGER NOT NULL DEFAULT 0,
    updated     INTEGER NOT NULL DEFAULT 0,
    missing     INTEGER NOT NULL DEFAULT 0,
    failed      INTEGER NOT NULL DEFAULT 0,
    error       TEXT DEFAULT '',
    started_at  DATETIME,
    finished_at DATETIME,
    created_at  DATETIME
);
CREATE INDEX IF NOT EXISTS idx_scan_jobs_lib ON scan_jobs(library_id, created_at DESC);

CREATE TABLE IF NOT EXISTS scan_logs (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    job_id     INTEGER NOT NULL,
    level      INTEGER NOT NULL DEFAULT 1,  -- 1=info 2=warn 3=error
    path       TEXT DEFAULT '',
    message    TEXT DEFAULT '',
    created_at DATETIME
);
CREATE INDEX IF NOT EXISTS idx_scan_logs_job ON scan_logs(job_id, level);

-- ===================== AI（预留 v0.7.0） =====================
CREATE TABLE IF NOT EXISTS faces (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    media_id    INTEGER NOT NULL,
    cluster_id  INTEGER,
    bbox_json   TEXT DEFAULT '',
    score       REAL NOT NULL DEFAULT 0,
    embedding   BLOB,
    created_at  DATETIME
);
CREATE INDEX IF NOT EXISTS idx_faces_media ON faces(media_id);
CREATE INDEX IF NOT EXISTS idx_faces_cluster ON faces(cluster_id);

CREATE TABLE IF NOT EXISTS face_clusters (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id       INTEGER NOT NULL DEFAULT 0,
    name          TEXT DEFAULT '',
    cover_face_id INTEGER,
    face_count    INTEGER NOT NULL DEFAULT 0,
    status        INTEGER NOT NULL DEFAULT 0,  -- 0=未命名 1=已命名 2=已忽略
    created_at    DATETIME,
    updated_at    DATETIME
);

CREATE TABLE IF NOT EXISTS media_scenes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    media_id    INTEGER NOT NULL,
    scene_label TEXT NOT NULL DEFAULT '',
    score       REAL NOT NULL DEFAULT 0,
    created_at  DATETIME
);
CREATE INDEX IF NOT EXISTS idx_scenes_media ON media_scenes(media_id);
CREATE INDEX IF NOT EXISTS idx_scenes_label ON media_scenes(scene_label);

-- ===================== 迁移版本 =====================
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    TEXT PRIMARY KEY,
    applied_at DATETIME
);
INSERT OR IGNORE INTO schema_migrations(version, applied_at) VALUES ('001_init', CURRENT_TIMESTAMP);
