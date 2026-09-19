/**
 * 本地持久化
 * 说明：MVP 使用 Storage + 内存索引 + 防抖落盘。
 * 若单个文件夹超过 2 万条记录，建议切换 plus.sqlite（接口不变，实现替换本文件即可）。
 */
import { pathKey } from './api.js'

const TASKS = 'naspic.tasks'
const LOGS = 'naspic.logs'
const MAX_LOG = 500
let flushTimer = null
let dirty = new Set()

function recKey(taskId) { return 'naspic.records.' + taskId }

// ---------- 同步任务 ----------

export function getTasks() {
  return uni.getStorageSync(TASKS) || []
}

export function saveTasks(tasks) {
  uni.setStorageSync(TASKS, tasks)
}

export function upsertTask(task) {
  const tasks = getTasks()
  const idx = tasks.findIndex((t) => t.folderUri === task.folderUri)
  if (idx >= 0) tasks[idx] = Object.assign({}, tasks[idx], task)
  else tasks.push(Object.assign({ id: Date.now() }, task))
  saveTasks(tasks)
  return tasks
}

export function removeTask(folderUri) {
  const tasks = getTasks().filter((t) => t.folderUri !== folderUri)
  saveTasks(tasks)
  uni.removeStorageSync(recKey(localTaskId(folderUri)))
  return tasks
}

function localTaskId(folderUri) {
  const t = getTasks().find((x) => x.folderUri === folderUri)
  return t ? t.id : 0
}

// ---------- 单文件记录 ----------

export function getRecords(taskId) {
  return uni.getStorageSync(recKey(taskId)) || {}
}

function persist(taskId, records) {
  dirty.add(taskId)
  if (flushTimer) return
  flushTimer = setTimeout(() => {
    flushTimer = null
    dirty.forEach((id) => {
      uni.setStorageSync(recKey(id), getRecords(id))
    })
    dirty.clear()
  }, 1000)
}

export function getRecord(taskId, path) {
  const r = getRecords(taskId)
  return r[pathKey(path)]
}

export function upsertRecord(taskId, path, patch) {
  const records = getRecords(taskId)
  const k = pathKey(path)
  records[k] = Object.assign({ path, updatedAt: Date.now() }, records[k] || {}, patch)
  persist(taskId, records)
  return records[k]
}

export function markDone(taskId, path, info) {
  return upsertRecord(taskId, path, Object.assign({ state: 3, error: '' }, info))
}

export function markFailed(taskId, path, err, retry) {
  return upsertRecord(taskId, path, {
    state: retry >= 5 ? 6 : 4,
    retry: (retry || 0) + 1,
    error: String((err && err.message) || err),
  })
}

export function countByState(taskId) {
  const records = getRecords(taskId)
  const c = { total: 0, done: 0, failed: 0, pending: 0, skipped: 0 }
  Object.values(records).forEach((r) => {
    c.total++
    if (r.state === 3) c.done++
    else if (r.state === 5) c.skipped++
    else if (r.state === 4 || r.state === 6) c.failed++
    else c.pending++
  })
  return c
}

/** 清理 N 天前已完成的记录，避免本地存储无限膨胀 */
export function gcRecords(taskId, keepDays = 30) {
  const records = getRecords(taskId)
  const cutoff = Date.now() - keepDays * 86400000
  Object.keys(records).forEach((k) => {
    if (records[k].state === 3 && records[k].updatedAt < cutoff) delete records[k]
  })
  persist(taskId, records)
}

// ---------- 日志 ----------

export function addLog(level, msg) {
  const logs = uni.getStorageSync(LOGS) || []
  logs.unshift({ t: Date.now(), level, msg: String(msg).slice(0, 300) })
  if (logs.length > MAX_LOG) logs.length = MAX_LOG
  uni.setStorageSync(LOGS, logs)
}

export function getLogs() {
  return uni.getStorageSync(LOGS) || []
}
