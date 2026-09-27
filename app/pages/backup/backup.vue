<template>
  <view class="page">
    <!-- ============ 总开关 ============ -->
    <view class="card">
      <view class="row">
        <view class="col">
          <text class="title">自动备份</text>
          <text class="desc">开启后在 WiFi 下自动增量备份照片与视频</text>
        </view>
        <switch :checked="settings.autoBackup" @change="onToggleAuto" color="#2979ff" />
      </view>
    </view>

    <!-- ============ 系统相册 ============ -->
    <view class="card" v-if="settings.autoBackup">
      <view class="row">
        <view class="col">
          <text class="title">系统相册</text>
          <text class="desc">自动备份手机相册中的新增照片与视频</text>
        </view>
        <switch :checked="systemTask.enabled" @change="onToggleSystem" color="#2979ff" />
      </view>

      <view class="sub" v-if="systemTask.enabled">
        <view class="row">
          <text class="label">上传网络</text>
          <view class="seg">
            <text :class="['seg-item', systemTask.wifiOnly ? 'on' : '']" @click="setSystem('wifiOnly', true)">仅 WiFi</text>
            <text :class="['seg-item', !systemTask.wifiOnly ? 'on' : '']" @click="setSystem('wifiOnly', false)">允许流量</text>
          </view>
        </view>
        <view class="row">
          <text class="label">画质</text>
          <view class="seg">
            <text :class="['seg-item', systemTask.uploadOriginal ? 'on' : '']" @click="setSystem('uploadOriginal', true)">原图</text>
            <text :class="['seg-item', !systemTask.uploadOriginal ? 'on' : '']" @click="setSystem('uploadOriginal', false)">压缩</text>
          </view>
        </view>
        <view class="row">
          <text class="label">文件类型</text>
          <view class="seg">
            <text :class="['seg-item', hasType('image') ? 'on' : '']" @click="toggleType('image')">照片</text>
            <text :class="['seg-item', hasType('video') ? 'on' : '']" @click="toggleType('video')">视频</text>
          </view>
        </view>
      </view>
    </view>

    <!-- ============ 自定义文件夹 ============ -->
    <view class="card" v-if="settings.autoBackup">
      <view class="row">
        <view class="col">
          <text class="title">自定义文件夹</text>
          <text class="desc">可单独开启手机内任意文件夹，每个文件夹独立配置</text>
        </view>
        <text class="add" @click="addFolder">+ 添加</text>
      </view>

      <view v-if="folders.length === 0" class="empty">尚未添加任何文件夹</view>

      <view class="folder" v-for="(f, i) in folders" :key="f.folderUri">
        <view class="row">
          <view class="col">
            <text class="fname">{{ f.folderPath || f.folderUri }}</text>
            <text class="desc">{{ summarize(f) }}</text>
          </view>
          <switch :checked="f.enabled" @change="(e) => toggleFolder(i, e)" color="#2979ff" />
        </view>

        <view class="sub" v-if="f.enabled">
          <view class="row">
            <text class="label">仅 WiFi</text>
            <switch :checked="f.wifiOnly" @change="(e) => setFolder(i, 'wifiOnly', e.detail.value)" color="#2979ff" />
          </view>
          <view class="row">
            <text class="label">上传原图</text>
            <switch :checked="f.uploadOriginal" @change="(e) => setFolder(i, 'uploadOriginal', e.detail.value)" color="#2979ff" />
          </view>
          <view class="row">
            <text class="label">包含子目录</text>
            <switch :checked="f.includeSubdir" @change="(e) => setFolder(i, 'includeSubdir', e.detail.value)" color="#2979ff" />
          </view>
          <view class="row">
            <text class="label">目标相册库</text>
            <picker :value="libIndex(f)" :range="libNames" @change="(e) => pickLib(i, e)">
              <text class="picker">{{ libName(f) || '请选择' }}</text>
            </picker>
          </view>
          <text class="remove" @click="removeFolder(i)">移除该文件夹</text>
        </view>
      </view>
    </view>

    <!-- ============ 冲突处理 ============ -->
    <view class="card" v-if="settings.autoBackup">
      <view class="row">
        <view class="col">
          <text class="title">冲突处理</text>
          <text class="desc">服务端已存在同名但内容不同的文件时如何处理</text>
        </view>
      </view>
      <view class="sub">
        <view class="row" v-for="opt in conflictOptions" :key="opt.value" @click="settings.conflict = opt.value">
          <text class="label">{{ opt.label }}</text>
          <text class="radio">{{ settings.conflict === opt.value ? '●' : '○' }}</text>
        </view>
        <text class="tip">无论选择哪种策略，手机本地原图都不会被删除或覆盖。</text>
      </view>
    </view>

    <!-- ============ 同步状态与历史 ============ -->
    <view class="card">
      <view class="row">
        <view class="col">
          <text class="title">同步状态</text>
          <text class="desc">{{ statusText }}</text>
        </view>
        <button class="btn" :class="{ stop: running }" @click="onMainBtn">{{ running ? (stopping ? '停止中…' : '停止同步') : '立即同步' }}</button>
      </view>

      <view class="progress" v-if="running">
        <view class="bar"><view class="bar-inner" :style="{ width: percent + '%' }" /></view>
        <text class="desc">{{ progress.done }}/{{ progress.total }} · 成功 {{ progress.synced }} · 失败 {{ progress.failed }}</text>
      </view>

      <view class="sub">
        <view class="row" @click="showFail">
          <text class="label">失败列表</text>
          <text class="arrow">{{ failCount }} 条 ›</text>
        </view>
        <view class="row" @click="showLogs">
          <text class="label">同步日志</text>
          <text class="arrow">{{ logs.length }} 条 ›</text>
        </view>
        <view class="row">
          <text class="label">上次同步</text>
          <text class="arrow">{{ lastSyncText }}</text>
        </view>
      </view>
    </view>

    <!-- ============ 后台保活 ============ -->
    <view class="card">
      <view class="row">
        <view class="col">
          <text class="title">后台保活</text>
          <text class="desc">系统可能限制后台运行，按提示设置可显著提升同步成功率</text>
        </view>
      </view>
      <view class="sub">
        <text class="tip" v-if="isAndroid">
          · 设置 → 电池 → 应用省电策略：设为「无限制」
          · 设置 → 应用管理 → 自启动：允许
          · 多任务界面下拉锁定 Naspic
        </text>
        <text class="tip" v-else>
          · 设置 → 通用 → 后台 App 刷新：开启
          · 照片权限建议选择「所有照片」
          · 建议开启「充电时自动同步」
        </text>
        <button class="btn ghost" @click="openSystemSettings">前往系统设置</button>
        <button class="btn ghost" @click="guideBattery">电池优化白名单</button>
      </view>
    </view>
  </view>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import * as store from '../../utils/store.js'
