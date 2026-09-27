/**
 * 文件扫描
 * - scanSystemAlbum：系统相册（Android 走 MediaStore，iOS 走 Photos 框架封装）
 * - scanFolder   ：自定义文件夹（Android SAF / 绝对路径，iOS 安全书签目录）
 *
 * 输出统一结构：
 *   { id, path, name, size, mtime, type: 'image'|'video' }
 *
 * 注意：本文件是参考实现，系统相册枚举依赖原生能力。
 * 若项目接入了原生插件（如 Ba-Media / 自研插件），只需替换 scanSystemAlbum 内部实现，
 * 保持返回结构不变即可，engine 与 uploader 无需改动。
 */

const VIDEO_EXT = ['mp4', 'mov', 'm4v', 'mkv', 'avi', 'webm', '3gp']
const IMAGE_EXT = ['jpg', 'jpeg', 'png', 'gif', 'webp', 'heic', 'heif', 'bmp']

function extOf(name) {
  const i = String(name).lastIndexOf('.')
  return i < 0 ? '' : name.slice(i + 1).toLowerCase()
}

function typeOf(name) {
  const e = extOf(name)
  if (IMAGE_EXT.includes(e)) return 'image'
  if (VIDEO_EXT.includes(e)) return 'video'
  return 'other'
}

// ---------- 系统相册 ----------

/**
 * 扫描系统相册（增量：只返回 mtime > since 的媒体）
 * @param {{since?:number, types?:string[]}} opt
 */
export function scanSystemAlbum(opt = {}) {
  const since = opt.since || 0
  const types = opt.types || ['image', 'video']
  // #ifdef APP-PLUS
  if (plus.os.name === 'Android') return scanAndroidMediaStore(since, types)
  return scanIOSPhotos(since, types)
  // #endif
  // #ifndef APP-PLUS
  return Promise.resolve([])
  // #endif
}

/**
 * Android：通过 MediaStore 查询
 * - projection 传 null 返回所有列，避免创建 Java String[] 的兼容性问题
 * - Android 10+ 的 _data 列可能为 null，回退用 _id 构建 content URI
 */
function scanAndroidMediaStore(since, types) {
  return new Promise((resolve) => {
    try {
      const MediaStore = plus.android.importClass('android.provider.MediaStore')
      const ContentUris = plus.android.importClass('android.content.ContentUris')
      const main = plus.android.runtimeMainActivity()
      const resolver = plus.android.invoke(main, 'getContentResolver')
      const out = []
      console.log('[scanner] resolver=' + (resolver ? 'ok' : 'null'))

      // Java boolean/long → JS 原生类型
      const toBool = (v) => v === true || v === 1 || v === '1' || (v && v.booleanValue && v.booleanValue())
      const toNum = (v) => {
        if (v == null) return 0
        if (typeof v === 'number') return v
        if (typeof v === 'string') return parseInt(v, 10) || 0
        if (v.longValue) return v.longValue()
        if (v.intValue) return v.intValue()
        return Number(v) || 0
      }

      const getCol = (cursor, name) => {
        const idx = plus.android.invoke(cursor, 'getColumnIndex', name)
        return Number(idx) >= 0 ? Number(idx) : -1
      }

      const query = (baseUri, kind) => {
        const cursor = plus.android.invoke(
          resolver, 'query', baseUri, null, null, null, 'date_modified DESC'
        )
        if (!cursor) {
          console.warn('[scanner] query 返回 null (kind=' + kind + ')，可能缺少媒体权限')
          return
        }
        try {
          const colData = getCol(cursor, '_data')
          const colName = getCol(cursor, '_display_name')
          const colSize = getCol(cursor, '_size')
          const colMtime = getCol(cursor, 'date_modified')
          const colId = getCol(cursor, '_id')
          console.log('[scanner] 列索引 _data=' + colData + ' _name=' + colName + ' _id=' + colId)

          let rowCount = 0
          while (toBool(plus.android.invoke(cursor, 'moveToNext'))) {
            rowCount++
            const name = colName >= 0 ? plus.android.invoke(cursor, 'getString', colName) : ''
            const size = colSize >= 0 ? toNum(plus.android.invoke(cursor, 'getLong', colSize)) : 0
            const mtime = colMtime >= 0 ? toNum(plus.android.invoke(cursor, 'getLong', colMtime)) : 0
            if (since > 0 && mtime <= since) continue
            let path = colData >= 0 ? plus.android.invoke(cursor, 'getString', colData) : null
            if (!path && colId >= 0) {
              const id = toNum(plus.android.invoke(cursor, 'getLong', colId))
              const uri = ContentUris.withAppendedId(baseUri, id)
              path = uri.toString()
            }
            if (!path) continue
            const isContent = path.indexOf('content://') === 0
            if (!isContent && path.indexOf('file://') !== 0) path = 'file://' + path
            out.push({ id: path, path, name: name || path.split('/').pop(), size, mtime, type: kind, saf: isContent })
            if (rowCount <= 3) console.log('[scanner] 样本 ' + rowCount + ': ' + name + ' size=' + size)
          }
          console.log('[scanner] ' + kind + ' 共 ' + rowCount + ' 行，入库 ' + out.length + ' 个')
        } finally {
          plus.android.invoke(cursor, 'close')
        }
      }

      if (types.includes('image')) query(MediaStore.Images.Media.EXTERNAL_CONTENT_URI, 'image')
      if (types.includes('video')) query(MediaStore.Video.Media.EXTERNAL_CONTENT_URI, 'video')
      console.log('[scanner] MediaStore 扫描到 ' + out.length + ' 个文件')
      resolve(out)
    } catch (e) {
      console.warn('[scanner] MediaStore 查询失败:', e.message, e.stack)
      resolve([])
    }
  })
}

