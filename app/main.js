import { createSSRApp } from 'vue'
import App from './App.vue'
import { initNetwork } from './utils/net.js'

export function createApp() {
  const app = createSSRApp(App)
  initNetwork()   // 网络状态监听（WiFi 策略依赖）
  // initDevice 改到登录成功后调用（需要 token 才能注册设备）
  return { app }
}
