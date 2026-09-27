/**
 * 全局登录会话状态（轻量响应式，跨页面共享）
 *  - needRelogin: 服务端令牌失效（401）后置 true，由全局 ReLoginModal 弹窗接管
 *  - 用模块级 reactive 而不是 uni.$emit 广播，避免「App 启动时页面还没挂载监听」的时序问题
 */
import { reactive } from 'vue'

export const session = reactive({
  needRelogin: false,
})

export function triggerRelogin() {
  session.needRelogin = true
}

export function clearRelogin() {
  session.needRelogin = false
}
