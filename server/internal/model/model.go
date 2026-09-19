// Package model 定义与 migrations/001_init 一一对应的 GORM 模型。
// 约定：所有时间字段统一存 UTC；字段注释与 SQL 迁移保持一致。
package model

import (
	"time"
)

// ---------- 枚举常量 ----------

// 相册库类型
const (
	LibraryTypeManaged = 1 // 托管存储
	LibraryTypeMounted = 2 // 挂载外部目录
)

// 媒体来源类型
const (
	SourceManaged = 1 // 托管存储文件（真实拥有）
	SourceMounted = 2 // 挂载目录索引文件（不拥有原图）
)

// 媒体类型
const (
	MediaImage = 1
	MediaVideo = 2
	MediaOther = 3
)

// 挂载目录模式
const (
	MountModeReadOnly  = 1 // 只读（默认，强制）
	MountModeReadWrite = 2 // 读写（高危）
)

// 挂载监听模式
const (
	WatchInotify = 1
	WatchPoll    = 2
)

// 索引状态
const (
	IndexNormal  = 1 // 正常
	IndexMissing = 2 // 源文件缺失（挂载目录被外部删除）
	IndexBroken  = 3 // 损坏无法解析
)

// 同步任务状态
const (
	SyncIdle     = 1
	SyncScanning = 2
	SyncRunning  = 3
	SyncPaused   = 4
	SyncError    = 5
)

// 单文件同步状态
const (
	RecordPending  = 0
	RecordHashing  = 1
	RecordUploading = 2
	RecordDone     = 3
	RecordFailed   = 4
	RecordSkipped  = 5
	RecordGiveUp   = 6
)

// ---------- 基础 ----------

type BaseModel struct {
	ID        int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
}

// ---------- 用户与群组 ----------

type User struct {
	ID        int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
	Username    string     `gorm:"size:64;uniqueIndex;not null" json:"username"`
	PasswordHash string    `gorm:"size:255;not null" json:"-"`
	Nickname    string     `gorm:"size:64" json:"nickname"`
	Avatar      string     `gorm:"size:512" json:"avatar"`
	Role        int8       `gorm:"default:2" json:"role"`   // 1=管理员 2=普通
	Status      int8       `gorm:"default:1" json:"status"` // 1=正常 2=禁用
	LastLoginAt *time.Time `json:"last_login_at"`
	DeletedAt   *time.Time `gorm:"index" json:"-"`
}

func (User) TableName() string { return "users" }

type Group struct {
	ID        int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
	Name      string     `gorm:"size:64;uniqueIndex;not null" json:"name"`
	Remark    string     `gorm:"size:255" json:"remark"`
	DeletedAt *time.Time `gorm:"index" json:"-"`
}

func (Group) TableName() string { return "groups" }

type UserGroup struct {
	UserID  int64 `gorm:"primaryKey" json:"user_id"`
	GroupID int64 `gorm:"primaryKey" json:"group_id"`
}

func (UserGroup) TableName() string { return "user_groups" }

// ---------- 相册库 / 挂载目录 ----------

type Library struct {
	ID        int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
	Name              string     `gorm:"size:128;not null" json:"name"`
	Type              int8       `gorm:"not null" json:"type"` // LibraryTypeManaged / Mounted
	OwnerID           int64      `gorm:"not null;default:0" json:"owner_id"`
	StorageRoot       string     `gorm:"size:1024" json:"storage_root"`
	QuotaBytes        int64      `json:"quota_bytes"`
	UsedBytes         int64      `json:"used_bytes"`
	DefaultVisibility int8       `gorm:"default:1" json:"default_visibility"`
	Status            int8       `gorm:"default:1" json:"status"` // 1=正常 2=暂停扫描 3=异常
	MediaCount        int        `json:"media_count"`
	DeletedAt         *time.Time `gorm:"index" json:"-"`
}

func (Library) TableName() string { return "libraries" }

type MountDir struct {
	ID        int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
	LibraryID       int64      `gorm:"uniqueIndex;not null" json:"library_id"`
	HostPath        string     `gorm:"size:1024;not null" json:"host_path"`
	Mode            int8       `gorm:"not null;default:1" json:"mode"` // 1=只读 2=读写
	AllowDelete     int8       `gorm:"not null;default:0" json:"allow_delete"`
	WatchMode       int8       `gorm:"not null;default:2" json:"watch_mode"` // 1=inotify 2=轮询
	PollIntervalSec int        `gorm:"not null;default:300" json:"poll_interval_sec"`
	Recursive       int8       `gorm:"not null;default:1" json:"recursive"`
	IgnoreRules     string     `gorm:"type:text" json:"ignore_rules"` // JSON array
	IncludeExts     string     `gorm:"type:text" json:"include_exts"` // JSON array
	ScanStatus      int8       `gorm:"not null;default:1" json:"scan_status"`
	LastScanAt      *time.Time `json:"last_scan_at"`
	LastScanCostMs  int        `json:"last_scan_cost_ms"`
	FileCount       int        `json:"file_count"`
	ErrorMessage    string     `gorm:"size:1024" json:"error_message"`
	DeletedAt       *time.Time `gorm:"index" json:"-"`
}

