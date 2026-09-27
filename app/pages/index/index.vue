<template>
  <view class="page">
    <!-- 头部横幅 -->
    <view class="header">
      <view class="h-logo">📷</view>
      <view class="h-text">
        <text class="h-title">Naspic</text>
        <text class="h-sub">私有云相册 · 手机备份</text>
      </view>
      <view class="h-pill" :class="connClass">
        <text class="h-dot" />
        <text>{{ connText }}</text>
      </view>
    </view>

    <!-- 未登录：连接服务器 -->
    <view class="card" v-if="!loggedIn">
      <text class="card-title">连接服务器</text>
      <text class="card-desc">同一局域网内建议使用内网 IP，传输更快更稳定</text>
      <input class="input" v-model="form.baseURL" placeholder="服务器地址，如 http://192.168.1.10:8080" />
      <input class="input" v-model="form.username" placeholder="用户名" />
      <input class="input" password v-model="form.password" placeholder="密码" />
      <button class="btn primary" :loading="loading" @click="doLogin">连接</button>
      <text class="tip">默认账号 admin / naspic123，登录后请尽快修改密码</text>
    </view>

    <!-- 已登录：概览 + 操作 -->
    <view class="card" v-else>
      <view class="row">
        <view class="col">
          <text class="label">设备标识</text>
          <text class="val">{{ deviceId }}</text>
        </view>
        <view class="col right">
          <text class="label">状态</text>
          <text class="val" :class="{ ok: !running }">{{ running ? '同步中' : '空闲' }}</text>
        </view>
      </view>

      <button class="btn primary block" :class="{ stop: running }" @click="onSyncBtn">
        {{ running ? (stopping ? '停止中…' : '停止同步') : '立即同步' }}
      </button>

      <view class="bar" v-if="running">
        <view class="bar-inner" :style="{ width: percent + '%' }" />
      </view>
      <text class="tip center" v-if="running">{{ percent }}%</text>

      <view class="menu" @click="goBackup">
        <text class="menu-label">自动备份设置</text>
        <text class="arrow">›</text>
      </view>
      <view class="menu" @click="logout">
        <text class="menu-label danger">退出登录</text>
        <text class="arrow">›</text>
      </view>

      <text class="tip center" v-if="lastResult">{{ lastResult }}</text>
    </view>
  </view>
</template>

<script setup>
import { ref, reactive, computed, onMounted, watch } from 'vue'
import * as api from '../../utils/api.js'
import { runAll, isRunning, stopSync } from '../../sync/engine.js'
import { deviceUUID, localDeviceId, initDevice } from '../../utils/device.js'
import { session } from '../../utils/session.js'

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
  username: _s.username || 'admin',
  password: '',
})

const connText = computed(() => {
  if (session.needRelogin) return '登录已过期'
  if (!loggedIn.value) return '未连接'
  return running.value ? '同步中' : '已连接'
})
const connClass = computed(() => {
  if (session.needRelogin) return 'warn'
  if (!loggedIn.value) return 'off'
  return running.value ? 'busy' : 'on'
})

onMounted(() => {
  loggedIn.value = !!uni.getStorageSync('naspic.token')
  deviceId.value = deviceUUID().slice(0, 12)
  running.value = isRunning()
  if (loggedIn.value) initDevice()

  uni.$off('naspic:unauthorized', onSessionExpired)
  uni.$on('naspic:unauthorized', onSessionExpired)
  uni.$off('naspic:relogin-success', onReloginOk)
  uni.$on('naspic:relogin-success', onReloginOk)
})

// 全局重新登录弹窗触发：同步中 401 时，本页同步收尾并回到未登录态
function onSessionExpired() {
  stopSync()
  running.value = false
  stopping.value = false
  loggedIn.value = false
  lastResult.value = '登录已过期，请重新登录'
}
function onReloginOk() {
  loggedIn.value = !!uni.getStorageSync('naspic.token')
  deviceId.value = deviceUUID().slice(0, 12)
  running.value = isRunning()
  initDevice()
  lastResult.value = ''
}
// 弹窗已统一处理 UI，这里只保证页面状态一致
watch(() => session.needRelogin, (v) => { if (v) onSessionExpired() })

