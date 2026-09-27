<template>
  <view class="page">
    <view class="card">
      <text class="title">Naspic 私有云相册</text>
      <text class="desc">{{ loggedIn ? '已连接服务器' : '请先连接服务器' }}</text>
    </view>

    <view class="card" v-if="!loggedIn">
      <input class="input" v-model="form.baseURL" placeholder="服务器地址，如 http://192.168.1.10:8080" />
      <input class="input" v-model="form.username" placeholder="用户名" />
      <input class="input" password v-model="form.password" placeholder="密码" />
      <button class="btn" :loading="loading" @click="doLogin">连接</button>
      <text class="tip">同一局域网内建议使用内网 IP，传输速度更快</text>
    </view>

    <view class="card" v-else>
      <view class="row" @click="goBackup">
        <text class="label">自动备份设置</text>
        <text class="arrow">›</text>
      </view>
      <view class="row">
        <text class="label">设备标识</text>
        <text class="arrow">{{ deviceId }}</text>
      </view>
      <button class="btn" :class="{ stop: running }" @click="onSyncBtn">
        {{ running ? (stopping ? '停止中…' : '停止同步') : '立即同步' }}
      </button>
      <view class="bar" v-if="running">
        <view class="bar-inner" :style="{ width: percent + '%' }" />
      </view>
      <text class="tip" v-if="lastResult">{{ lastResult }}</text>
      <button class="btn ghost" @click="logout">退出登录</button>
    </view>
  </view>
</template>

<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import * as api from '../../utils/api.js'
import { runAll, isRunning, stopSync } from '../../sync/engine.js'
import { deviceUUID, localDeviceId, initDevice } from '../../utils/device.js'

const loggedIn = ref(false)
const loading = ref(false)
const running = ref(false)
const stopping = ref(false) // 已请求停止，等队列收尾
const percent = ref(0)
const lastResult = ref('')
const deviceId = ref('')

const _s = uni.getStorageSync('naspic.settings') || {}
const form = reactive({
  baseURL: _s.baseURL || '',
  username: 'admin',
  password: '',
})

onMounted(() => {
  loggedIn.value = !!uni.getStorageSync('naspic.token')
  deviceId.value = deviceUUID().slice(0, 12)
  running.value = isRunning()
  // 已登录则上报设备（每次启动刷新 last_seen_at）
  if (loggedIn.value) initDevice()
  uni.$off('naspic:unauthorized', onSessionExpired)
  uni.$on('naspic:unauthorized', onSessionExpired)
})

async function doLogin() {
  if (!form.baseURL) return uni.showToast({ title: '请填写服务器地址', icon: 'none' })
  loading.value = true
  try {
    await api.login(form.baseURL.replace(/\/$/, ''), form.username, form.password)
    loggedIn.value = true
    // 登录成功后注册设备（需要 token）
    await initDevice()
    uni.showToast({ title: '连接成功' })
  } catch (e) {
    uni.showToast({ title: e.message || '连接失败', icon: 'none' })
  } finally {
    loading.value = false
  }
}

async function syncNow() {
  if (running.value) return
  stopping.value = false
  // 登录态校验：401 时不启动，避免几千个文件白跑一遍
  try {
    await api.ensureAuth()
  } catch (e) {
    if (e && e.unauthorized) {
      loggedIn.value = false
      lastResult.value = '登录已过期，请重新连接服务器'
      uni.showModal({
        title: '登录已过期', content: '服务器令牌失效了，请重新连接服务器。',
        showCancel: false, confirmText: '知道了',
      })
    } else {
      lastResult.value = '连不上服务器：' + (e.message || '')
    }
    return
  }
  running.value = true
  percent.value = 0
  try {
    const r = await runAll()
    if (r && r.stopped) lastResult.value = '同步已停止'
    else lastResult.value = `完成：新增 ${r.synced || 0}，跳过 ${r.skipped || 0}，失败 ${r.failed || 0}`
    percent.value = 100
  } catch (e) {
    lastResult.value = e.message || '同步失败'
  } finally {
    running.value = false
    stopping.value = false
  }
}

/** 主按钮：同步中时可随时停止 */
function onSyncBtn() {
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

/** 服务端返回 401 时统一处理：停同步 + 回到登录态 */
function onSessionExpired() {
  stopSync()
  running.value = false
  stopping.value = false
  loggedIn.value = false
  lastResult.value = '登录已过期，请重新连接服务器'
}

function goBackup() {
  uni.navigateTo({ url: '/pages/backup/backup' })
}

function logout() {
  uni.removeStorageSync('naspic.token')
  loggedIn.value = false
}
</script>

<style scoped>
.page { padding: 20rpx; }
.card { background: #fff; border-radius: 16rpx; padding: 28rpx; margin-bottom: 20rpx; }
.title { font-size: 34rpx; font-weight: 700; color: #303133; display: block; }
.desc { font-size: 26rpx; color: #909399; margin-top: 8rpx; display: block; }
.input {
  height: 76rpx; border: 1rpx solid #dcdfe6; border-radius: 10rpx;
  padding: 0 20rpx; font-size: 28rpx; margin-bottom: 20rpx;
}
.btn { margin-top: 16rpx; background: #2979ff; color: #fff; border-radius: 40rpx; font-size: 28rpx; }
.btn.ghost { background: #fff; color: #2979ff; border: 1rpx solid #2979ff; }
.btn.stop { background: #e53935; }
.row { display: flex; justify-content: space-between; padding: 20rpx 0; border-bottom: 1rpx solid #f5f5f5; }
.label { font-size: 28rpx; color: #606266; }
.arrow { color: #c0c4cc; }
.tip { font-size: 24rpx; color: #909399; display: block; margin-top: 16rpx; }
.bar { height: 10rpx; background: #ebeef5; border-radius: 6rpx; margin-top: 20rpx; overflow: hidden; }
.bar-inner { height: 100%; background: #2979ff; transition: width .2s; }
</style>
