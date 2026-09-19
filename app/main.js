import { createSSRApp } from 'vue'
import App from './App.vue'
import { initNetwork } from './utils/net.js'
import { initDevice } from './utils/device.js'

export function createApp() {
  const app = createSSRApp(App)
  initNetwork()   // 网络状态监听（WiFi 策略依赖）
  initDevice()    // 设备注册（首次生成 device_uuid）
  return { app }
}