import * as api from '../../utils/api.js'
import { pickFolder } from '../../utils/folderPicker.js'
import { runAll, onStateChange, isRunning, stopSync, STATE } from '../../sync/engine.js'
import { deviceUUID, localDeviceId } from '../../utils/device.js'
import { requestAlbumPermission } from '../../utils/permission.js'

const SETTINGS_KEY = 'naspic.backupSettings'

const settings = reactive(
  Object.assign(
    { autoBackup: true, conflict: 'rename' },
    uni.getStorageSync(SETTINGS_KEY) || {}
  )
)

const folders = ref([])
const systemTask = reactive({
  folderUri: 'system://album',
  folderPath: '系统相册',
  folderType: 1,
  enabled: true,
  wifiOnly: true,
  uploadOriginal: true,
  includeSubdir: false,
  fileTypes: ['image', 'video'],
})

const libraries = ref([])
// 第 0 项是「留空」哨兵：targetLibraryId=0 时后端按设备名自动建/找托管库
const AUTO_LIB = { id: 0, name: '自动（按设备名建库，推荐）' }
const libNames = computed(() => [AUTO_LIB.name].concat(libraries.value.map((l) => l.name || l.storage_root)))
const running = ref(false)
const stopping = ref(false) // 已点停止，等队列收尾
const progress = reactive({ done: 0, total: 0, synced: 0, failed: 0, skipped: 0 })
const logs = ref([])
const failCount = ref(0)
const isAndroid = ref(true)

const conflictOptions = [
  { value: 'rename', label: '保留双方，服务端自动重命名（推荐）' },
  { value: 'skip', label: '跳过，不上传重名文件' },
  { value: 'dedup', label: '仅按内容去重，同名不同内容则重命名' },
]

const percent = computed(() =>
  progress.total ? Math.floor((progress.done / progress.total) * 100) : 0
)
const statusText = computed(() => {
  if (running.value) return '正在同步…'
  return settings.autoBackup ? '自动备份已开启，等待触发' : '自动备份已关闭'
})
const lastSyncText = computed(() => {
  const t = settings.lastSyncAt
  if (!t) return '从未'
  const d = new Date(t)
  return `${d.getMonth() + 1}月${d.getDate()}日 ${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`
})

