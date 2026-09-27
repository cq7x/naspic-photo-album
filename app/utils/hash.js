/**
 * 文件哈希工具
 * 策略：<20MB 全量 SHA-256；≥20MB 取「头 1MB + 尾 1MB + 文件大小」快速哈希。
 * 目的：手机上算哈希是 CPU/IO 大户，大文件必须采样，否则相册同步会发烫且极慢。
 */
import { sha256 } from './sha256.js'

const SEG = 1 << 20 // 1MB
export const HASH_FULL_MAX = 20 * SEG // 20MB

/**
 * 读取文件指定区间（App 端走 plus.io，H5 端走 fetch Range）
 * @param {string} path 本地文件路径
 * @param {number} start
 * @param {number} len
 * @returns {Promise<Uint8Array>}
 */
function readRange(path, start, len) {
  return new Promise((resolve, reject) => {
    // #ifdef APP-PLUS
    try {
      // 把 file:// 前缀去掉，用 Java FileInputStream 直接读取（plus.io 回调不稳定）
      let realPath = path
      if (realPath.indexOf('file://') === 0) realPath = realPath.slice(7)

      // SAF content:// URI：用 ContentResolver + ParcelFileDescriptor 读取
      if (path.indexOf('content://') === 0) {
        const main = plus.android.runtimeMainActivity()
        const Uri = plus.android.importClass('android.net.Uri')
        const ReflectArray = plus.android.importClass('java.lang.reflect.Array')
        const Byte = plus.android.importClass('java.lang.Byte')
        const resolver = plus.android.invoke(main, 'getContentResolver')
        const uri = Uri.parse(path)
        const pfd = plus.android.invoke(resolver, 'openFileDescriptor', uri, 'r')
        if (!pfd) return reject(new Error('无法打开 SAF 文件'))
        const size = plus.android.invoke(pfd, 'getStatSize')
        const end = Math.min(start + len, size)
        const count = end - start
        const FileInputStream = plus.android.importClass('java.io.FileInputStream')
        const fd = plus.android.invoke(pfd, 'getFileDescriptor')
        const fis = new FileInputStream(fd)
        if (start > 0) plus.android.invoke(fis, 'skip', start)
        const jbuf = plus.android.invoke(ReflectArray, 'newInstance', Byte.TYPE, count)
        let offset = 0
        while (offset < count) {
          const n = plus.android.invoke(fis, 'read', jbuf, offset, count - offset)
          if (n <= 0) break
          offset += n
        }
        plus.android.invoke(fis, 'close')
        plus.android.invoke(pfd, 'close')
        const out = new Uint8Array(count)
        for (let i = 0; i < count; i++) {
          out[i] = plus.android.invoke(ReflectArray, 'getByte', jbuf, i) & 0xff
        }
        resolve(out)
        return
      }

      // 本地文件路径：直接用 Java FileInputStream（同步调用，避免 plus.io 回调不触发）
      const File = plus.android.importClass('java.io.File')
      const FileInputStream = plus.android.importClass('java.io.FileInputStream')
      const ReflectArray = plus.android.importClass('java.lang.reflect.Array')
      const Byte = plus.android.importClass('java.lang.Byte')

      const f = new File(realPath)
      if (!plus.android.invoke(f, 'exists')) {
        return reject(new Error('文件不存在: ' + realPath))
      }
      const fileSize = plus.android.invoke(f, 'length')
      const end = Math.min(start + len, fileSize)
      const count = end - start
      if (count <= 0) {
        resolve(new Uint8Array(0))
        return
      }

      const fis = new FileInputStream(f)
      if (start > 0) plus.android.invoke(fis, 'skip', start)
      const jbuf = plus.android.invoke(ReflectArray, 'newInstance', Byte.TYPE, count)
      let offset = 0
      while (offset < count) {
        const n = plus.android.invoke(fis, 'read', jbuf, offset, count - offset)
        if (n <= 0) break
        offset += n
      }
      plus.android.invoke(fis, 'close')
      const out = new Uint8Array(count)
      for (let i = 0; i < count; i++) {
        out[i] = plus.android.invoke(ReflectArray, 'getByte', jbuf, i) & 0xff
      }
      resolve(out)
    } catch (err) {
      reject(err)
    }
    // #endif

    // #ifdef H5
    fetch(path)
      .then((r) => r.arrayBuffer())
      .then((ab) => resolve(new Uint8Array(ab.slice(start, start + len))))
      .catch(reject)
    // #endif
  })
}

/** 获取文件大小 */
export function fileSize(path) {
  return new Promise((resolve) => {
    // #ifdef APP-PLUS
    try {
      // SAF content:// URI
      if (path.indexOf('content://') === 0) {
        const main = plus.android.runtimeMainActivity()
        const Uri = plus.android.importClass('android.net.Uri')
        const resolver = plus.android.invoke(main, 'getContentResolver')
        const pfd = plus.android.invoke(resolver, 'openFileDescriptor', Uri.parse(path), 'r')
        if (!pfd) return resolve(0)
        const size = plus.android.invoke(pfd, 'getStatSize')
        plus.android.invoke(pfd, 'close')
        resolve(size || 0)
        return
      }
      // 本地文件：用 Java File.length() 直接获取
      let realPath = path
      if (realPath.indexOf('file://') === 0) realPath = realPath.slice(7)
      const File = plus.android.importClass('java.io.File')
      const f = new File(realPath)
      if (!plus.android.invoke(f, 'exists')) return resolve(0)
      resolve(plus.android.invoke(f, 'length') || 0)
    } catch (e) {
      resolve(0)
    }
    // #endif
    // #ifndef APP-PLUS
    resolve(0)
    // #endif
  })
}

/**
 * 计算文件哈希
 * @returns {Promise<{hash:string, algo:'sha256'|'fast', size:number}>}
 */
export async function hashFile(path, size) {
  if (!size) size = await fileSize(path)
  if (size > 0 && size <= HASH_FULL_MAX) {
    const buf = await readRange(path, 0, size)
    return { hash: sha256(buf), algo: 'sha256', size }
  }
  // 大文件：头尾采样 + 文件大小，兼顾速度与碰撞概率
  const head = await readRange(path, 0, SEG)
  const tail = await readRange(path, Math.max(0, size - SEG), SEG)
  const merged = new Uint8Array(head.length + tail.length + 8)
  merged.set(head, 0)
  merged.set(tail, head.length)
  const dv = new DataView(merged.buffer)
  dv.setUint32(merged.length - 8, Math.floor(size / 4294967296))
  dv.setUint32(merged.length - 4, size >>> 0)
  return { hash: sha256(merged), algo: 'fast', size }
}

/** 读取一个分片（供上传使用） */
export function readChunk(path, index, chunkSize) {
  return readRange(path, index * chunkSize, chunkSize)
}
