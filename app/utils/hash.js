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
      plus.io.resolveLocalFileSystemURL(
        path,
        (entry) => {
          entry.file(
            (file) => {
              const reader = new plus.io.FileReader()
              reader.onloadend = (e) => {
                const ab = e.target && e.target.result
                resolve(new Uint8Array(ab || new ArrayBuffer(0)))
              }
              reader.onerror = (e) => reject(new Error('读取失败: ' + (e && e.message)))
              const end = Math.min(start + len, file.size)
              reader.readAsArrayBuffer(file.slice(start, end))
            },
            (e) => reject(new Error('打开文件失败'))
          )
        },
        (e) => reject(new Error('路径无效: ' + path))
      )
      return
    } catch (err) {
      reject(err)
      return
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
  return new Promise((resolve, reject) => {
    // #ifdef APP-PLUS
    plus.io.resolveLocalFileSystemURL(path, (entry) => {
      entry.file((file) => resolve(file.size), () => resolve(0))
    }, () => resolve(0))
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
