/**
 * 运行时权限请求
 * Android 6+ 必须在运行时请求危险权限，manifest 声明不够。
 */

// 检查是否已授权（用 Context.checkSelfPermission，Android 框架自带，不依赖 AndroidX）
export function hasPermission(perm) {
  // #ifdef APP-PLUS
  try {
    const main = plus.android.runtimeMainActivity()
    // Context.checkSelfPermission 是 API 23+ 方法，PERMISSION_GRANTED = 0
    const res = plus.android.invoke(main, 'checkSelfPermission', perm)
    return Number(res) === 0
  } catch (e) {
    console.warn('[perm] checkSelfPermission 失败:', e.message)
    return false
  }
  // #endif
  // #ifndef APP-PLUS
  return true
  // #endif
}

// 请求一组权限，返回 { granted: [已授权], denied: [拒绝] }
export function requestPermissions(perms) {
  return new Promise((resolve) => {
    // #ifdef APP-PLUS
    if (plus.os.name !== 'Android') return resolve({ granted: perms, denied: [] })
    try {
      // UniApp 封装的原生权限请求，回调可靠
      plus.android.requestPermissions(
        perms,
        (e) => {
          const granted = []
          const denied = []
          // e.deniedPresent / e.granted 是已存在的数组
          if (e.granted) granted.push(...e.granted)
          if (e.deniedPresent) denied.push(...e.deniedPresent)
          if (e.deniedAlways) denied.push(...e.deniedAlways)
          resolve({ granted, denied })
        },
        (e) => {
          console.warn('[perm] requestPermissions error:', e)
          resolve({ granted: [], denied: perms })
        }
      )
    } catch (e) {
      console.warn('[perm] requestPermissions 失败:', e.message)
      resolve({ granted: [], denied: perms })
    }
    // #endif
    // #ifndef APP-PLUS
    resolve({ granted: perms, denied: [] })
    // #endif
  })
}

// 请求相册权限（根据 Android 版本自动选择）
export async function requestAlbumPermission() {
  // #ifdef APP-PLUS
  if (plus.os.name !== 'Android') return true

  const Build = plus.android.importClass('android.os.Build')
  const sdk = Build.VERSION.SDK_INT
  let perms
  if (sdk >= 33) {
    // Android 13+
    perms = ['android.permission.READ_MEDIA_IMAGES', 'android.permission.READ_MEDIA_VIDEO']
  } else {
    perms = ['android.permission.READ_EXTERNAL_STORAGE']
  }

  // 已全部授权则直接返回
  const need = perms.filter((p) => !hasPermission(p))
  if (need.length === 0) return true

  const r = await requestPermissions(need)
  return r.denied.length === 0
  // #endif
  // #ifndef APP-PLUS
  return true
  // #endif
}
