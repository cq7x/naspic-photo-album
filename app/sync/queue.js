/**
 * 并发可控的任务队列 + 指数退避重试
 *
 * 中断约定：所有函数都接受一个 stopper（形如 { stopped: boolean }）。
 * 同步过程中用户点「停止」时把 stopper.stopped 置 true，
 * 队列会在「下一个文件」「下一次重试」「下一次退避等待」处立即收尾，
 * 不会出现点停止后还在那儿傻等 30 秒的情况。
 */

/** 被中断时抛出的错误 */
export class Stopped extends Error {
  constructor(msg = '已停止') {
    super(msg)
    this.name = 'Stopped'
    this.stopped = true
  }
}

function isStopped(stopper) {
  return !!(stopper && stopper.stopped)
}

/** 可中断的 sleep：每 500ms 检查一次停止标志 */
export async function sleep(ms, stopper) {
  const step = 500
  let left = ms
  while (left > 0) {
    if (isStopped(stopper)) throw new Stopped()
    const w = Math.min(step, left)
    await new Promise((r) => setTimeout(r, w))
    left -= w
  }
}

/**
 * 以固定并发执行任务
 * @param {Array} items
 * @param {number} limit
 * @param {(item:any, index:number)=>Promise<any>} worker
 * @param {(done:number, total:number)=>void} onProgress
 * @param {{stopped:boolean}} [stopper]
 */
export async function runLimit(items, limit, worker, onProgress, stopper) {
  let cursor = 0
  let done = 0
  const total = items.length

  async function runner() {
    while (cursor < items.length) {
      if (isStopped(stopper)) return
      const i = cursor++
      try {
        await worker(items[i], i)
      } catch (e) {
        // 单文件失败不影响整体，由 worker 内部记录
      }
      done++
      if (onProgress) onProgress(done, total)
      if (isStopped(stopper)) return
    }
  }

  const pool = []
  for (let i = 0; i < Math.min(limit, total); i++) pool.push(runner())
  await Promise.all(pool)
}

/**
 * 指数退避重试：1s → 2s → 4s → 8s → 16s（上限 30s）
 * @param {Function} fn
 * @param {number} max
 * @param {Function} [onRetry]
 * @param {{stopped:boolean}} [stopper]
 */
export async function retry(fn, max = 5, onRetry, stopper) {
  let lastErr
  for (let i = 0; i < max; i++) {
    if (isStopped(stopper)) throw new Stopped()
    try {
      return await fn()
    } catch (e) {
      lastErr = e
      if (isStopped(stopper)) throw e
      // 认证类错误不重试：重试也是 401，白白耗时间
      if (e && e.unauthorized) throw e
      const wait = Math.min(30000, Math.pow(2, i) * 1000)
      if (onRetry) onRetry(i + 1, wait, e)
      if (i === max - 1) break
      await sleep(wait, stopper)
    }
  }
  throw lastErr
}
