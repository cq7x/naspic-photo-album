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
 * Android：通过 MediaStore 查询，效率远高于遍历文件系统
 * 依赖 plus.android 运行时反射调用，此处给出可直接使用的实现骨架
 */
function scanAndroidMediaStore(since, types) {
  return new Promise((resolve) => {
    try {
      const Context = plus.android.importClass('android.provider.MediaStore')
      const MainActivity = plus.android.runtimeMainActivity()
      const resolver = MainActivity.getContentResolver()
      const out = []

      const query = (uri, kind) => {
        const projection = ['_data', '_display_name', '_size', 'date_modified', 'mime_type']
        const selection = since > 0 ? 'date_modified > ?' : null
        const args = since > 0 ? [String(since)] : null
        const cursor = plus.android.invoke(resolver, 'query', uri, projection, selection, args, 'date_modified DESC')
        if (!cursor) return
        while (plus.android.invoke(cursor, 'moveToNext')) {
          const path = plus.android.invoke(cursor, 'getString', 0)
          const name = plus.android.invoke(cursor, 'getString', 1)
          const size = plus.android.invoke(cursor, 'getLong', 2)
          const mtime = plus.android.invoke(cursor, 'getLong', 3)
          out.push({ id: path, path, name, size, mtime, type: kind })
        }
        plus.android.invoke(cursor, 'close')
      }

      if (types.includes('image')) query(Context.Images.Media.EXTERNAL_CONTENT_URI, 'image')
      if (types.includes('video')) query(Context.Video.Media.EXTERNAL_CONTENT_URI, 'video')
      resolve(out)
    } catch (e) {
      console.warn('[scanner] MediaStore 查询失败，回退为空:', e)
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
 * @param {string} uri 目录路径或 SAF tree URI 解析后的本地路径
 * @param {{includeSubdir?:boolean, types?:string[]}} opt
 */
export function scanFolder(uri, opt = {}) {
  const includeSubdir = opt.includeSubdir !== false
  const types = opt.types || ['image', 'video']
  const result = []

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
