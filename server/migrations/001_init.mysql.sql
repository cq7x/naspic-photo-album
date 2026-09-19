-- Naspic 私有云相册 初始迁移（MySQL 5.7+ / 8.0 推荐）
-- 版本: v0.2.0  字符集: utf8mb4
-- 注意: 联合唯一键中包含长文本时改用 hash 列，避免 3072 bytes 索引长度限制

SET NAMES utf8mb4;

-- ===================== 用户与群组 =====================
CREATE TABLE IF NOT EXISTS users (
    id             BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
    username       VARCHAR(64)  NOT NULL,
    password_hash  VARCHAR(255) NOT NULL,
    nickname       VARCHAR(64)  DEFAULT '',
    avatar         VARCHAR(512) DEFAULT '',
    role           TINYINT      NOT NULL DEFAULT 2,
    status         TINYINT      NOT NULL DEFAULT 1,
    last_login_at  DATETIME     DEFAULT NULL,
    created_at     DATETIME     DEFAULT NULL,
    updated_at     DATETIME     DEFAULT NULL,
    deleted_at     DATETIME     DEFAULT NULL,
    UNIQUE KEY uk_username (username),
    KEY idx_owner (owner_id_placeholder)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
-- 修正：MySQL 不允许引用不存在列，单独建索引
ALTER TABLE users DROP INDEX idx_owner, ADD INDEX idx_users_deleted (deleted_at);

CREATE TABLE IF NOT EXISTS `groups` (
    id         BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name       VARCHAR(64) NOT NULL,
    remark     VARCHAR(255) DEFAULT '',
    created_at DATETIME    DEFAULT NULL,
    updated_at DATETIME    DEFAULT NULL,
    deleted_at DATETIME    DEFAULT NULL,
    UNIQUE KEY uk_group_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS user_groups (
    user_id  BIGINT NOT NULL,
    group_id BIGINT NOT NULL,
    PRIMARY KEY (user_id, group_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ===================== 相册库 / 挂载目录 =====================
CREATE TABLE IF NOT EXISTS libraries (
    id                 BIGINT        NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name               VARCHAR(128)  NOT NULL,
    type               TINYINT       NOT NULL,
    owner_id           BIGINT        NOT NULL DEFAULT 0,
    storage_root       VARCHAR(1024) DEFAULT '',
    quota_bytes        BIGINT        NOT NULL DEFAULT 0,
    used_bytes         BIGINT        NOT NULL DEFAULT 0,
    default_visibility TINYINT       NOT NULL DEFAULT 1,
    status             TINYINT       NOT NULL DEFAULT 1,
    media_count        INT           NOT NULL DEFAULT 0,
    created_at         DATETIME      DEFAULT NULL,
    updated_at         DATETIME      DEFAULT NULL,
    deleted_at         DATETIME      DEFAULT NULL,
    KEY idx_libraries_owner (owner_id, deleted_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS mount_dirs (
    id                BIGINT        NOT NULL AUTO_INCREMENT PRIMARY KEY,
    library_id        BIGINT        NOT NULL,
    host_path         VARCHAR(1024) NOT NULL,
    mode              TINYINT       NOT NULL DEFAULT 1,
    allow_delete      TINYINT       NOT NULL DEFAULT 0,
    watch_mode        TINYINT       NOT NULL DEFAULT 2,
    poll_interval_sec INT           NOT NULL DEFAULT 300,
    recursive         TINYINT       NOT NULL DEFAULT 1,
    ignore_rules      TEXT,
    include_exts      TEXT,
    scan_status       TINYINT       NOT NULL DEFAULT 1,
    last_scan_at      DATETIME      DEFAULT NULL,
    last_scan_cost_ms INT           NOT NULL DEFAULT 0,
    file_count        INT           NOT NULL DEFAULT 0,
    error_message     VARCHAR(1024) DEFAULT '',
    created_at        DATETIME      DEFAULT NULL,
    updated_at        DATETIME      DEFAULT NULL,
    deleted_at        DATETIME      DEFAULT NULL,
    UNIQUE KEY uk_mount_lib (library_id),
    KEY idx_mount_dirs_status (scan_status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ===================== 媒体元数据（核心大表） =====================
CREATE TABLE IF NOT EXISTS media_files (
    id                     BIGINT        NOT NULL AUTO_INCREMENT PRIMARY KEY,
    library_id             BIGINT        NOT NULL,
    source_type            TINYINT       NOT NULL,
    relative_path          VARCHAR(1024) NOT NULL,
    filename               VARCHAR(255)  NOT NULL DEFAULT '',
    ext                    VARCHAR(16)   NOT NULL DEFAULT '',
    mime                   VARCHAR(64)   NOT NULL DEFAULT '',
    size_bytes             BIGINT        NOT NULL DEFAULT 0,
    hash                   CHAR(64)      NOT NULL DEFAULT '',
    hash_algo              VARCHAR(16)   NOT NULL DEFAULT 'sha256',
    media_type             TINYINT       NOT NULL DEFAULT 1,
    width                  INT           NOT NULL DEFAULT 0,
    height                 INT           NOT NULL DEFAULT 0,
    duration_ms            INT           NOT NULL DEFAULT 0,
    orientation            SMALLINT      NOT NULL DEFAULT 0,
    taken_at               DATETIME      DEFAULT NULL,
    taken_at_source        TINYINT       NOT NULL DEFAULT 2,
    camera_make            VARCHAR(128)  DEFAULT '',
    camera_model           VARCHAR(128)  DEFAULT '',
    gps_lat                DOUBLE        DEFAULT NULL,
    gps_lon                DOUBLE        DEFAULT NULL,
    exif_json              TEXT,
    index_status           TINYINT       NOT NULL DEFAULT 1,
    thumb_status           TINYINT       NOT NULL DEFAULT 0,
    ai_status              TINYINT       NOT NULL DEFAULT 0,
    uploaded_by_device_id  BIGINT        DEFAULT NULL,
    original_local_path    VARCHAR(1024) DEFAULT '',
    created_at             DATETIME      DEFAULT NULL,
    updated_at             DATETIME      DEFAULT NULL,
    deleted_at             DATETIME      DEFAULT NULL,
    UNIQUE KEY uk_media_lib_path (library_id, relative_path),
    KEY idx_media_lib_taken  (library_id, taken_at, id),
    KEY idx_media_hash       (hash),
    KEY idx_media_lib_status (library_id, index_status, media_type),
    KEY idx_media_thumb      (thumb_status, library_id),
    KEY idx_media_ai         (ai_status, library_id),
    KEY idx_media_device     (uploaded_by_device_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ===================== 标签 =====================
CREATE TABLE IF NOT EXISTS tags (
    id         BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    name       VARCHAR(64) NOT NULL,
    kind       TINYINT     NOT NULL DEFAULT 1,
    color      VARCHAR(16) DEFAULT '',
    created_at DATETIME    DEFAULT NULL,
    UNIQUE KEY uk_tag_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS media_tags (
    media_id BIGINT NOT NULL,
    tag_id   BIGINT NOT NULL,
    PRIMARY KEY (media_id, tag_id),
    KEY idx_media_tags_tag (tag_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ===================== 缩略图缓存 =====================
CREATE TABLE IF NOT EXISTS thumbnails (
    id          BIGINT        NOT NULL AUTO_INCREMENT PRIMARY KEY,
    media_id    BIGINT        NOT NULL,
    size_key    VARCHAR(16)   NOT NULL,
    rel_path    VARCHAR(1024) NOT NULL,
    width       INT           NOT NULL DEFAULT 0,
    height      INT           NOT NULL DEFAULT 0,
    bytes       INT           NOT NULL DEFAULT 0,
    source_hash CHAR(64)      NOT NULL DEFAULT '',
    status      TINYINT       NOT NULL DEFAULT 0,
    created_at  DATETIME      DEFAULT NULL,
    updated_at  DATETIME      DEFAULT NULL,
    UNIQUE KEY uk_thumb_media_size (media_id, size_key),
    KEY idx_thumb_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ===================== 权限 =====================
CREATE TABLE IF NOT EXISTS permissions (
    id            BIGINT   NOT NULL AUTO_INCREMENT PRIMARY KEY,
    subject_type  TINYINT  NOT NULL,
    subject_id    BIGINT   NOT NULL,
    resource_type TINYINT  NOT NULL,
    resource_id   BIGINT   NOT NULL,
    permission    TINYINT  NOT NULL DEFAULT 1,
    created_at    DATETIME DEFAULT NULL,
    UNIQUE KEY uk_perm (subject_type, subject_id, resource_type, resource_id),
    KEY idx_perm_res (resource_type, resource_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ===================== 手机同步 =====================
CREATE TABLE IF NOT EXISTS sync_devices (
    id           BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
    user_id      BIGINT       NOT NULL,
    device_uuid  VARCHAR(64)  NOT NULL,
    device_name  VARCHAR(128) DEFAULT '',
    platform     TINYINT      NOT NULL DEFAULT 1,
    app_version  VARCHAR(32)  DEFAULT '',
    last_ip      VARCHAR(64)  DEFAULT '',
    last_lan_ip  VARCHAR(64)  DEFAULT '',
    last_seen_at DATETIME     DEFAULT NULL,
    status       TINYINT      NOT NULL DEFAULT 1,
    created_at   DATETIME     DEFAULT NULL,
    updated_at   DATETIME     DEFAULT NULL,
    UNIQUE KEY uk_device_uuid (device_uuid)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sync_tasks (
    id                BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
    device_id         BIGINT       NOT NULL,
    user_id           BIGINT       NOT NULL DEFAULT 0,
    target_library_id BIGINT       NOT NULL,
    folder_uri        VARCHAR(512) NOT NULL,
    folder_path       VARCHAR(512) DEFAULT '',
    folder_type       TINYINT      NOT NULL DEFAULT 2,
    enabled           TINYINT      NOT NULL DEFAULT 1,
    wifi_only         TINYINT      NOT NULL DEFAULT 1,
    upload_original   TINYINT      NOT NULL DEFAULT 1,
    include_subdir    TINYINT      NOT NULL DEFAULT 1,
    file_types        VARCHAR(128) DEFAULT '["image","video"]',
    sync_cursor       VARCHAR(128) DEFAULT '',
    last_sync_at      DATETIME     DEFAULT NULL,
    status            TINYINT      NOT NULL DEFAULT 1,
    total_count       INT          NOT NULL DEFAULT 0,
    synced_count      INT          NOT NULL DEFAULT 0,
    failed_count      INT          NOT NULL DEFAULT 0,
    skipped_count     INT          NOT NULL DEFAULT 0,
    last_error        VARCHAR(512) DEFAULT '',
    created_at        DATETIME     DEFAULT NULL,
    updated_at        DATETIME     DEFAULT NULL,
    UNIQUE KEY uk_sync_task_folder (device_id, folder_uri),
    KEY idx_sync_task_device (device_id, enabled)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS sync_records (
    id                BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
    task_id           BIGINT       NOT NULL,
    device_id         BIGINT       NOT NULL DEFAULT 0,
    local_id          VARCHAR(128) DEFAULT '',
    local_path        TEXT,
    local_path_hash   CHAR(64)     NOT NULL DEFAULT '',
    file_hash         CHAR(64)     DEFAULT '',
    size_bytes        BIGINT       NOT NULL DEFAULT 0,
    local_mtime       BIGINT       NOT NULL DEFAULT 0,
    media_id          BIGINT       DEFAULT NULL,
    state             TINYINT      NOT NULL DEFAULT 0,
    upload_session_id VARCHAR(64)  DEFAULT '',
    chunk_total       INT          NOT NULL DEFAULT 0,
    chunk_done        INT          NOT NULL DEFAULT 0,
    retry_count       INT          NOT NULL DEFAULT 0,
    last_error        VARCHAR(512) DEFAULT '',
    created_at        DATETIME     DEFAULT NULL,
    updated_at        DATETIME     DEFAULT NULL,
    UNIQUE KEY uk_sync_record (task_id, local_path_hash),
    KEY idx_sync_record_state (task_id, state)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ===================== 分片上传会话 =====================
CREATE TABLE IF NOT EXISTS upload_sessions (
    id              BIGINT        NOT NULL AUTO_INCREMENT PRIMARY KEY,
    session_id      VARCHAR(64)   NOT NULL,
    user_id         BIGINT        NOT NULL DEFAULT 0,
    device_id       BIGINT        DEFAULT NULL,
    library_id      BIGINT        NOT NULL,
    file_hash       CHAR(64)      NOT NULL DEFAULT '',
    size_bytes      BIGINT        NOT NULL DEFAULT 0,
    chunk_size      INT           NOT NULL DEFAULT 0,
    chunk_total     INT           NOT NULL DEFAULT 0,
    uploaded_chunks TEXT,
    tmp_dir         VARCHAR(512)  DEFAULT '',
    relative_path   VARCHAR(1024) DEFAULT '',
    status          TINYINT       NOT NULL DEFAULT 0,
    expires_at      DATETIME      DEFAULT NULL,
    created_at      DATETIME      DEFAULT NULL,
    updated_at      DATETIME      DEFAULT NULL,
    UNIQUE KEY uk_session_id (session_id),
    KEY idx_upload_status (status, expires_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ===================== 扫描任务与日志 =====================
CREATE TABLE IF NOT EXISTS scan_jobs (
    id          BIGINT       NOT NULL AUTO_INCREMENT PRIMARY KEY,
    library_id  BIGINT       NOT NULL,
    type        TINYINT      NOT NULL DEFAULT 1,
    trigger_src TINYINT      NOT NULL DEFAULT 1,
    status      TINYINT      NOT NULL DEFAULT 1,
    total       INT          NOT NULL DEFAULT 0,
    scanned     INT          NOT NULL DEFAULT 0,
    added       INT          NOT NULL DEFAULT 0,
    updated     INT          NOT NULL DEFAULT 0,
    missing     INT          NOT NULL DEFAULT 0,
    failed      INT          NOT NULL DEFAULT 0,
    error       VARCHAR(512) DEFAULT '',
    started_at  DATETIME     DEFAULT NULL,
    finished_at DATETIME     DEFAULT NULL,
    created_at  DATETIME     DEFAULT NULL,
    KEY idx_scan_jobs_lib (library_id, created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS scan_logs (
    id         BIGINT        NOT NULL AUTO_INCREMENT PRIMARY KEY,
    job_id     BIGINT        NOT NULL,
    level      TINYINT       NOT NULL DEFAULT 1,
    path       VARCHAR(1024) DEFAULT '',
    message    VARCHAR(1024) DEFAULT '',
    created_at DATETIME      DEFAULT NULL,
    KEY idx_scan_logs_job (job_id, level)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ===================== AI（预留 v0.7.0） =====================
CREATE TABLE IF NOT EXISTS faces (
    id         BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    media_id   BIGINT      NOT NULL,
    cluster_id BIGINT      DEFAULT NULL,
    bbox_json  VARCHAR(256) DEFAULT '',
    score      FLOAT       NOT NULL DEFAULT 0,
    embedding  BLOB,
    created_at DATETIME    DEFAULT NULL,
    KEY idx_faces_media (media_id),
    KEY idx_faces_cluster (cluster_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS face_clusters (
    id            BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    user_id       BIGINT      NOT NULL DEFAULT 0,
    name          VARCHAR(64) DEFAULT '',
    cover_face_id BIGINT      DEFAULT NULL,
    face_count    INT         NOT NULL DEFAULT 0,
    status        TINYINT     NOT NULL DEFAULT 0,
    created_at    DATETIME    DEFAULT NULL,
    updated_at    DATETIME    DEFAULT NULL
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IF NOT EXISTS media_scenes (
    id          BIGINT      NOT NULL AUTO_INCREMENT PRIMARY KEY,
    media_id    BIGINT      NOT NULL,
    scene_label VARCHAR(64) NOT NULL DEFAULT '',
    score       FLOAT       NOT NULL DEFAULT 0,
    created_at  DATETIME    DEFAULT NULL,
    KEY idx_scenes_media (media_id),
    KEY idx_scenes_label (scene_label)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- ===================== 迁移版本 =====================
CREATE TABLE IF NOT EXISTS schema_migrations (
    version    VARCHAR(64) NOT NULL PRIMARY KEY,
    applied_at DATETIME    DEFAULT NULL
);
INSERT IGNORE INTO schema_migrations(version, applied_at) VALUES ('001_init', NOW());
