import { computed, reactive } from 'vue'
import { uploadWeb } from '../api'

// 全局上传任务队列。
// 之前上传逻辑写在 Gallery.vue 内部，弹窗一关任务就没了（组件状态跟着走），
// 大批量上传只能干等着。这里提到全局：弹窗关掉、甚至切到别的页面，
// 上传照样在后台跑，右下角面板随时能看进度。

const CONC = 3 // 并发数：3 条够快，又不至于把家庭宽带上行打满拖垮浏览

export const up = reactive({
  tasks: [],
  running: false,
  speed: '', // 展示用："2.4 MB/s"
  collapsed: false, // 面板收起（只留一个小胶囊）
  totalDone: 0, // 本次启动以来成功数
})

let seq = 0
let tick = null
let lastLoaded = 0
let lastTs = 0

export const upStats = computed(() => {
  const t = up.tasks
  const total = t.length
  const done = t.filter((x) => x.state === 'ok').length
  const failed = t.filter((x) => x.state === 'err').length
  const active = t.filter((x) => x.state === 'run' || x.state === 'wait').length
  let loaded = 0
  let size = 0
  for (const x of t) {
    size += x.file ? x.file.size : 0
    loaded += x.state === 'ok' ? (x.file ? x.file.size : 0) : x.loaded || 0
  }
  const pct = size > 0 ? Math.min(100, Math.round((loaded * 100) / size)) : 0
  return { total, done, failed, active, pct, loaded, size }
})

export const hasTasks = computed(() => up.tasks.length > 0)

export function fmtBytes(b) {
  if (!b) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(u.length - 1, Math.floor(Math.log(b) / Math.log(1024)))
  return (b / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1) + ' ' + u[i]
}

function errText(err) {
  const st = err && err.response && err.response.status
  const body = err && err.response && err.response.data
  if (body && body.msg) return body.msg
  if (st === 401) return '登录已过期，请重新登录'
  if (st === 403) return '该相册库只读，不能上传'
  if (st === 413) return '文件过大，被服务器拒绝'
  if (st === 0 || !st) return '网络错误或服务未响应'
  return err.message || '上传失败'
}

function notify() {
  // Gallery 监听这个事件去刷列表（传完一个就出现一个，不用等全部结束）
  try {
    window.dispatchEvent(new CustomEvent('naspic:upload-progress'))
  } catch (e) {
    /* 老浏览器兜不了就算了 */
  }
}

// 入队：把弹窗里选好的清单交给后台任务
export function enqueue(items, libId) {
  for (const it of items) {
    up.tasks.push({
      k: 'u' + ++seq,
      file: it.file,
      name: it.file.name,
      preview: it.preview || '',
      isVideo: !!it.isVideo,
      libId,
      state: 'wait', // wait / run / ok / err / canceled
      pct: 0,
      loaded: 0,
      msg: '',
      dedup: false,
      ctrl: null,
    })
  }
  start()
}

function startTicker() {
  if (tick) return
  lastLoaded = 0
  lastTs = Date.now()
  tick = setInterval(() => {
    const now = Date.now()
    const cur = up.tasks.reduce((s, x) => s + (x.loaded || 0), 0)
    const dt = (now - lastTs) / 1000
    if (dt > 0 && cur >= lastLoaded) {
      const bps = (cur - lastLoaded) / dt
      up.speed = bps > 0 ? fmtBytes(bps) + '/s' : ''
    }
    lastLoaded = cur
    lastTs = now
  }, 1000)
}
function stopTicker() {
  if (tick) clearInterval(tick)
  tick = null
  up.speed = ''
}

export function start() {
  if (up.running) return // 已在跑：新入队的会被工作池自动取走
  const wait = up.tasks.filter((x) => x.state === 'wait')
  if (!wait.length) return
  up.running = true
  up.collapsed = false
  startTicker()
  Promise.all(Array.from({ length: CONC }, worker)).then(() => {
    up.running = false
    stopTicker()
    notify()
  })
}

async function worker() {
  for (;;) {
    const t = up.tasks.find((x) => x.state === 'wait')
    if (!t) return
    t.state = 'run'
    t.pct = 0
    t.msg = ''
    t.loaded = 0
    t.ctrl = typeof AbortController !== 'undefined' ? new AbortController() : null
    try {
      const res = await uploadWeb(
        t.libId,
        [t.file],
        (e) => {
          if (e && e.total) {
            t.loaded = e.loaded
            t.pct = Math.min(99, Math.round((e.loaded * 100) / e.total))
          }
        },
        t.ctrl ? { signal: t.ctrl.signal } : null
      )
      t.state = 'ok'
      t.pct = 100
      t.loaded = t.file.size
      const r0 = res && res.results && res.results[0]
      t.dedup = !!(r0 && r0.dedup)
      up.totalDone++
      notify()
    } catch (err) {
      if (t.state === 'canceled') {
        t.msg = '已取消'
      } else {
        t.state = 'err'
        t.msg = errText(err)
      }
    } finally {
      t.ctrl = null
    }
  }
}

// 取消：正在传的直接 abort，没轮到的直接标记取消
export function cancelTask(t) {
  if (t.state === 'run' && t.ctrl) {
    t.state = 'canceled'
    try {
      t.ctrl.abort()
    } catch (e) {
      /* noop */
    }
  } else if (t.state === 'wait') {
    t.state = 'canceled'
    t.msg = '已取消'
  }
  const i = up.tasks.indexOf(t)
  if (i >= 0) up.tasks.splice(i, 1)
}

export function cancelAll() {
  ;[...up.tasks].forEach((t) => {
    if (t.state === 'run' || t.state === 'wait') cancelTask(t)
  })
}

export function retryTask(t) {
  t.state = 'wait'
  t.msg = ''
  t.pct = 0
  t.loaded = 0
  start()
}

export function retryFailed() {
  up.tasks.forEach((t) => {
    if (t.state === 'err') {
      t.state = 'wait'
      t.msg = ''
      t.pct = 0
      t.loaded = 0
    }
  })
  start()
}

// 清掉已完成的（成功/取消），失败和进行中的保留，方便重试
export function clearFinished() {
  up.tasks = up.tasks.filter((t) => t.state === 'run' || t.state === 'wait' || t.state === 'err')
}

export function closeDock() {
  clearFinished()
  up.collapsed = false
}

export function fmtSize(b) {
  return fmtBytes(b)
}
