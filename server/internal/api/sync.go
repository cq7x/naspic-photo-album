package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"github.com/naspic/naspic/internal/model"
)

// registerDevice 手机端注册/上报设备（APP 首次启动与每次同步前调用）
func (s *Server) registerDevice(c *gin.Context) {
	var req struct {
		DeviceUUID string `json:"device_uuid"`
		DeviceName string `json:"device_name"`
		Platform   int8   `json:"platform"` // 1=Android 2=iOS
		AppVersion string `json:"app_version"`
		LANIP      string `json:"lan_ip"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.DeviceUUID == "" {
		fail(c, http.StatusBadRequest, "device_uuid 必填")
		return
	}
	now := time.Now()
	var dev model.SyncDevice
	err := s.db.Where("device_uuid = ?", req.DeviceUUID).First(&dev).Error
	if err != nil {
		dev = model.SyncDevice{
			UserID: CurrentUserID(c), DeviceUUID: req.DeviceUUID,
			DeviceName: req.DeviceName, Platform: req.Platform,
			AppVersion: req.AppVersion, Status: 1,
			CreatedAt: &now, UpdatedAt: &now,
		}
		if req.Platform == 0 {
			dev.Platform = 1
		}
		if err := s.db.Create(&dev).Error; err != nil {
			fail(c, 500, err.Error())
			return
		}
	} else {
		updates := map[string]any{
			"device_name": req.DeviceName, "app_version": req.AppVersion,
			"last_ip": c.ClientIP(), "last_seen_at": now, "updated_at": now,
		}
		if req.LANIP != "" {
			updates["last_lan_ip"] = req.LANIP
		}
		if req.Platform > 0 {
			updates["platform"] = req.Platform
		}
		if err := s.db.Model(&model.SyncDevice{}).Where("id = ?", dev.ID).Updates(updates).Error; err != nil {
			fail(c, 500, err.Error())
			return
		}
	}
	ok(c, dev)
}

func (s *Server) listDevices(c *gin.Context) {
	uid := CurrentUserID(c)
	q := s.db.Order("last_seen_at DESC")
	if !isAdminUser(s.db, uid) {
		q = q.Where("user_id = ?", uid)
	}
	var list []model.SyncDevice
	if err := q.Find(&list).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, list)
}

// listSyncTasks 同步任务列表（可按设备过滤）
func (s *Server) listSyncTasks(c *gin.Context) {
	deviceID, _ := strconv.ParseInt(c.Query("device_id"), 10, 64)
	q := s.db.Order("id DESC")
	if deviceID > 0 {
		q = q.Where("device_id = ?", deviceID)
	}
	var list []model.SyncTask
	if err := q.Find(&list).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, list)
}

type upsertSyncTaskReq struct {
	ID              int64   `json:"id"`
	DeviceID        int64   `json:"device_id"`
	TargetLibraryID int64   `json:"target_library_id"`
	FolderURI       string  `json:"folder_uri"`
	FolderPath      string  `json:"folder_path"`
	FolderType      int8    `json:"folder_type"` // 1=系统相册 2=自定义文件夹
	Enabled         *bool   `json:"enabled"`
	WifiOnly        *bool   `json:"wifi_only"`
	UploadOriginal  *bool   `json:"upload_original"`
	IncludeSubdir   *bool   `json:"include_subdir"`
	FileTypes       []string `json:"file_types"`
	SyncCursor      string  `json:"sync_cursor"`
	Status          *int8   `json:"status"`
	TotalCount      *int    `json:"total_count"`
	SyncedCount     *int    `json:"synced_count"`
	FailedCount     *int    `json:"failed_count"`
	SkippedCount    *int    `json:"skipped_count"`
}

// upsertSyncTask 新建或更新手机文件夹同步任务（APP 每次改动配置后上报）
func (s *Server) upsertSyncTask(c *gin.Context) {
	var req upsertSyncTaskReq
	if err := c.ShouldBindJSON(&req); err != nil {
		fail(c, http.StatusBadRequest, "参数错误")
		return
	}
	if req.DeviceID == 0 || req.FolderURI == "" {
		fail(c, http.StatusBadRequest, "device_id / folder_uri 必填")
		return
	}
	now := time.Now()
	var task model.SyncTask
	err := s.db.Where("device_id = ? AND folder_uri = ?", req.DeviceID, req.FolderURI).First(&task).Error
	if err != nil {
		task = model.SyncTask{
			DeviceID: req.DeviceID, UserID: CurrentUserID(c),
			TargetLibraryID: req.TargetLibraryID, FolderURI: req.FolderURI,
			FolderPath: req.FolderPath, FolderType: firstNonZero8(req.FolderType, 2),
			Enabled: 1, WifiOnly: 1, UploadOriginal: 1, IncludeSubdir: 1,
			FileTypes: `["image","video"]`, Status: model.SyncIdle,
			CreatedAt: &now, UpdatedAt: &now,
		}
		applyTaskPatch(&task, &req)
		if err := s.db.Create(&task).Error; err != nil {
			fail(c, 500, err.Error())
			return
		}
		ok(c, task)
		return
	}
	applyTaskPatch(&task, &req)
	task.UpdatedAt = &now
	if err := s.db.Model(&model.SyncTask{}).Where("id = ?", task.ID).
		Updates(map[string]any{
			"target_library_id": task.TargetLibraryID, "folder_path": task.FolderPath,
			"enabled": task.Enabled, "wifi_only": task.WifiOnly,
			"upload_original": task.UploadOriginal, "include_subdir": task.IncludeSubdir,
			"file_types": task.FileTypes, "sync_cursor": task.SyncCursor,
			"status": task.Status, "total_count": task.TotalCount,
			"synced_count": task.SyncedCount, "failed_count": task.FailedCount,
			"skipped_count": task.SkippedCount, "last_sync_at": task.LastSyncAt,
			"updated_at": now,
		}).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, task)
}

func (s *Server) deleteSyncTask(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := s.db.Delete(&model.SyncTask{}, id).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	s.db.Where("task_id = ?", id).Delete(&model.SyncRecord{})
	ok(c, gin.H{"id": id})
}

// listSyncRecords 单文件同步记录（供 APP 查询失败列表 / 断点续传状态）
func (s *Server) listSyncRecords(c *gin.Context) {
	taskID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	state, hasState := c.GetQuery("state")
	q := s.db.Where("task_id = ?", taskID)
	if hasState {
		if st, err := strconv.Atoi(state); err == nil {
			q = q.Where("state = ?", st)
		}
	}
	var list []model.SyncRecord
	if err := q.Order("id DESC").Limit(200).Find(&list).Error; err != nil {
		fail(c, 500, err.Error())
		return
	}
	ok(c, list)
}

// ---------- 工具 ----------

func applyTaskPatch(t *model.SyncTask, r *upsertSyncTaskReq) {
	if r.TargetLibraryID > 0 {
		t.TargetLibraryID = r.TargetLibraryID
	}
	if r.FolderPath != "" {
		t.FolderPath = r.FolderPath
	}
	if r.Enabled != nil {
		t.Enabled = boolToInt8(*r.Enabled)
	}
	if r.WifiOnly != nil {
		t.WifiOnly = boolToInt8(*r.WifiOnly)
	}
	if r.UploadOriginal != nil {
		t.UploadOriginal = boolToInt8(*r.UploadOriginal)
	}
	if r.IncludeSubdir != nil {
		t.IncludeSubdir = boolToInt8(*r.IncludeSubdir)
	}
	if len(r.FileTypes) > 0 {
		t.FileTypes = toJSONArray(r.FileTypes)
	}
	if r.SyncCursor != "" {
		t.SyncCursor = r.SyncCursor
	}
	if r.Status != nil {
		t.Status = *r.Status
	}
	if r.TotalCount != nil {
		t.TotalCount = *r.TotalCount
	}
	if r.SyncedCount != nil {
		t.SyncedCount = *r.SyncedCount
	}
	if r.FailedCount != nil {
		t.FailedCount = *r.FailedCount
	}
	if r.SkippedCount != nil {
		t.SkippedCount = *r.SkippedCount
	}
	now := time.Now()
	t.LastSyncAt = &now
}

func firstNonZero8(v int8, def int8) int8 {
	if v != 0 {
		return v
	}
	return def
}

// isAdminUser 判断是否为管理员
func isAdminUser(db *gorm.DB, uid int64) bool {
	if uid == 0 {
		return false
	}
	var u model.User
	if err := db.Select("role").First(&u, uid).Error; err != nil {
		return false
	}
	return u.Role == 1
}

// toJSONArray 将字符串数组序列化为 JSON 字符串
func toJSONArray(s []string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return "[]"
	}
	return string(b)
}