func (MountDir) TableName() string { return "mount_dirs" }

// ---------- 媒体元数据（核心大表） ----------

type MediaFile struct {
	ID        int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
	// 复合唯一索引 (library_id, relative_path)：scanner 的 ON CONFLICT upsert 依赖它，
	// 必须与 migrations/001_init 中的 uk_media_lib_path 保持一致
	LibraryID          int64      `gorm:"not null;uniqueIndex:uk_media_lib_path,priority:1;index:idx_media_lib_taken;index:idx_media_lib_status" json:"library_id"`
	SourceType         int8       `gorm:"not null" json:"source_type"` // SourceManaged / SourceMounted
	// 512 而不是 1024：MySQL InnoDB 单列索引上限 3072 字节，utf8mb4 按 4 字节/字符算，
	// 1024 会让 (library_id, relative_path) 这个唯一索引直接建不出来（error 1071）。
	// 512 字符的相对路径对任何实际目录都够用。
	RelativePath       string     `gorm:"size:512;not null;uniqueIndex:uk_media_lib_path,priority:2" json:"relative_path"`
	Filename           string     `gorm:"size:255" json:"filename"`
	Ext                string     `gorm:"size:16" json:"ext"`
	Mime               string     `gorm:"size:64" json:"mime"`
	SizeBytes          int64      `json:"size_bytes"`
	Hash               string     `gorm:"size:64;index:idx_media_hash" json:"hash"`
	HashAlgo           string     `gorm:"size:16;default:sha256" json:"hash_algo"`
	MediaType          int8       `json:"media_type"`
	Width              int        `json:"width"`
	Height             int        `json:"height"`
	DurationMs         int        `json:"duration_ms"`
	Orientation        int16      `json:"orientation"`
	TakenAt            *time.Time `gorm:"index:idx_media_lib_taken" json:"taken_at"`
	TakenAtSource      int8       `gorm:"default:2" json:"taken_at_source"`
	CameraMake         string     `gorm:"size:128" json:"camera_make"`
	CameraModel        string     `gorm:"size:128" json:"camera_model"`
	GPSLat             *float64   `json:"gps_lat"`
	GPSLon             *float64   `json:"gps_lon"`
	ExifJSON           string     `gorm:"type:text" json:"-"`
	IndexStatus        int8       `gorm:"not null;default:1;index:idx_media_lib_status" json:"index_status"`
	ThumbStatus        int8       `gorm:"not null;default:0;index:idx_media_thumb" json:"thumb_status"`
	AIStatus           int8       `gorm:"not null;default:0;index:idx_media_ai" json:"ai_status"`
	// Favorite 收藏标记（v1.1.0 起）。0=未收藏 1=已收藏。
	// AutoMigrate 会自动补列，无需手工迁移
	Favorite           int8       `gorm:"not null;default:0;index:idx_media_fav" json:"favorite"`
	UploadedByDeviceID *int64     `json:"uploaded_by_device_id"`
	OriginalLocalPath  string     `gorm:"size:1024" json:"original_local_path"` // 仅展示，绝不反向操作手机文件
	DeletedAt          *time.Time `gorm:"index" json:"-"`
}

func (MediaFile) TableName() string { return "media_files" }

// ---------- 相册（v1.6.0 起取代「收藏」） ----------

// AlbumRuleType 相册的归类方式
type AlbumRuleType int8

const (
	AlbumRuleManual   AlbumRuleType = 0 // 手动挑照片
	AlbumRuleTime     AlbumRuleType = 1 // 按拍摄时间区间
	AlbumRuleKeyword  AlbumRuleType = 2 // 文件名/路径关键词
	AlbumRuleLibrary  AlbumRuleType = 3 // 整个相册库
	AlbumRuleFavorite AlbumRuleType = 4 // 老收藏数据迁移而来
)