/**
 * iOS：需通过原生 Photos 框架（PHAsset）枚举。
 * 推荐使用原生插件；此处返回空数组并在控制台提示，避免误报「同步完成」。
 */
function scanIOSPhotos(since, types) {
  console.warn('[scanner] iOS 系统相册枚举需要原生插件支持')
  return Promise.resolve([])
}

// ---------- 自定义文件夹 ----------

/**
 * 遍历目录（可递归）
 * @param {string} uri 目录路径或 SAF tree URI（content://...）
 * @param {{includeSubdir?:boolean, types?:string[]}} opt
 */
export function scanFolder(uri, opt = {}) {
  const includeSubdir = opt.includeSubdir !== false
  const types = opt.types || ['image', 'video']
  const result = []

  // #ifdef APP-PLUS
  if (uri.indexOf('content://') === 0) {
    // SAF content:// URI：用 DocumentFile 遍历
    return walkDocumentTree(uri, includeSubdir, types, result)
  }
  // #endif

  return new Promise((resolve, reject) => {
    // #ifdef APP-PLUS
    plus.io.resolveLocalFileSystemURL(
      uri,
      (entry) => walk(entry, includeSubdir, types, result, resolve),
      (e) => reject(new Error('无法访问目录: ' + uri))
    )
    // #endif
    // #ifndef APP-PLUS
    resolve([])
    // #endif
  })
}

