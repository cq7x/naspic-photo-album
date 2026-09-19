/**
 * 服务端接口封装
 * 特性：
 *  1. 局域网优先：每次同步前并行探测 LAN / 公网，取最快可达者（800ms 超时）
 *  2. 统一错误处理与 token 注入
 */
import { sha256Text } from './sha256.js'

const KEY = 'naspic.settings'

function settings() {
  return uni.getStorageSync(KEY) || {}
}

function saveSettings(patch) {
  const s = Object.assign({}, settings(), patch)
  uni.setStorageSync(KEY, s)
  return s
}

function token() {
  return uni.getStorageSync('naspic.token') || ''
}

// ---------- 基址探测（局域网优先） ----------

let cachedBase = null

async function ping(base) {
  return new Promise((resolve) => {
    const t = setTimeout(() => resolve(null), 800)
    uni.request({
      url: base + '/api/v1/health',
      method: 'GET',
      timeout: 800,
      success: (res) => {
        clearTimeout(t)
        if (res.statusCode === 200) resolve(base)
        else resolve(null)
      },
      fail: () => { clearTimeout(t); resolve(null) },
    })
  })
}

/** 解析可用基址：LAN 优先，失败回落公网 */
export async function resolveBase(force = false) {
  if (cachedBase && !force) return cachedBase
  const s = settings()
  const candidates = []
  if (s.lanIP) candidates.push(`http://${s.lanIP}:${s.lanPort || 8080}`)
  if (s.baseURL) candidates.push(s.baseURL)
  if (candidates.length === 0) throw new Error('未配置服务器地址')

  const winner = await Promise.any(candidates.map(ping)).catch(() => null)
  cachedBase = winner || s.baseURL || candidates[0]
  // 记录本次是否走 LAN，供设置页展示
  saveSettings({ usingLAN: !!winner && winner !== s.baseURL })
  return cachedBase
}

// ---------- 通用请求 ----------

async function request(path, options = {}) {
  const base = await resolveBase()
  const { method = 'GET', data, header = {}, timeout = 30000, raw = false } = options
  return new Promise((resolve, reject) => {
    uni.request({
      url: base + path,
      method,
      data,
      timeout,
      header: Object.assign(
        { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token() },
        header
      ),
      success: (res) => {
        if (res.statusCode >= 200 && res.statusCode < 300) {
          const body = res.data
          if (raw) return resolve(body)
          if (body && body.code === 0) return resolve(body.data)
          reject(new Error((body && body.msg) || '请求失败 ' + res.statusCode))
        } else {
          reject(new Error('HTTP ' + res.statusCode))
        }
      },
      fail: (err) => reject(new Error(err.errMsg || '网络错误')),
    })
  })
}

// ---------- 认证 ----------

export function login(baseURL, username, password) {
  saveSettings({ baseURL })
  cachedBase = null
  return new Promise((resolve, reject) => {
    uni.request({
      url: baseURL + '/api/v1/auth/login',
      method: 'POST',
      data: { username, password },
      success: (res) => {
        const b = res.data
        if (b && b.code === 0) {
          uni.setStorageSync('naspic.token', b.data.token)
          resolve(b.data)
        } else reject(new Error((b && b.msg) || '登录失败'))
      },
      fail: reject,
    })
  })
}

// ---------- 设备与同步任务 ----------

export function registerDevice(payload) {
  return request('/api/v1/sync/devices', { method: 'POST', data: payload })
}

export function listLibraries() {
  return request('/api/v1/libraries')
}

export function upsertSyncTask(task) {
  return request('/api/v1/sync/tasks', { method: 'PUT', data: task })
}

export function listSyncTasks(deviceId) {
  return request('/api/v1/sync/tasks' + (deviceId ? '?device_id=' + deviceId : ''))
}

export function deleteSyncTask(id) {
  return request('/api/v1/sync/tasks/' + id, { method: 'DELETE' })
}

// ---------- 上传三步 ----------

/** 秒传探测 */
export function uploadCheck(payload) {
  return request('/api/v1/upload/check', { method: 'POST', data: payload })
}

/** 初始化会话 */
export function uploadInit(payload) {
  return request('/api/v1/upload/init', { method: 'POST', data: payload })
}

/**
 * 上传单个分片（断点续传）
 * @param {string} sessionId
 * @param {number} index
 * @param {Uint8Array} chunk
 */
export function uploadChunk(sessionId, index, chunk) {
  const base = cachedBase
  return new Promise((resolve, reject) => {
    uni.request({
      url: `${base}/api/v1/upload/chunk?session_id=${sessionId}&index=${index}`,
      method: 'PUT',
      // #ifdef APP-PLUS
      data: chunk.buffer ? chunk.buffer.slice(chunk.byteOffset, chunk.byteOffset + chunk.length) : chunk,
      // #endif
      // #ifndef APP-PLUS
      data: chunk,
      // #endif
      header: {
        'Content-Type': 'application/octet-stream',
        Authorization: 'Bearer ' + token(),
      },
      timeout: 60000,
      success: (res) => {
        const b = res.data
        if (res.statusCode === 200 && b && b.code === 0) resolve(b.data)
        else reject(new Error((b && b.msg) || '分片上传失败'))
      },
      fail: (e) => reject(new Error(e.errMsg || '分片上传失败')),
    })
  })
}

export function uploadComplete(payload) {
  return request('/api/v1/upload/complete', { method: 'POST', data: payload })
}

// ---------- 工具 ----------

export function pathKey(p) { return sha256Text(p || '') }

export { saveSettings, settings }
