<script>
import { initNetwork } from './utils/net.js'

export default {
  onLaunch() {
    initNetwork()
    // #ifdef APP-PLUS
    // Android：转发目录选择器的结果（自定义基座需在原生层调用 forwardActivityResult）
    plus.globalEvent.addEventListener('newintent', () => {})
    // 引导关闭电池优化（仅提示一次）
    const asked = uni.getStorageSync('naspic.batteryAsked')
    if (plus.os.name === 'Android' && !asked) {
      uni.setStorageSync('naspic.batteryAsked', 1)
      this.guideBattery()
    }
    // #endif
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

<style>
page {
  background: #f5f7fa;
  font-size: 28rpx;
}
</style>