async function doLogin() {
  if (!form.baseURL) return uni.showToast({ title: '请填写服务器地址', icon: 'none' })
  loading.value = true
  try {
    await api.login(form.baseURL.replace(/\/$/, ''), form.username, form.password)
    loggedIn.value = true
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
  try {
    await api.ensureAuth()
  } catch (e) {
    if (e && e.unauthorized) {
      loggedIn.value = false
      lastResult.value = '登录已过期，请重新登录'
      uni.showModal({ title: '登录已过期', content: '请重新登录后再次同步。', showCancel: false, confirmText: '知道了' })
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

function goBackup() {
  uni.navigateTo({ url: '/pages/backup/backup' })
}

function logout() {
  uni.removeStorageSync('naspic.token')
  loggedIn.value = false
}
</script>

<style scoped>
.page { padding: 20rpx 20rpx 60rpx; }

/* 头部横幅 */
.header {
  display: flex; align-items: center; padding: 36rpx 28rpx;
  background: linear-gradient(135deg, #2979ff, #1565ff);
  border-radius: 20rpx; color: #fff; margin-bottom: 20rpx;
}
.h-logo { font-size: 48rpx; margin-right: 18rpx; }
.h-text { flex: 1; display: flex; flex-direction: column; }
.h-title { font-size: 36rpx; font-weight: 700; }
.h-sub { font-size: 23rpx; opacity: 0.85; margin-top: 4rpx; }
.h-pill {
  display: flex; align-items: center; padding: 8rpx 18rpx;
  border-radius: 30rpx; font-size: 22rpx; background: rgba(255, 255, 255, 0.2);
}
.h-dot { width: 12rpx; height: 12rpx; border-radius: 50%; background: #fff; margin-right: 10rpx; }
.h-pill.on .h-dot { background: #69f0ae; }
.h-pill.warn { background: rgba(255, 255, 255, 0.28); }
.h-pill.warn .h-dot { background: #ffd54f; }
.h-pill.busy .h-dot { background: #ffd54f; }
.h-pill.off .h-dot { background: #cfd8dc; }

/* 卡片 */
.card { background: #fff; border-radius: 20rpx; padding: 28rpx; margin-bottom: 20rpx; }
.card-title { font-size: 32rpx; font-weight: 700; color: #303133; display: block; }
.card-desc { font-size: 24rpx; color: #909399; margin: 8rpx 0 20rpx; display: block; }

.input {
  height: 80rpx; border: 1rpx solid #dcdfe6; border-radius: 12rpx;
  padding: 0 20rpx; font-size: 28rpx; margin-bottom: 18rpx;
}
.btn { margin-top: 10rpx; border-radius: 42rpx; font-size: 30rpx; }
.btn.primary { background: #2979ff; color: #fff; }
.btn.block { width: 100%; height: 88rpx; line-height: 88rpx; }
.btn.stop { background: #e53935; }
.tip { font-size: 24rpx; color: #909399; display: block; margin-top: 14rpx; line-height: 1.6; }
.tip.center { text-align: center; }

.row { display: flex; justify-content: space-between; padding: 10rpx 0; }
.col { display: flex; flex-direction: column; }
.col.right { align-items: flex-end; }
.label { font-size: 24rpx; color: #909399; }
.val { font-size: 28rpx; color: #303133; margin-top: 4rpx; }
.val.ok { color: #67c23a; }

.bar { height: 12rpx; background: #ebeef5; border-radius: 8rpx; margin-top: 20rpx; overflow: hidden; }
.bar-inner { height: 100%; background: #2979ff; transition: width .2s; }

.menu {
  display: flex; align-items: center; justify-content: space-between;
  padding: 26rpx 0; border-top: 1rpx solid #f2f3f5; margin-top: 16rpx;
}
.menu-label { font-size: 29rpx; color: #303133; }
.menu-label.danger { color: #e53935; }
.arrow { color: #c0c4cc; font-size: 32rpx; }
</style>