// SAF content:// URI 遍历（用 DocumentsContract，不依赖 AndroidX DocumentFile）
function walkDocumentTree(uri, includeSubdir, types, result) {
  return new Promise((resolve) => {
    // #ifdef APP-PLUS
    try {
      const main = plus.android.runtimeMainActivity()
      const Uri = plus.android.importClass('android.net.Uri')
      const DocumentsContract = plus.android.importClass('android.provider.DocumentsContract')
      const resolver = plus.android.invoke(main, 'getContentResolver')
      const treeUri = Uri.parse(uri)
      const rootDocId = DocumentsContract.getTreeDocumentId(treeUri)
      const MIME_DIR = 'vnd.android.document/directory'

      const getCol = (cursor, name) => {
        const idx = plus.android.invoke(cursor, 'getColumnIndex', name)
        return idx >= 0 ? idx : -1
      }

      // 递归遍历：传入父文档的 documentId
      const traverse = (parentDocId) => {
        const childrenUri = DocumentsContract.buildChildDocumentsUriUsingTree(treeUri, parentDocId)
        // projection=null 返回所有列
        const cursor = plus.android.invoke(resolver, 'query', childrenUri, null, null, null, null)
        if (!cursor) return
        try {
          const colId = getCol(cursor, 'document_id')
          const colName = getCol(cursor, '_display_name')
          const colMime = getCol(cursor, 'mime_type')
          const colSize = getCol(cursor, '_size')
          const colMtime = getCol(cursor, 'last_modified')
          while (plus.android.invoke(cursor, 'moveToNext')) {
            const docId = colId >= 0 ? plus.android.invoke(cursor, 'getString', colId) : ''
            const name = colName >= 0 ? plus.android.invoke(cursor, 'getString', colName) : ''
            const mime = colMime >= 0 ? plus.android.invoke(cursor, 'getString', colMime) : ''
            if (!name) continue
            if (mime === MIME_DIR) {
              if (includeSubdir && docId) traverse(docId)
            } else {
              const t = typeOf(name)
              if (t === 'other' || !types.includes(t)) continue
              const docUri = DocumentsContract.buildDocumentUriUsingTree(treeUri, docId)
              const size = colSize >= 0 ? plus.android.invoke(cursor, 'getLong', colSize) : 0
              const mtime = colMtime >= 0 ? plus.android.invoke(cursor, 'getLong', colMtime) : 0
              result.push({
                id: docUri.toString(),
                path: docUri.toString(),
                name,
                size: size || 0,
                mtime: Math.floor((mtime || 0) / 1000),
                type: t,
                saf: true,
              })
            }
          }
        } finally {
          plus.android.invoke(cursor, 'close')
        }
      }

      traverse(rootDocId)
      console.log('[scanner] SAF 目录扫描到 ' + result.length + ' 个文件')
      resolve(result)
    } catch (e) {
      console.warn('[scanner] SAF 遍历失败:', e.message)
      resolve([])
    }
    // #endif
    // #ifndef APP-PLUS
    resolve([])
    // #endif
  })
}

function walk(dirEntry, includeSubdir, types, result, resolve) {
  const reader = dirEntry.createReader()
  const pending = []
  let finished = false

  const done = () => {
    if (finished) return
    finished = true
    if (pending.length === 0) resolve(result)
  }

  reader.readEntries(
    (entries) => {
      if (!entries || entries.length === 0) return done()
      entries.forEach((entry) => {
        if (entry.isFile) {
          const name = entry.name
          const t = typeOf(name)
          if (t === 'other' || !types.includes(t)) return
          pending.push(entry)
          entry.getMetadata(
            (meta) => {
              result.push({
                id: entry.fullPath || entry.toURL(),
                path: entry.fullPath || entry.toURL(),
                name,
                size: meta.size || 0,
                mtime: Math.floor((meta.modificationTime || Date.now()) / 1000),
                type: t,
              })
              pending.splice(pending.indexOf(entry), 1)
              if (pending.length === 0 && finished) resolve(result)
            },
            () => {
              pending.splice(pending.indexOf(entry), 1)
              if (pending.length === 0 && finished) resolve(result)
            }
          )
        } else if (entry.isDirectory && includeSubdir) {
          pending.push(entry)
          walk(entry, includeSubdir, types, result, () => {
            pending.splice(pending.indexOf(entry), 1)
            if (pending.length === 0 && finished) resolve(result)
          })
        }
      })
      // 本层读取完毕
      setTimeout(done, 0)
    },
    () => done()
  )
}

/** 计算新的增量游标（秒） */
export function nextCursor(files, old) {
  let max = old || 0
  files.forEach((f) => { if (f.mtime > max) max = f.mtime })
  return max
}