onMounted(async () => {
  try {
    const info = uni.getSystemInfoSync()
    isAndroid.value = String(info.platform).toLowerCase() === 'android'
  } catch (e) { /* 忽略 */ }

  // 目标库（只接受托管库，挂载库只读不可上传）
  try {
    const libs = await api.listLibraries()
    libraries.value = libs.filter((l) => l.type === 1 && l.writable)
  } catch (e) { /* 未登录时忽略 */ }

  const tasks = store.getTasks()
  const sys = tasks.find((t) => t.folderType === 1)
  if (sys) Object.assign(systemTask, sys)
  folders.value = tasks.filter((t) => t.folderType === 2)
  logs.value = store.getLogs()
  running.value = isRunning()

  onStateChange((evt) => {
    if (evt.type === 'progress') {
      Object.assign(progress, {
        synced: evt.synced, failed: evt.failed, skipped: evt.skipped,
        total: evt.total, done: evt.synced + evt.failed + evt.skipped,
      })
    } else if (evt.type === 'stopping') {
      stopping.value = true
    }
  })

  // 任何接口返回 401 都清 token 并广播，这里统一提示重新登录
  uni.$off('naspic:unauthorized', onSessionExpired)
  uni.$on('naspic:unauthorized', onSessionExpired)
})

function onSessionExpired() {
  stopSync()
  running.value = false
  stopping.value = false
  uni.showModal({
    title: '登录已过期',
    content: '服务端令牌失效了（一般是服务器重启或重新部署导致）。请到「我的」重新登录后再次同步。',
    showCancel: false,
    confirmText: '知道了',
  })
}

/** 主按钮：同步中 → 停止；空闲 → 开始同步 */
function onMainBtn() {
  if (running.value) {
    if (stopping.value) {
      uni.showToast({ title: '正在收尾，稍等几秒', icon: 'none' })
      return
    }
    stopSync()
    stopping.value = true
    uni.showToast({ title: '已请求停止，正在收尾…', icon: 'none' })
    return
  }
  syncNow()
}

function persist() {
  uni.setStorageSync(SETTINGS_KEY, { ...settings })
}

function saveTasksToStore() {
  const all = [{ ...systemTask }, ...folders.value]
  store.saveTasks(all.map((t) => Object.assign({}, t, {
    id: t.id || hashId(t.folderUri),
    deviceId: localDeviceId(),
    // targetLibraryId 留 0：后端会根据 device_id 自动创建以设备名命名的托管库
    targetLibraryId: t.targetLibraryId || 0,
  })))
}

function hashId(s) {
  let h = 0
  for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0
  return Math.abs(h)
}

function onToggleAuto(e) {
  settings.autoBackup = e.detail.value
  persist()
}

function onToggleSystem(e) {
  systemTask.enabled = e.detail.value
  saveTasksToStore()
  syncServerTask(systemTask)
}

function setSystem(key, val) {
  systemTask[key] = val
  saveTasksToStore()
  syncServerTask(systemTask)
}

function toggleType(t) {
  const i = systemTask.fileTypes.indexOf(t)
  if (i >= 0) systemTask.fileTypes.splice(i, 1)
  else systemTask.fileTypes.push(t)
  saveTasksToStore()
  syncServerTask(systemTask)
}
function hasType(t) {
  return systemTask.fileTypes.includes(t)
}

// ---------- 自定义文件夹 ----------

async function addFolder() {
  try {
    const picked = await pickFolder()
    if (folders.value.some((f) => f.folderUri === picked.uri)) {
      return uni.showToast({ title: '该文件夹已添加', icon: 'none' })
    }
    folders.value.push({
      folderUri: picked.uri,
      folderPath: picked.path,
      folderType: 2,
      enabled: true,
      wifiOnly: true,
      uploadOriginal: true,
      includeSubdir: true,
      fileTypes: ['image', 'video'],
      targetLibraryId: 0, // 留 0 → 后端按设备名自动建/找托管库，别默认塞第一个库
    })
    saveTasksToStore()
    syncServerTask(folders.value[folders.value.length - 1])
  } catch (e) {
    uni.showToast({ title: e.message || '选择失败', icon: 'none' })
  }
}

