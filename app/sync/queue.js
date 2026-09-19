/**
 * 并发可控的任务队列 + 指数退避重试
 */

/**
 * 以固定并发执行任务
 * @param {Array} items
 * @param {number} limit
 * @param {(item:any, index:number)=>Promise<any>} worker
 * @param {(done:number, total:number)=>void} onProgress
 */
export async function runLimit(items, limit, worker, onProgress) {
  let cursor = 0
  let done = 0
  const total = items.length

  async function runner() {
    while (cursor < items.length) {
      const i = cursor++
      try {
        await worker(items[i], i)
      } catch (e) {
        // 单文件失败不影响整体，由 worker 内部记录
      }
      done++
      if (onProgress) onProgress(done, total)
    }
  }

  const pool = []
  for (let i = 0; i < Math.min(limit, total); i++) pool.push(runner())
  await Promise.all(pool)
}

/**
 * 指数退避重试：1s → 2s → 4s → 8s → 16s（上限 30s）
 */
export async function retry(fn, max = 5, onRetry) {
  let lastErr
  for (let i = 0; i < max; i++) {
    try {
      return await fn()
    } catch (e) {
      lastErr = e
      const wait = Math.min(30000, Math.pow(2, i) * 1000)
      if (onRetry) onRetry(i + 1, wait, e)
      await sleep(wait)
    }
  }
  throw lastErr
}

export function sleep(ms) {
  return new Promise((r) => setTimeout(r, ms))
}