// Album 用户自建相册。手动相册就是往里塞照片；
// 规则相册（时间区间/关键词/库）可以一键「按规则重新匹配」，
// 也支持新扫描入库的照片自动纳入（AutoAdd）。
type Album struct {
	ID        int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	CreatedAt *time.Time `json:"created_at"`
	UpdatedAt *time.Time `json:"updated_at"`
	Name        string        `gorm:"size:128;not null" json:"name"`
	Remark      string        `gorm:"size:512" json:"remark"`
	CoverPath   string        `gorm:"size:1024" json:"cover_path"` // 封面：媒体相对路径，空则自动取最新一张
	RuleType    AlbumRuleType `gorm:"not null;default:0" json:"rule_type"`
	RuleJSON    string        `gorm:"type:text" json:"rule_json"` // {"start":"","end":"","keyword":"","library_id":0,"media_type":0}
	AutoAdd     bool          `gorm:"not null;default:false" json:"auto_add"`
	SortOrder   int           `gorm:"not null;default:0" json:"sort_order"`
	ItemCount   int           `gorm:"not null;default:0" json:"item_count"`
	DeletedAt   *time.Time    `gorm:"index" json:"-"`
}

func (Album) TableName() string { return "albums" }

type AlbumItem struct {
	ID        int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	AlbumID   int64      `gorm:"not null;uniqueIndex:uk_album_media" json:"album_id"`
	MediaID   int64      `gorm:"not null;uniqueIndex:uk_album_media;index:idx_album_items_media" json:"media_id"`
	SortOrder int        `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt *time.Time `json:"created_at"`
}

func (AlbumItem) TableName() string { return "album_items" }

// ---------- 标签 ----------

type Tag struct {
	ID        int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name      string     `gorm:"size:64;uniqueIndex;not null" json:"name"`
	Kind      int8       `gorm:"default:1" json:"kind"` // 1=手动 2=AI 3=地点
	Color     string     `gorm:"size:16" json:"color"`
	CreatedAt *time.Time `json:"created_at"`
}

func (Tag) TableName() string { return "tags" }

type MediaTag struct {
	MediaID int64 `gorm:"primaryKey" json:"media_id"`
	TagID   int64 `gorm:"primaryKey;index:idx_media_tags_tag" json:"tag_id"`
}

func (MediaTag) TableName() string { return "media_tags" }

// ---------- 缩略图缓存 ----------

type Thumbnail struct {
	ID         int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	MediaID    int64      `gorm:"not null;uniqueIndex:uk_thumb_media_size" json:"media_id"`
	SizeKey    string     `gorm:"size:16;not null;uniqueIndex:uk_thumb_media_size" json:"size_key"`
	RelPath    string     `gorm:"size:1024;not null" json:"rel_path"`
	Width      int        `json:"width"`
	Height     int        `json:"height"`
	Bytes      int        `json:"bytes"`
	SourceHash string     `gorm:"size:64" json:"source_hash"`
	Status     int8       `gorm:"not null;default:0;index:idx_thumb_status" json:"status"`
	CreatedAt  *time.Time `json:"created_at"`
	UpdatedAt  *time.Time `json:"updated_at"`
}

func (Thumbnail) TableName() string { return "thumbnails" }

// ---------- 权限 ----------

type Permission struct {
	ID           int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	SubjectType  int8       `gorm:"not null;uniqueIndex:uk_perm;index:idx_perm_res" json:"subject_type"` // 1=用户 2=群组
	SubjectID    int64      `gorm:"not null;uniqueIndex:uk_perm" json:"subject_id"`
	ResourceType int8       `gorm:"not null;uniqueIndex:uk_perm;index:idx_perm_res" json:"resource_type"` // 1=库 2=挂载目录
	ResourceID   int64      `gorm:"not null;uniqueIndex:uk_perm;index:idx_perm_res" json:"resource_id"`
	Permission   int8       `gorm:"not null;default:1" json:"permission"` // 1=只读 2=上传 3=管理
	CreatedAt    *time.Time `json:"created_at"`
}

func (Permission) TableName() string { return "permissions" }

// ---------- 手机同步 ----------

type SyncDevice struct {
	ID          int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      int64      `gorm:"not null" json:"user_id"`
	DeviceUUID  string     `gorm:"size:64;uniqueIndex;not null" json:"device_uuid"`
	DeviceName  string     `gorm:"size:128" json:"device_name"`
	Platform    int8       `gorm:"default:1" json:"platform"` // 1=Android 2=iOS
	AppVersion  string     `gorm:"size:32" json:"app_version"`
	LastIP      string     `gorm:"size:64" json:"last_ip"`
	LastLANIP   string     `gorm:"size:64" json:"last_lan_ip"`
	LastSeenAt  *time.Time `json:"last_seen_at"`
	Status      int8       `gorm:"default:1" json:"status"` // 1=正常 2=已吊销
	CreatedAt   *time.Time `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at"`
}