function toggleFolder(i, e) {
  folders.value[i].enabled = e.detail.value
  saveTasksToStore()
  syncServerTask(folders.value[i])
}
function setFolder(i, key, val) {
  folders.value[i][key] = val
  saveTasksToStore()
  syncServerTask(folders.value[i])
}
function removeFolder(i) {
  const f = folders.value[i]
  store.removeTask(f.folderUri)
  folders.value.splice(i, 1)
  api.deleteSyncTask(f.id).catch(() => {})
}
function libIndex(f) {
  if (!f.targetLibraryId) return 0
  const i = libraries.value.findIndex((l) => l.id === f.targetLibraryId)
  return i < 0 ? 0 : i + 1
}
function libName(f) {
  if (!f.targetLibraryId) return AUTO_LIB.name
  const l = libraries.value.find((x) => x.id === f.targetLibraryId)
  return l ? l.name : AUTO_LIB.name
}
function pickLib(i, e) {
  const idx = Number(e.detail.value) || 0
  const lib = idx === 0 ? AUTO_LIB : libraries.value[idx - 1]
  if (lib) {
    folders.value[i].targetLibraryId = lib.id
    saveTasksToStore()
    syncServerTask(folders.value[i])
  }
}
function summarize(f) {
  const parts = []
  parts.push(f.wifiOnly ? '仅WiFi' : '允许流量')
  parts.push(f.uploadOriginal ? '原图' : '压缩')
  if (f.includeSubdir) parts.push('含子目录')
  return parts.join(' · ')
}

// ---------- 上报服务端 ----------

async function syncServerTask(t) {
  try {
    await api.upsertSyncTask({
      device_id: localDeviceId(),
      target_library_id: t.targetLibraryId,
      folder_uri: t.folderUri,
      folder_path: t.folderPath,
      folder_type: t.folderType,
      enabled: t.enabled,
      wifi_only: t.wifiOnly,
      upload_original: t.uploadOriginal,
      include_subdir: t.includeSubdir,
      file_types: t.fileTypes,
    })
  } catch (e) {
    console.warn('[backup] 上报失败:', e.message)
  }
}

// ---------- 同步 ----------

async function syncNow() {
  if (running.value) return
  stopping.value = false

  // 开门第一件事：确认登录态。401 时别再拿 2102 个文件白白跑一遍
  try {
    await api.ensureAuth()
  } catch (e) {
    if (e && e.unauthorized) {
      uni.showModal({
        title: '登录已过期',
        content: '请先到「我的」重新登录，同步需要有效的登录状态。',
        showCancel: false,
        confirmText: '知道了',
      })
    } else {
      uni.showToast({ title: '连不上服务器：' + (e.message || ''), icon: 'none' })
    }
    return
  }

  // 同步前再次确认相册权限（用户可能在系统设置中撤销了）
  // #ifdef APP-PLUS
  if (plus.os.name === 'Android') {
    const ok = await requestAlbumPermission()
    if (!ok) {
      uni.showModal({
        title: '缺少相册权限',
        content: '请在系统设置中授予 Naspic 相册权限后再同步。',
        confirmText: '去设置',
        cancelText: '取消',
        success: (r) => {
          if (r.confirm) {
            try {
              const main = plus.android.runtimeMainActivity()
              const Intent = plus.android.importClass('android.content.Intent')
              const Settings = plus.android.importClass('android.provider.Settings')
              const intent = new Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS)
              intent.setData(plus.android.importClass('android.net.Uri').parse('package:' + main.getPackageName()))
              main.startActivity(intent)
            } catch (e) { /* ignore */ }
          }
        },
      })
      return
    }
  }
  // #endif
  running.value = true
  Object.assign(progress, { done: 0, total: 0, synced: 0, failed: 0, skipped: 0 })
  try {
    // 确保任务已持久化（含 deviceId / id）
    saveTasksToStore()

    const tasks = store.getTasks().filter((t) => t.enabled)
    console.log('[backup] 即将同步 ' + tasks.length + ' 个任务', JSON.stringify(tasks.map((t) => ({
      id: t.id, lib: t.targetLibraryId, dev: t.deviceId, type: t.folderType, wifi: t.wifiOnly
    }))))

    const r = await runAll()
    settings.lastSyncAt = Date.now()
    persist()
    if (r && r.stopped) {
      uni.showToast({ title: '同步已停止', icon: 'none' })
    } else {
      uni.showToast({
        title: `完成：新增 ${r.synced || 0}，失败 ${r.failed || 0}`,
        icon: 'none',
      })
    }
  } catch (e) {
    uni.showToast({ title: e.message || '同步失败', icon: 'none' })
  } finally {
    running.value = false
    logs.value = store.getLogs()
    // 统计失败条数
    let n = 0
    store.getTasks().forEach((t) => { n += store.countByState(t.id).failed })
    failCount.value = n
  }
}

