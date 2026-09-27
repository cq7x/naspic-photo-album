// #ifdef APP-PLUS
const Intent = plus.android.importClass('android.content.Intent')
const DocumentsContract = plus.android.importClass('android.provider.DocumentsContract')
// #endif

/**
 * 打开系统目录选择器，获取可持久化授权的文件夹
 * Android：SAF ACTION_OPEN_DOCUMENT_TREE + takePersistableUriPermission
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
    intent.addFlags(Intent.FLAG_GRANT_WRITE_URI_PERMISSION)
    intent.addFlags(Intent.FLAG_GRANT_PERSISTABLE_URI_PERMISSION)

    const CODE = 1001

    // 重写 Activity 的 onActivityResult 接收结果（用完恢复原方法）
    const orig = main.onActivityResult
    main.onActivityResult = function (requestCode, resultCode, data) {
      if (requestCode === CODE) {
        main.onActivityResult = orig || function () {}
        if (resultCode !== -1 || !data) return reject(new Error('已取消'))
        try {
          const uri = data.getData()
          const flags = data.getFlags() &
            (Intent.FLAG_GRANT_READ_URI_PERMISSION | Intent.FLAG_GRANT_WRITE_URI_PERMISSION)
          const resolver = plus.android.invoke(main, 'getContentResolver')
          plus.android.invoke(resolver, 'takePersistableUriPermission', uri, flags)
          // 用 DocumentsContract.getTreeDocumentId 获取目录名（不依赖 AndroidX DocumentFile）
          let name = uri.toString()
          try {
            const docId = DocumentsContract.getTreeDocumentId(uri)
            const docUri = DocumentsContract.buildDocumentUriUsingTree(uri, docId)
            const proj = ['_display_name']
            const JStringArray = plus.android.importClass('[Ljava.lang.String;')
            const ja = plus.android.invoke(JStringArray, 'newInstance', 1)
            ja[0] = '_display_name'
            const cursor = plus.android.invoke(resolver, 'query', docUri, ja, null, null, null)
            if (cursor && plus.android.invoke(cursor, 'moveToFirst')) {
              name = plus.android.invoke(cursor, 'getString', 0) || name
              plus.android.invoke(cursor, 'close')
            }
          } catch (e) { /* 取不到名字就用 URI */ }
          resolve({ uri: uri.toString(), path: name })
        } catch (e) {
          reject(new Error('处理选择结果失败: ' + e.message))
        }
      }
    }
    main.startActivityForResult(intent, CODE)
    // #endif

    // #ifndef APP-PLUS
    reject(new Error('仅 App 端支持目录选择'))
    // #endif
  })
}