func (SyncDevice) TableName() string { return "sync_devices" }

type SyncTask struct {
	ID              int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	DeviceID        int64      `gorm:"not null;uniqueIndex:uk_sync_task_folder;index:idx_sync_task_device" json:"device_id"`
	UserID          int64      `json:"user_id"`
	TargetLibraryID int64      `gorm:"not null" json:"target_library_id"`
	FolderURI       string     `gorm:"size:512;not null;uniqueIndex:uk_sync_task_folder" json:"folder_uri"`
	FolderPath      string     `gorm:"size:512" json:"folder_path"`
	FolderType      int8       `gorm:"default:2" json:"folder_type"` // 1=系统相册 2=自定义
	Enabled         int8       `gorm:"default:1;index:idx_sync_task_device" json:"enabled"`
	WifiOnly        int8       `gorm:"default:1" json:"wifi_only"`
	UploadOriginal  int8       `gorm:"default:1" json:"upload_original"`
	IncludeSubdir   int8       `gorm:"default:1" json:"include_subdir"`
	FileTypes       string     `gorm:"size:128;default:[\"image\",\"video\"]" json:"file_types"`
	SyncCursor      string     `gorm:"size:128" json:"sync_cursor"`
	LastSyncAt      *time.Time `json:"last_sync_at"`
	Status          int8       `gorm:"default:1" json:"status"`
	TotalCount      int        `json:"total_count"`
	SyncedCount     int        `json:"synced_count"`
	FailedCount     int        `json:"failed_count"`
	SkippedCount    int        `json:"skipped_count"`
	LastError       string     `gorm:"size:512" json:"last_error"`
	CreatedAt       *time.Time `json:"created_at"`
	UpdatedAt       *time.Time `json:"updated_at"`
}

func (SyncTask) TableName() string { return "sync_tasks" }

type SyncRecord struct {
	ID              int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	TaskID          int64      `gorm:"not null;uniqueIndex:uk_sync_record;index:idx_sync_record_state" json:"task_id"`
	DeviceID        int64      `json:"device_id"`
	LocalID         string     `gorm:"size:128" json:"local_id"`
	LocalPath       string     `gorm:"type:text" json:"local_path"`
	LocalPathHash   string     `gorm:"size:64;not null;uniqueIndex:uk_sync_record" json:"local_path_hash"`
	FileHash        string     `gorm:"size:64" json:"file_hash"`
	SizeBytes       int64      `json:"size_bytes"`
	LocalMtime      int64      `json:"local_mtime"`
	MediaID         *int64     `json:"media_id"`
	State           int8       `gorm:"not null;default:0;index:idx_sync_record_state" json:"state"`
	UploadSessionID string     `gorm:"size:64" json:"upload_session_id"`
	ChunkTotal      int        `json:"chunk_total"`
	ChunkDone       int        `json:"chunk_done"`
	RetryCount      int        `json:"retry_count"`
	LastError       string     `gorm:"size:512" json:"last_error"`
	CreatedAt       *time.Time `json:"created_at"`
	UpdatedAt       *time.Time `json:"updated_at"`
}

func (SyncRecord) TableName() string { return "sync_records" }

// ---------- 分片上传会话 ----------

type UploadSession struct {
	ID              int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	SessionID       string     `gorm:"size:64;uniqueIndex;not null" json:"session_id"`
	UserID          int64      `json:"user_id"`
	DeviceID        *int64     `json:"device_id"`
	LibraryID       int64      `gorm:"not null" json:"library_id"`
	FileHash        string     `gorm:"size:64" json:"file_hash"`
	SizeBytes       int64      `json:"size_bytes"`
	ChunkSize       int        `json:"chunk_size"`
	ChunkTotal      int        `json:"chunk_total"`
	UploadedChunks  string     `gorm:"type:text" json:"uploaded_chunks"` // JSON array
	TmpDir          string     `gorm:"size:512" json:"tmp_dir"`
	RelativePath    string     `gorm:"size:1024" json:"relative_path"`
	Status          int8       `gorm:"not null;default:0;index:idx_upload_status" json:"status"`
	ExpiresAt       *time.Time `gorm:"index:idx_upload_status" json:"expires_at"`
	CreatedAt       *time.Time `json:"created_at"`
	UpdatedAt       *time.Time `json:"updated_at"`
}

func (UploadSession) TableName() string { return "upload_sessions" }

// ---------- 扫描任务与日志 ----------