function showFail() {
  uni.showToast({ title: `失败 ${failCount.value} 条（详见同步日志）`, icon: 'none' })
}
function showLogs() {
  uni.showModal({ title: '同步日志', content: logs.value.slice(0, 5).map((l) => l.msg).join('\n') || '暂无' })
}

// ---------- 系统设置引导 ----------

function openSystemSettings() {
  // #ifdef APP-PLUS
  try {
    const main = plus.android.runtimeMainActivity()
    const Intent = plus.android.importClass('android.content.Intent')
    const Settings = plus.android.importClass('android.provider.Settings')
    const intent = new Intent(Settings.ACTION_APPLICATION_DETAILS_SETTINGS)
    intent.setData(plus.android.importClass('android.net.Uri').parse('package:' + main.getPackageName()))
    main.startActivity(intent)
  } catch (e) {
    plus.runtime.openURL('app-settings:')
  }
  // #endif
  // #ifndef APP-PLUS
  uni.showToast({ title: '请在系统设置中搜索 Naspic', icon: 'none' })
  // #endif
}

function guideBattery() {
  // #ifdef APP-PLUS
  try {
    const main = plus.android.runtimeMainActivity()
    const Intent = plus.android.importClass('android.content.Intent')
    const Settings = plus.android.importClass('android.provider.Settings')
    const intent = new Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS)
    intent.setData(plus.android.importClass('android.net.Uri').parse('package:' + main.getPackageName()))
    main.startActivity(intent)
  } catch (e) {
    uni.showToast({ title: '请手动关闭电池优化', icon: 'none' })
  }
  // #endif
}
</script>

<style scoped>
.page { padding: 20rpx; padding-bottom: 60rpx; }
.card {
  background: #fff; border-radius: 16rpx; padding: 24rpx; margin-bottom: 20rpx;
}
.row { display: flex; align-items: center; justify-content: space-between; padding: 12rpx 0; }
.col { display: flex; flex-direction: column; flex: 1; margin-right: 20rpx; }
.title { font-size: 30rpx; font-weight: 600; color: #303133; }
.desc { font-size: 24rpx; color: #909399; margin-top: 6rpx; }
.label { font-size: 28rpx; color: #606266; }
.sub { border-top: 1rpx solid #f0f0f0; margin-top: 12rpx; padding-top: 8rpx; }
.folder { border-top: 1rpx solid #f0f0f0; padding-top: 12rpx; margin-top: 12rpx; }
.fname { font-size: 28rpx; color: #303133; }
.add { color: #2979ff; font-size: 28rpx; }
.remove { color: #f56c6c; font-size: 26rpx; margin-top: 12rpx; display: block; }
.empty { color: #c0c4cc; font-size: 26rpx; padding: 20rpx 0; text-align: center; }
.seg { display: flex; border: 1rpx solid #dcdfe6; border-radius: 8rpx; overflow: hidden; }
.seg-item { padding: 8rpx 20rpx; font-size: 24rpx; color: #606266; }
.seg-item.on { background: #2979ff; color: #fff; }
.picker { color: #2979ff; font-size: 26rpx; }
.arrow { color: #c0c4cc; font-size: 26rpx; }
.radio { color: #2979ff; font-size: 32rpx; }
.tip { font-size: 24rpx; color: #909399; line-height: 1.8; display: block; }
.progress { padding: 12rpx 0; }
.bar { height: 10rpx; background: #ebeef5; border-radius: 6rpx; overflow: hidden; }
.bar-inner { height: 100%; background: #2979ff; transition: width .2s; }
.btn {
  font-size: 26rpx; padding: 0 28rpx; height: 64rpx; line-height: 64rpx;
  background: #2979ff; color: #fff; border-radius: 32rpx;
}
.btn.ghost { background: #fff; color: #2979ff; border: 1rpx solid #2979ff; margin-top: 16rpx; }
/* 同步中：按钮变成「停止」的红色态 */
.btn.stop { background: #e53935; }
.btn[disabled] { opacity: 0.5; }
</style>
