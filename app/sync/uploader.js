/**
 * 分片上传器：秒传探测 → 初始化 → 分片（断点续传）→ 合并完成
 */
import * as api from '../utils/api.js'
import { hashFile, readChunk } from '../utils/hash.js'
import { retry, Stopped } from './queue.js'

/**
 * 上传单个文件
 * @param {{path:string,name:string,size:number,type:string,mtime:number}} file
 * @param {object} task      同步任务（含 targetLibraryId 等策略）
 * @param {object} ctx       { onProgress(done,total), network: 'wifi'|'mobile', stopper:{stopped} }
 * @returns {Promise<{mediaId:number, dedup:boolean, hash:string}>}
 */
export async function upload(file, task, ctx = {}) {
  const libraryId = task.targetLibraryId || 0
  const network = ctx.network || 'wifi'
  const stopper = ctx.stopper
  const bail = () => {
    if (stopper && stopper.stopped) throw new Stopped()
  }
  bail()
  console.log('[uploader] 开始上传', file.name, 'size=' + file.size, 'path=' + file.path)

  // 1. 内容哈希（大文件采样）
  console.log('[uploader] 计算哈希...', file.name)
  const { hash } = await hashFile(file.path, file.size)
  console.log('[uploader] 哈希完成', file.name, 'hash=' + hash.slice(0, 12))

  // 2. 秒传探测（library_id=0 时后端会根据 device_id 自动建库）
  console.log('[uploader] 秒传探测', file.name)
  const chk = await api.uploadCheck({
    library_id: libraryId,
    device_id: task.deviceId || 0,
    hash,
    size: file.size,
    filename: file.name,
  })
  // 后端可能自动建库后返回 library_id
  const realLibId = chk.library_id || libraryId
  if (chk.exists) {
    console.log('[uploader] 秒传命中', file.name)
    return { mediaId: chk.media_id, dedup: true, hash, skipped: true }
  }
  if (!chk.resumable) chk.session_id = null
  console.log('[uploader] 秒传未命中', file.name, 'session=' + chk.session_id, 'lib=' + realLibId)

  // 3. 初始化会话
  let sessionId = chk.session_id
  let chunkSize = chk.chunk_size || (network === 'wifi' ? 4 << 20 : 1 << 20)
  let uploaded = chk.uploaded_chunks || []
  let chunkTotal

  if (!sessionId) {
    console.log('[uploader] 初始化会话', file.name)
    const init = await api.uploadInit({
      library_id: realLibId,
      hash,
      size: file.size,
      filename: file.name,
      network,
      device_id: task.deviceId || 0,
      local_path: file.path,
    })
    sessionId = init.session_id
    chunkSize = init.chunk_size
    chunkTotal = init.chunk_total
    uploaded = []
    console.log('[uploader] 会话已建', file.name, 'session=' + sessionId, 'chunkTotal=' + chunkTotal)
  } else {
    chunkTotal = Math.ceil(file.size / chunkSize)
  }

  // 4. 逐片上传
  for (let i = 0; i < chunkTotal; i++) {
    if (uploaded.includes(i)) continue
    bail()
    const chunk = await readChunk(file.path, i, chunkSize)
    await retry(() => api.uploadChunk(sessionId, i, chunk), 5, (n, wait, err) => {
      console.warn(`[uploader] 分片 ${i} 第 ${n} 次失败，${wait}ms 后重试`, err.message)
    }, stopper)
    uploaded.push(i)
    if (ctx.onProgress) ctx.onProgress(uploaded.length, chunkTotal)
  }
  bail()
  console.log('[uploader] 分片全部完成', file.name)

  // 5. 合并
  const res = await api.uploadComplete({
    session_id: sessionId,
    filename: file.name,
    device_id: task.deviceId || 0,
    local_path: file.path,
  })
  console.log('[uploader] 合并完成', file.name, 'mediaId=' + res.media_id)
  return { mediaId: res.media_id, dedup: !!res.dedup, renamed: !!res.renamed, hash }
}
