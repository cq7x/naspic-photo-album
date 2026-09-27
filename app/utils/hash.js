/**
 * 文件哈希工具
 * 策略：<20MB 全量 SHA-256；≥20MB 取「头 1MB + 尾 1MB + 文件大小」快速哈希。
 * 目的：手机上算哈希是 CPU/IO 大户，大文件必须采样，否则相册同步会发烫且极慢。
 *
 * ⚠️ 读取实现（v1.3 重写）：
 * 旧版用 plus.android.invoke(fis,'read',jbuf,...) 填 Java 数组后逐字节 getByte 取回，
 * 桥接对大数组不可靠 —— 读失败时 Java 数组保持零初始化，产出「长度正确的全零流」，
 * 哈希/上传全程自洽，服务端落盘全零文件（2026-09-27 事故根因）。
 * 现改为：Java 端分块读入 → Base64.encodeToString 编码成字符串过桥 → JS 解码。
 * 字符串跨桥是可靠的；任何读失败都会 reject 报错，绝不静默返回全零。
 */
import { sha256 } from './sha256.js'

const SEG = 1 << 20 // 1MB
export const HASH_FULL_MAX = 20 * SEG // 20MB

const B64A = 'ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/'

/** base64 → Uint8Array（不依赖 atob，App 端 v8/JSC 环境均可用） */
function b64Decode(s) {
  let buf = 0, bits = 0
  const out = new Uint8Array(Math.floor((s.length * 3) / 4))
  let o = 0
  for (let i = 0; i < s.length; i++) {
    const c = B64A.indexOf(s.charAt(i))
    if (c < 0) continue // 跳过 '=' 等非表字符
    buf = (buf << 6) | c
    bits += 6
    if (bits >= 8) {
      bits -= 8
      out[o++] = (buf >> bits) & 0xff
    }
  }
  return out.subarray(0, o)
}

const BLOCK = 1 << 18 // 256KB，单次过桥的块大小

/**
 * 从已打开的 FileInputStream 读取 [start, start+count) 区间。
 * 统一走「Java 读 → Base64 字符串过桥 → JS 解码」，失败即 reject。
 */
function readStreamRange(fis, start, count, label) {
  return new Promise((resolve, reject) => {
    try {
      const ReflectArray = plus.android.importClass('java.lang.reflect.Array')
      const Byte = plus.android.importClass('java.lang.Byte')
      const B64 = plus.android.importClass('android.util.Base64') // NO_WRAP = 2

      if (start > 0) {
        let cur = Number(plus.android.invoke(fis, 'skip', start) || 0)
        // skip 可能少跳，用 read 空读兜底（极少数流不支持精确定位时直接报错）
        while (cur < start) {
          const jbuf = plus.android.invoke(ReflectArray, 'newInstance', Byte.TYPE,
            Math.min(BLOCK, start - cur))
          const n = plus.android.invoke(fis, 'read', jbuf)
          if (n === null || n === undefined || n <= 0) {
            return reject(new Error(label + ': 定位失败 skip=' + cur + '/' + start))
          }
          cur += n
        }
      }

      const out = new Uint8Array(count)
      let got = 0
      while (got < count) {
        const want = Math.min(BLOCK, count - got)
        const jbuf = plus.android.invoke(ReflectArray, 'newInstance', Byte.TYPE, want)
        let n = 0
        while (n < want) {
          const r = plus.android.invoke(fis, 'read', jbuf, n, want - n)
          if (r === null || r === undefined || r <= 0) break
          n += r
        }
        if (n <= 0) {
          if (got === 0) {
            return reject(new Error(label + ': 读取失败（0 字节，文件可能不可读）'))
          }
          return reject(new Error(label + ': 文件比预期短 got=' + got + '/' + count))
        }
        const b64 = plus.android.invoke(B64, 'encodeToString', jbuf, 0, n, 2)
        if (!b64) {
          return reject(new Error(label + ': Base64 编码失败'))
        }
        const bin = b64Decode(b64)
        out.set(bin, got)
        got += n
      }
      resolve(out)
    } catch (err) {
      reject(err)
    }
  })
}

/**
 * 读取文件指定区间（App 端走 plus.android + Base64 过桥，H5 端走 fetch Range）
 * @param {string} path 本地文件路径
 * @param {number} start
 * @param {number} len
 * @returns {Promise<Uint8Array>}
 */
function readRange(path, start, len) {
  return new Promise((resolve, reject) => {
    // #ifdef APP-PLUS
    try {
      let realPath = path
      if (realPath.indexOf('file://') === 0) realPath = realPath.slice(7)

      // SAF content:// URI：ContentResolver + ParcelFileDescriptor
      if (path.indexOf('content://') === 0) {
        const main = plus.android.runtimeMainActivity()
        const Uri = plus.android.importClass('android.net.Uri')
        const resolver = plus.android.invoke(main, 'getContentResolver')
        const uri = Uri.parse(path)
        const pfd = plus.android.invoke(resolver, 'openFileDescriptor', uri, 'r')
        if (!pfd) return reject(new Error('无法打开 SAF 文件'))
        const size = plus.android.invoke(pfd, 'getStatSize')
        const end = Math.min(start + len, size)
        const count = end - start
        if (count <= 0) return resolve(new Uint8Array(0))
        const FileInputStream = plus.android.importClass('java.io.FileInputStream')
        const fd = plus.android.invoke(pfd, 'getFileDescriptor')
        const fis = new FileInputStream(fd)
        readStreamRange(fis, start, count, 'SAF读取')
          .then(resolve, reject)
          .then(() => {
            try { plus.android.invoke(fis, 'close') } catch (e) { /* ignore */ }
            try { plus.android.invoke(pfd, 'close') } catch (e) { /* ignore */ }
          })
        return
      }

      // 本地文件路径：Java FileInputStream
      const File = plus.android.importClass('java.io.File')
      const FileInputStream = plus.android.importClass('java.io.FileInputStream')

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
      readStreamRange(fis, start, count, '文件读取')
        .then(resolve, reject)
        .then(() => {
          try { plus.android.invoke(fis, 'close') } catch (e) { /* ignore */ }
        })
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
