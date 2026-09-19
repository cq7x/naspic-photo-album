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
      <button class="btn" :disabled="running" @click="syncNow">
        {{ running ? '同步中…' : '立即同步' }}
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
import { runAll, isRunning } from '../../sync/engine.js'
import { deviceUUID, localDeviceId } from '../../utils/device.js'

const loggedIn = ref(false)
const loading = ref(false)
const running = ref(false)
const percent = ref(0)
const lastResult = ref('')
const deviceId = ref('')

const form = reactive({
  baseURL: uni.getStorageSync('naspic.settings')?.baseURL || '',
  username: 'admin',
  password: '',
})

onMounted(() => {
  loggedIn.value = !!uni.getStorageSync('naspic.token')
  deviceId.value = deviceUUID().slice(0, 12)
  running.value = isRunning()
})

async function doLogin() {
  if (!form.baseURL) return uni.showToast({ title: '请填写服务器地址', icon: 'none' })
  loading.value = true
  try {
    await api.login(form.baseURL.replace(/\/$/, ''), form.username, form.password)
    loggedIn.value = true
    uni.showToast({ title: '连接成功' })
  } catch (e) {
    uni.showToast({ title: e.message || '连接失败', icon: 'none' })
  } finally {
    loading.value = false
  }
}

async function syncNow() {
  if (running.value) return
  running.value = true
  percent.value = 0
  try {
    const r = await runAll()
    lastResult.value = `完成：新增 ${r.synced || 0}，跳过 ${r.skipped || 0}，失败 ${r.failed || 0}`
    percent.value = 100
  } catch (e) {
    lastResult.value = e.message || '同步失败'
  } finally {
    running.value = false
  }
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
.row { display: flex; justify-content: space-between; padding: 20rpx 0; border-bottom: 1rpx solid #f5f5f5; }
.label { font-size: 28rpx; color: #606266; }
.arrow { color: #c0c4cc; }
.tip { font-size: 24rpx; color: #909399; display: block; margin-top: 16rpx; }
.bar { height: 10rpx; background: #ebeef5; border-radius: 6rpx; margin-top: 20rpx; overflow: hidden; }
.bar-inner { height: 100%; background: #2979ff; transition: width .2s; }
</style>
