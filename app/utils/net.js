/**
 * 网络状态工具
 * 提供：网络类型判定（WiFi / 移动网络 / 离线）与变化订阅。
 */

const listeners = new Set()
let current = { isWifi: false, isConnected: true, type: 'unknown' }

function normalize(t) {
  if (t === 'wifi') return { isWifi: true, isConnected: true, type: 'wifi' }
  if (t === 'none') return { isWifi: false, isConnected: false, type: 'none' }
  return { isWifi: false, isConnected: true, type: t || 'cellular' }
}

/** 初始化网络监听（App.vue 中调用一次） */
export function initNetwork() {
  refresh()
  // #ifdef APP-PLUS
  // plus.globalEvent 在部分环境下不稳定，额外用定时兜底
  setInterval(refresh, 15000)
  // #endif
  uni.onNetworkStatusChange((res) => {
    current = normalize(res.networkType)
    listeners.forEach((fn) => fn(current))
  })
}

function refresh() {
  uni.getNetworkType({
    success: (res) => { current = normalize(res.networkType) },
    fail: () => {},
  })
}

export function getNetwork() { return current }
export function isWifi() { return current.isWifi }
export function isOnline() { return current.isConnected }

/** 订阅网络变化，返回取消函数 */
export function onNetworkChange(fn) {
  listeners.add(fn)
  return () => listeners.delete(fn)
}

/** 等待 WiFi（配合 wifiOnly 策略） */
export function waitForWifi(timeoutMs = 0) {
  return new Promise((resolve) => {
    if (isWifi()) return resolve(true)
    let timer = null
    const off = onNetworkChange((n) => {
      if (n.isWifi) {
        if (timer) clearTimeout(timer)
        off()
        resolve(true)
      }
    })
    if (timeoutMs > 0) {
      timer = setTimeout(() => { off(); resolve(false) }, timeoutMs)
    }
  })
}
