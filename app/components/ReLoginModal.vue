<template>
  <view v-if="session.needRelogin" class="rl-mask" @click.stop>
    <view class="rl-card">
      <view class="rl-head">
        <view class="rl-dot" />
        <text class="rl-title">登录已过期</text>
      </view>
      <text class="rl-desc">服务器令牌已失效（通常是服务器重启或重新部署导致）。重新登录后即可继续同步，无需重新配置。</text>

      <view class="rl-field">
        <text class="rl-label">服务器</text>
        <text class="rl-val">{{ serverDisplay }}</text>
      </view>
      <view class="rl-field">
        <text class="rl-label">用户名</text>
        <text class="rl-val">{{ username }}</text>
      </view>

      <input
        class="rl-input"
        v-model="password"
        :password="!showPwd"
        placeholder="请输入密码"
        confirm-type="done"
        @confirm="submit"
      />
      <view class="rl-eye" @click="showPwd = !showPwd">
        <text>{{ showPwd ? '隐藏密码' : '显示密码' }}</text>
      </view>

      <button class="rl-btn" :loading="loading" :disabled="!password" @click="submit">重新登录</button>
      <text class="rl-err" v-if="err">{{ err }}</text>
    </view>
  </view>
</template>

<script setup>
import { ref, computed } from 'vue'
import * as api from '../utils/api.js'
import { session, clearRelogin } from '../utils/session.js'
import { initDevice } from '../utils/device.js'

const password = ref('')
const showPwd = ref(false)
const loading = ref(false)
const err = ref('')

const username = computed(() => api.settings().username || 'admin')
const serverDisplay = computed(() => {
  const s = api.settings()
  return s.baseURL || (s.lanIP ? `http://${s.lanIP}:${s.lanPort || 8080}` : '未配置服务器地址')
})

async function submit() {
  if (!password.value || loading.value) return
  loading.value = true
  err.value = ''
  try {
    await api.login(api.settings().baseURL, username.value, password.value)
    clearRelogin()
    password.value = ''
    uni.showToast({ title: '登录成功' })
    // 重新上报设备，让服务端刷新 last_seen_at
    try { await initDevice() } catch (e) { /* ignore */ }
    uni.$emit('naspic:relogin-success')
  } catch (e) {
    err.value = e.message || '登录失败'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.rl-mask {
  position: fixed; left: 0; top: 0; right: 0; bottom: 0;
  background: rgba(0, 0, 0, 0.45);
  display: flex; align-items: center; justify-content: center;
  z-index: 9999; padding: 40rpx;
}
.rl-card {
  width: 100%; max-width: 620rpx; background: #fff;
  border-radius: 24rpx; padding: 40rpx 36rpx 36rpx;
  box-shadow: 0 12rpx 40rpx rgba(0, 0, 0, 0.18);
}
.rl-head { display: flex; align-items: center; margin-bottom: 16rpx; }
.rl-dot {
  width: 16rpx; height: 16rpx; border-radius: 50%;
  background: #e53935; margin-right: 14rpx;
}
.rl-title { font-size: 34rpx; font-weight: 700; color: #303133; }
.rl-desc { font-size: 25rpx; color: #909399; line-height: 1.7; display: block; margin-bottom: 24rpx; }
.rl-field {
  display: flex; align-items: center; padding: 16rpx 0;
  border-bottom: 1rpx solid #f2f3f5;
}
.rl-label { font-size: 26rpx; color: #909399; width: 110rpx; }
.rl-val {
  flex: 1; font-size: 26rpx; color: #303133;
  overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
}
.rl-input {
  height: 84rpx; border: 1rpx solid #dcdfe6; border-radius: 12rpx;
  padding: 0 22rpx; font-size: 28rpx; margin-top: 24rpx;
}
.rl-eye { text-align: right; font-size: 24rpx; color: #2979ff; padding: 12rpx 4rpx; }
.rl-btn {
  margin-top: 8rpx; background: #2979ff; color: #fff;
  border-radius: 42rpx; font-size: 30rpx; height: 88rpx; line-height: 88rpx;
}
.rl-btn[disabled] { opacity: 0.5; }
.rl-err { display: block; color: #e53935; font-size: 24rpx; margin-top: 16rpx; text-align: center; }
</style>
