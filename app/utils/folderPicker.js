// #ifdef APP-PLUS
const Intent = plus.android.importClass('android.content.Intent')
const Uri = plus.android.importClass('android.net.Uri')
const DocumentFile = plus.android.importClass('androidx.documentfile.provider.DocumentFile')
// #endif

/**
 * 打开系统目录选择器，获取可持久化授权的文件夹
 * Android：SAF ACTION_OPEN_DOCUMENT_TREE + takePersistableUriPermission
 * iOS    ：需原生插件（UIDocumentPickerViewController + security-scoped bookmark）
 *
 * @returns {Promise<{uri:string, path:string}>}
 */
export function pickFolder() {
  return new Promise((resolve, reject) => {
    // #ifdef APP-PLUS
    if (plus.os.name !== 'Android') {
      return reject(new Error('iOS 目录选择需接入原生插件'))
    }
    const main = plus.android.runtimeMainActivity()
    const intent = new Intent(Intent.ACTION_OPEN_DOCUMENT_TREE)
    intent.addFlags(Intent.FLAG_GRANT_READ_URI_PERMISSION)
    intent.addFlags(Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION)

    const CODE = 1001
    main.startActivityForResult(intent, CODE)

    // 结果通过全局回调接收（在 App.vue 的 onActivityResult 中转发）
    globalThis.__naspicFolderCallback = (resultCode, data) => {
      globalThis.__naspicFolderCallback = null
      if (resultCode !== -1 || !data) return reject(new Error('已取消'))
      const uri = data.getData()
      // 持久化授权：重启后依然可访问
      const flags = data.getFlags() & (Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION)
      main.getContentResolver().takePersistableUriPermission(uri, flags)

      const docFile = DocumentFile.fromTreeUri(main, uri)
      resolve({
        uri: uri.toString(),
        path: docFile ? docFile.getName() : uri.toString(),
      })
    }
    // #endif

    // #ifndef APP-PLUS
    reject(new Error('仅 App 端支持目录选择'))
    // #endif
  })
}

/**
 * 在 App.vue 中转发 onActivityResult 到本模块
 * 用法：
 *   // #ifdef APP-PLUS
 *   plus.globalEvent.addEventListener('newintent', ...)
 *   // #endif
 *   或在自定义基座里重写 onActivityResult 后调用 forwardActivityResult
 */
export function forwardActivityResult(resultCode, data) {
  if (typeof globalThis.__naspicFolderCallback === 'function') {
    globalThis.__naspicFolderCallback(resultCode, data)
  }
}
