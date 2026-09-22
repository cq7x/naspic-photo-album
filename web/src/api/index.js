import axios from 'axios'

const http = axios.create({
  baseURL: '/api/v1',
  timeout: 30000,
})

http.interceptors.request.use((cfg) => {
  const token = localStorage.getItem('naspic.token')
  if (token) cfg.headers.Authorization = 'Bearer ' + token
  return cfg
})

http.interceptors.response.use(
  (res) => {
    const body = res.data
    if (body && body.code !== undefined && body.code !== 0) {
      return Promise.reject(new Error(body.msg || '请求失败'))
    }
    return body && body.data !== undefined ? body.data : body
  },
  (err) => {
    // 后端 fail() 统一返回 { code, msg }。必须把 msg 提出来，
    // 否则用户只能看到 axios 的英文 "Request failed with status code 403"，
    // 完全不知道到底是哪条规则拦的。
    const resp = err.response
    if (resp) {
      const body = resp.data
      let msg = body && typeof body === 'object' ? body.msg : ''
      if (!msg) {
        switch (resp.status) {
          case 400: msg = '请求参数有误'; break
          case 401: msg = '登录已过期，请重新登录'; break
          case 403: msg = '没有权限执行该操作'; break
          case 404: msg = '接口不存在（404）'; break
          case 409: msg = '操作冲突，请稍后重试'; break
          case 413: msg = '文件过大，已超出服务限制'; break
          default: msg = resp.status >= 500
            ? '服务器内部错误（' + resp.status + '）'
            : '请求失败（' + resp.status + '）'
        }
      }
      if (resp.status === 401) {
        localStorage.removeItem('naspic.token')
        location.hash = '#/login'
      }
      err.message = msg
      return Promise.reject(err)
    }
    if (err.code === 'ECONNABORTED') {
      err.message = '请求超时，请检查网络或稍后重试'
    } else if (err.message === 'Network Error' || !err.request) {
      err.message = '网络错误，无法连接到服务器'
    }
    return Promise.reject(err)
  }
)

export default http

// ---------- 认证 ----------
export const login = (username, password) =>
  http.post('/auth/login', { username, password })

// ---------- 相册库 / 挂载目录 ----------
export const listLibraries = () => http.get('/libraries')
export const createLibrary = (data) => http.post('/libraries', data)
export const listMountDirs = () => http.get('/mount-dirs')
export const createMountDir = (data) => http.post('/mount-dirs', data)
export const deleteLibrary = (id) => http.delete('/libraries/' + id)

// ---------- 扫描 ----------
// force=true 表示忽略扫描缓存，强制重算哈希与元数据
export const triggerScan = (id, full = false, force = false) =>
  http.post(`/libraries/${id}/scan`, { full, force })
export const listScanJobs = (libraryId) =>
  http.get(`/libraries/${libraryId}/scan/jobs`)
export const scanJobLogs = (jobId) => http.get(`/scan/jobs/${jobId}/logs`)

// ---------- 缓存（缩略图 / 扫描指纹，均可重建） ----------
export const libraryCache = (libraryId) =>
  http.get(`/libraries/${libraryId}/cache`)
export const clearLibraryCache = (libraryId, scope = 'all') =>
  http.post(`/libraries/${libraryId}/cache/clear`, { scope })

// ---------- 媒体 ----------
export const listMedia = (params) => http.get('/media', { params })
export const deleteMedia = (id, physical = false) =>
  http.delete(`/media/${id}`, { params: { physical } })
export const mediaStats = (libraryId) =>
  http.get('/media/stats', { params: { library_id: libraryId || undefined } })
export const getMediaDetail = (id) => http.get(`/media/${id}/detail`)
export const updateMedia = (id, data) => http.patch(`/media/${id}`, data)
export const batchMedia = (ids, action, physical = false) =>
  http.post('/media/batch', { ids, action, physical })

