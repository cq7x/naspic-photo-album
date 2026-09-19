/**
 * 分片上传器：秒传探测 → 初始化 → 分片（断点续传）→ 合并完成
 */
import * as api from '../utils/api.js'
import { hashFile, readChunk } from '../utils/hash.js'
import { retry } from './queue.js'

/**
 * 上传单个文件
 * @param {{path:string,name:string,size:number,type:string,mtime:number}} file
 * @param {object} task      同步任务（含 targetLibraryId 等策略）
 * @param {object} ctx       { onProgress(done,total), network: 'wifi'|'mobile' }
 * @returns {Promise<{mediaId:number, dedup:boolean, hash:string}>}
 */
export async function upload(file, task, ctx = {}) {
  const libraryId = task.targetLibraryId
  const network = ctx.network || 'wifi'

  // 1. 内容哈希（大文件采样）
  const { hash } = await hashFile(file.path, file.size)

  // 2. 秒传探测
  const chk = await api.uploadCheck({ library_id: libraryId, hash, size: file.size, filename: file.name })
  if (chk.exists) return { mediaId: chk.media_id, dedup: true, hash, skipped: true }
  if (!chk.resumable) chk.session_id = null

  // 3. 初始化会话（已存在可续传会话时直接用）
  let sessionId = chk.session_id
  let chunkSize = chk.chunk_size || (network === 'wifi' ? 4 << 20 : 1 << 20)
  let uploaded = chk.uploaded_chunks || []
  let chunkTotal

  if (!sessionId) {
    const init = await api.uploadInit({
      library_id: libraryId,
      hash,
      size: file.size,
      filename: file.name,
      network,
      device_id: task.deviceId,
      local_path: file.path,
    })
    sessionId = init.session_id
    chunkSize = init.chunk_size
    chunkTotal = init.chunk_total
    uploaded = []
  } else {
    chunkTotal = Math.ceil(file.size / chunkSize)
  }

  // 4. 逐片上传（跳过已传分片 = 断点续传核心）
  for (let i = 0; i < chunkTotal; i++) {
    if (uploaded.includes(i)) continue
    const chunk = await readChunk(file.path, i, chunkSize)
    await retry(() => api.uploadChunk(sessionId, i, chunk), 5, (n, wait, err) => {
      console.warn(`[uploader] 分片 ${i} 第 ${n} 次失败，${wait}ms 后重试`, err.message)
    })
    uploaded.push(i)
    if (ctx.onProgress) ctx.onProgress(uploaded.length, chunkTotal)
  }

  // 5. 合并 + 服务端哈希校验 + 入库
  const res = await api.uploadComplete({
    session_id: sessionId,
    filename: file.name,
    device_id: task.deviceId,
    local_path: file.path,
  })
  return { mediaId: res.media_id, dedup: !!res.dedup, renamed: !!res.renamed, hash }
}
