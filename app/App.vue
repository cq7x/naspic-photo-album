<script>
import { requestAlbumPermission } from './utils/permission.js'
import * as api from './utils/api.js'
import ReLoginModal from './components/ReLoginModal.vue'

export default {
  components: { ReLoginModal },
  onLaunch() {
    // #ifdef APP-PLUS
    // 请求相册权限（Android 运行时权限，必须在读取相册前授权）
    requestAlbumPermission().then((ok) => {
      if (!ok) {
        uni.showModal({
          title: '需要相册权限',
          content: 'Naspic 需要访问相册才能备份照片和视频。请在系统设置中授予权限。',
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
      }
    })

    // 引导关闭电池优化（仅提示一次）
    const asked = uni.getStorageSync('naspic.batteryAsked')
    if (plus.os.name === 'Android' && !asked) {
      uni.setStorageSync('naspic.batteryAsked', 1)
      this.guideBattery()
    }
    // #endif

    // 启动即静默校验登录态：旧令牌（服务器重启/重部署）会直接触发全局重新登录弹窗，
    // 不用等用户点同步才报错。网络不通不算失效，不会误清 token。
    if (uni.getStorageSync('naspic.token')) {
      api.ensureAuth().catch(() => {})
    }
  },
  methods: {
    guideBattery() {
      uni.showModal({
        title: '保持后台同步',
        content: '为保证照片在后台自动备份，建议将 Naspic 加入电池优化白名单并允许自启动。',
        confirmText: '去设置',
        cancelText: '稍后',
        success: (r) => {
          if (!r.confirm) return
          try {
            const main = plus.android.runtimeMainActivity()
            const Intent = plus.android.importClass('android.content.Intent')
            const Settings = plus.android.importClass('android.provider.Settings')
            const intent = new Intent(Settings.ACTION_REQUEST_IGNORE_BATTERY_OPTIMIZATIONS)
            intent.setData(plus.android.importClass('android.net.Uri').parse('package:' + main.getPackageName()))
            main.startActivity(intent)
          } catch (e) {
            uni.showToast({ title: '请手动在系统设置中关闭电池优化', icon: 'none' })
          }
        },
      })
    },
  },
}
</script>

<template>
  <ReLoginModal />
</template>

<style>
page {
  background: #f5f7fa;
  font-size: 28rpx;
}
</style>
