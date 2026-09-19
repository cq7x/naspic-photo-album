/**
 * 同步引擎
 * 职责：把「策略」和「执行」串起来——扫描增量 → 过滤 → 上传 → 更新游标 → 上报服务端
 * 原则：
 *   1. 永不删除手机本地原图；
 *   2. 同名冲突由服务端自动重命名，双方文件都保留；
 *   3. 任一文件失败不影响其它文件，失败进入失败列表等待手动重试。
 */
import * as api from '../utils/api.js'
import * as store from '../utils/store.js'
import * as net from '../utils/net.js'
import { scanSystemAlbum, scanFolder, nextCursor } from './scanner.js'
import { upload } from './uploader.js'
import { runLimit } from './queue.js'

export const STATE = {
  IDLE: 1, SCANNING: 2, SYNCING: 3, PAUSED: 4, ERROR: 5,
}
export const REC = {
  PENDING: 0, HASHING: 1, UPLOADING: 2, DONE: 3, FAILED: 4, SKIPPED: 5, GIVE_UP: 6,
}

const listeners = new Set()
let running = false

export function onStateChange(fn) {
  listeners.add(fn)
  return () => listeners.delete(fn)
}
function emit(evt) { listeners.forEach((fn) => fn(evt)) }
export function isRunning() { return running }

/**
 * 执行一个同步任务（一个文件夹）
 * @param {object} task
 */
export async function runTask(task) {
  if (!task.enabled) return { skipped: true }
  if (task.wifiOnly && !net.isWifi()) {
    store.upsertTask(task.folderUri, { status: STATE.PAUSED })
    emit({ type: 'task', task, state: STATE.PAUSED, reason: 'waiting_wifi' })
    return { waitingWifi: true }
  }

  const taskId = task.id
  const types = task.fileTypes || ['image', 'video']

  // ① 扫描（增量：mtime > cursor）
  store.upsertTask(task.folderUri, { status: STATE.SCANNING })
  emit({ type: 'task', task, state: STATE.SCANNING })

  let files = []
  try {
    if (task.folderType === 1) {
      files = await scanSystemAlbum({ since: task.syncCursor || 0, types })
    } else {
      const all = await scanFolder(task.folderUri, { includeSubdir: task.includeSubdir !== false, types })
      files = task.syncCursor ? all.filter((f) => f.mtime > task.syncCursor) : all
    }
  } catch (e) {
    store.upsertTask(task.folderUri, { status: STATE.ERROR, lastError: e.message })
    store.addLog('error', `扫描失败 ${task.folderPath}: ${e.message}`)
    return { error: e }
  }

  // ② L1 过滤：本地记录里 mtime 未变且已完成 → 跳过
  const todo = files.filter((f) => {
    const r = store.getRecord(taskId, f.path)
    if (!r) return true
    if (r.state === REC.DONE && r.mtime === f.mtime) return false
    if (r.state === REC.GIVE_UP) return false // 已放弃，需用户手动重试
    return true
  })

  store.upsertTask(task.folderUri, { status: STATE.SYNCING, totalCount: todo.length })
  emit({ type: 'task', task, state: STATE.SYNCING, total: todo.length })

  // ③ 上传（并发：WiFi 2 / 移动网络 1）
  const concurrency = net.isWifi() ? 2 : 1
  let synced = 0, failed = 0, skipped = files.length - todo.length

  await runLimit(todo, concurrency, async (file) => {
    // 压缩策略：非原图时先本地压缩（长边 1920）
    const payload = task.uploadOriginal === false
      ? await compress(file)
      : file

    try {
      const r = await upload(payload, task, {
        network: net.isWifi() ? 'wifi' : 'mobile',
        onProgress: (done, total) => {
          emit({ type: 'file', task, file, done, total })
        },
      })
      if (r.skipped) {
        skipped++
        store.upsertRecord(taskId, file.path, { state: REC.SKIPPED, hash: r.hash, mtime: file.mtime })
      } else {
        synced++
        store.markDone(taskId, file.path, { mediaId: r.mediaId, hash: r.hash, mtime: file.mtime, size: file.size })
      }
    } catch (e) {
      failed++
      const rec = store.getRecord(taskId, file.path) || {}
      store.markFailed(taskId, file.path, e, rec.retry || 0)
      store.addLog('error', `上传失败 ${file.name}: ${e.message}`)
    }
    emit({ type: 'progress', task, synced, failed, skipped, total: todo.length })
  })

  // ④ 更新游标并上报服务端
  const cursor = nextCursor(files, task.syncCursor || 0)
  const c = store.countByState(taskId)
  const patched = Object.assign({}, task, {
    status: STATE.IDLE, syncCursor: cursor, lastSyncAt: Date.now(),
    totalCount: c.total, syncedCount: c.done, failedCount: c.failed, skippedCount: c.skipped,
  })
  store.upsertTask(task.folderUri, patched)
  try {
    await api.upsertSyncTask({
      device_id: task.deviceId,
      target_library_id: task.targetLibraryId,
      folder_uri: task.folderUri,
      folder_path: task.folderPath,
      folder_type: task.folderType,
      enabled: task.enabled !== false,
      wifi_only: task.wifiOnly !== false,
      upload_original: task.uploadOriginal !== false,
      include_subdir: task.includeSubdir !== false,
      file_types: task.fileTypes,
      sync_cursor: String(cursor),
      status: STATE.IDLE,
      total_count: c.total, synced_count: c.done, failed_count: c.failed, skipped_count: c.skipped,
    })
  } catch (e) {
    store.addLog('warn', '上报同步状态失败: ' + e.message)
  }

  store.addLog('info', `同步完成 ${task.folderPath}：新增 ${synced}，跳过 ${skipped}，失败 ${failed}`)
  emit({ type: 'task', task: patched, state: STATE.IDLE, synced, failed, skipped })
  return { synced, failed, skipped }
}

/** 执行全部启用的任务 */
export async function runAll() {
  if (running) return { ignored: true }
  running = true
  const result = { synced: 0, failed: 0, skipped: 0, tasks: 0 }
  try {
    const tasks = store.getTasks().filter((t) => t.enabled)
    for (const t of tasks) {
      const r = await runTask(t)
      result.synced += r.synced || 0
      result.failed += r.failed || 0
      result.skipped += r.skipped || 0
      result.tasks++
    }
  } finally {
    running = false
  }
  return result
}

/** 压缩（非原图上传策略） */
async function compress(file) {
  return new Promise((resolve) => {
    // 视频不压缩，直接原样上传
    if (file.type === 'video') return resolve(file)
    uni.compressImage({
      src: file.path,
      quality: 82,
      width: '1920',
      success: (res) => resolve(Object.assign({}, file, { path: res.tempFilePath })),
      fail: () => resolve(file),
    })
  })
}
