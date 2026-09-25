/**
 * 设备标识与注册
 * 首次启动生成 device_uuid 并持久化；每次启动/同步前上报服务端。
 */
import * as api from './api.js'

const KEY_UUID = 'naspic.device_uuid'
const KEY_DEVICE_ID = 'naspic.device_id'

export function deviceUUID() {
  let id = uni.getStorageSync(KEY_UUID)
  if (!id) {
    id = 'd' + Date.now().toString(36) + Math.random().toString(36).slice(2, 10)
    uni.setStorageSync(KEY_UUID, id)
  }
  return id
}

export function localDeviceId() {
  return uni.getStorageSync(KEY_DEVICE_ID) || 0
}

/** 注册/上报设备信息（失败不影响使用，下次再试） */
export async function initDevice() {
  // #ifdef APP-PLUS
  const payload = {
    device_uuid: deviceUUID(),
    platform: plus.os.name === 'iOS' ? 2 : 1,
    app_version: plus.runtime.version,
  }
  try {
    const info = uni.getSystemInfoSync()
    payload.device_name = `${info.deviceBrand || ''} ${info.deviceModel || ''}`.trim()
  } catch (e) { /* 忽略 */ }

  try {
    const res = await api.registerDevice(payload)
    if (res && res.id) uni.setStorageSync(KEY_DEVICE_ID, res.id)
  } catch (e) {
    console.warn('[device] 注册失败，稍后重试:', e.message)
  }
  // #endif
  // #ifndef APP-PLUS
  // H5/小程序端：仅生成 UUID，不注册设备
  console.log('[device] 非App环境，跳过设备注册')
  // #endif
}