// ---------- 相册（取代「收藏」） ----------
export const listAlbums = () => http.get('/albums')
export const createAlbum = (data) => http.post('/albums', data)
export const updateAlbum = (id, data) => http.patch(`/albums/${id}`, data)
export const deleteAlbum = (id) => http.delete(`/albums/${id}`)
export const albumMedia = (id, params) => http.get(`/albums/${id}/media`, { params })
export const addToAlbum = (id, mediaIds) =>
  http.post(`/albums/${id}/items`, { media_ids: mediaIds })
export const removeFromAlbum = (id, mediaIds) =>
  http.delete(`/albums/${id}/items`, { data: { media_ids: mediaIds } })
export const applyAlbumRule = (id, rule, replace = false) =>
  http.post(`/albums/${id}/apply`, { rule, replace })
export const mediaAlbums = (id) => http.get(`/media/${id}/albums`)

// ---------- 网页端上传 ----------
// opts 可传 { signal }（AbortController），用于右下角任务面板里「取消」某一条
export const uploadWeb = (libraryId, files, onProgress, opts) => {
  const fd = new FormData()
  files.forEach((f) => fd.append('files', f))
  fd.append('library_id', libraryId)
  return http.post('/upload/web', fd, {
    timeout: 0,
    onUploadProgress: onProgress,
    ...(opts || {}),
  })
}

// ---------- 补采拍摄时间（历史视频用 mtime 排序不准，跑 ffprobe 读容器时间） ----------
export const refreshTaken = (libraryId) =>
  http.post('/media/refresh-taken', { library_id: libraryId || 0 })
export const refreshTakenStatus = () => http.get('/media/refresh-taken')
export const token = () => localStorage.getItem('naspic.token') || ''
export const thumbUrl = (id, size = 'md') =>
  `/api/v1/media/${id}/thumb?size=${size}&token=${encodeURIComponent(token())}`
export const fileUrl = (id, download = false) =>
  `/api/v1/media/${id}/file${download ? '?download=1&' : '?'}token=${encodeURIComponent(
    token()
  )}`

// ---------- 管理员账号设置 ----------
export const listAdminUsers = () => http.get('/admin/users')
export const createAdminUser = (data) => http.post('/admin/users', data)
export const updateAdminUser = (id, data) => http.put(`/admin/users/${id}`, data)
export const deleteAdminUser = (id) => http.delete(`/admin/users/${id}`)
export const resetAdminPassword = (id, password) =>
  http.put(`/admin/users/${id}/password`, { password })
export const changeMyPassword = (oldPassword, newPassword) =>
  http.post('/auth/password', { old_password: oldPassword, new_password: newPassword })
export const myProfile = () => http.get('/auth/me')

// ---------- 权限 ----------
export const listPermissions = (resourceType, resourceId) =>
  http.get('/permissions', {
    params: { resource_type: resourceType, resource_id: resourceId },
  })
export const upsertPermission = (data) => http.put('/permissions', data)
export const deletePermission = (id) => http.delete('/permissions/' + id)
export const listUsers = () => http.get('/users')
export const listGroups = () => http.get('/groups')
export const createGroup = (data) => http.post('/groups', data)

// ---------- 手机同步 ----------
export const listDevices = () => http.get('/sync/devices')
export const listSyncTasks = (deviceId) =>
  http.get('/sync/tasks', { params: { device_id: deviceId } })
export const upsertSyncTask = (data) => http.put('/sync/tasks', data)
export const deleteSyncTask = (id) => http.delete('/sync/tasks/' + id)
export const listSyncRecords = (taskId, state) =>
  http.get(`/sync/tasks/${taskId}/records`, { params: { state } })

// ---------- 首次部署安装向导 ----------
// 这三个接口无需 token,setup 模式下后端不挂认证中间件
export const setupStatus = () => http.get('/setup/status')
export const setupTest = (form) => http.post('/setup/test', form)
export const setupSave = (form) => http.post('/setup/save', form)