type ScanJob struct {
	ID         int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	LibraryID  int64      `gorm:"not null;index:idx_scan_jobs_lib" json:"library_id"`
	Type       int8       `gorm:"default:1" json:"type"`        // 1=增量 2=全量重建
	TriggerSrc int8       `gorm:"default:1" json:"trigger_src"` // 1=手动 2=定时 3=inotify
	Status     int8       `gorm:"default:1" json:"status"`      // 1=排队 2=运行 3=完成 4=失败 5=取消
	Total      int        `json:"total"`
	Scanned    int        `json:"scanned"`
	Added      int        `json:"added"`
	Updated    int        `json:"updated"`
	Skipped    int        `json:"skipped"` // 命中扫描缓存直接跳过的文件数
	Missing    int        `json:"missing"`
	Failed     int        `json:"failed"`
	Error      string     `gorm:"size:512" json:"error"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	CreatedAt  *time.Time `json:"created_at"`
}

func (ScanJob) TableName() string { return "scan_jobs" }

type ScanLog struct {
	ID        int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	JobID     int64      `gorm:"not null;index:idx_scan_logs_job" json:"job_id"`
	Level     int8       `gorm:"default:1;index:idx_scan_logs_job" json:"level"` // 1=info 2=warn 3=error
	Path      string     `gorm:"size:1024" json:"path"`
	Message   string     `gorm:"size:1024" json:"message"`
	CreatedAt *time.Time `json:"created_at"`
}

func (ScanLog) TableName() string { return "scan_logs" }

// ScanCache 扫描指纹缓存。
// 记录每个文件上一次"确认过"的状态（大小 + 修改时间 + 哈希 + 尺寸/拍摄时间）。
// 下次扫描时若 (size, mod_unix) 与缓存一致，且索引里已有该条记录，
// 就直接跳过——不必重算全文件哈希与重新解析，几万张图的增量扫描可从分钟级降到秒级。
// 缓存只服务于性能，任何时候丢弃都安全（下一轮扫描会重建）。
type ScanCache struct {
	ID         int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	LibraryID  int64      `gorm:"not null;uniqueIndex:uk_scan_cache_lib_path;index:idx_scan_cache_lib" json:"library_id"`
	RelPath    string     `gorm:"size:512;not null;uniqueIndex:uk_scan_cache_lib_path" json:"rel_path"`
	SizeBytes  int64      `gorm:"not null" json:"size_bytes"`
	ModUnix    int64      `gorm:"not null" json:"mod_unix"` // 文件 mtime（Unix 秒）
	Hash       string     `gorm:"size:128" json:"hash"`
	Width      int        `json:"width"`
	Height     int        `json:"height"`
	TakenAt    *time.Time `json:"taken_at"`
	MediaType  int8       `json:"media_type"`
	HitCount   int        `gorm:"not null;default:0" json:"hit_count"` // 累计被跳过的次数，用于展示缓存效果
	CreatedAt  *time.Time `json:"created_at"`
	UpdatedAt  *time.Time `json:"updated_at"`
}

func (ScanCache) TableName() string { return "scan_cache" }

// ---------- AI（预留 v0.7.0） ----------

type Face struct {
	ID        int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	MediaID   int64      `gorm:"not null;index:idx_faces_media" json:"media_id"`
	ClusterID *int64     `gorm:"index:idx_faces_cluster" json:"cluster_id"`
	BBoxJSON  string     `gorm:"size:256" json:"bbox_json"`
	Score     float32    `json:"score"`
	Embedding []byte     `gorm:"type:blob" json:"-"` // 只存特征向量，绝不存图
	CreatedAt *time.Time `json:"created_at"`
}

func (Face) TableName() string { return "faces" }

type FaceCluster struct {
	ID          int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID      int64      `json:"user_id"`
	Name        string     `gorm:"size:64" json:"name"`
	CoverFaceID *int64     `json:"cover_face_id"`
	FaceCount   int        `json:"face_count"`
	Status      int8       `json:"status"`
	CreatedAt   *time.Time `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at"`
}

func (FaceCluster) TableName() string { return "face_clusters" }

type MediaScene struct {
	ID         int64      `gorm:"primaryKey;autoIncrement" json:"id"`
	MediaID    int64      `gorm:"not null;index:idx_scenes_media" json:"media_id"`
	SceneLabel string     `gorm:"size:64;not null;index:idx_scenes_label" json:"scene_label"`
	Score      float32    `json:"score"`
	CreatedAt  *time.Time `json:"created_at"`
}

func (MediaScene) TableName() string { return "media_scenes" }

// ---------- 迁移版本 ----------

type SchemaMigration struct {
	Version    string     `gorm:"primaryKey;size:64" json:"version"`
	AppliedAt  *time.Time `json:"applied_at"`
}

func (SchemaMigration) TableName() string { return "schema_migrations" }
