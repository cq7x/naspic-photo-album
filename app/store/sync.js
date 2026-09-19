/**
 * 全局同步状态（Vue3 reactive），供设置页、通知栏读取
 */
import { reactive, computed } from 'vue'
import * as store from '../utils/store.js'

const state = reactive({
  tasks: [],
  activeTaskUri: '',
  running: false,
  progress: { done: 0, total: 0, synced: 0, failed: 0, skipped: 0 },
  lastSyncAt: 0,
  logs: [],
})

export function useSyncStore() {
  return {
    state,
    tasks: computed(() => state.tasks),
    overall: computed(() => {
      const p = state.progress
      return p.total ? Math.floor((p.done / p.total) * 100) : 0
    }),
    refresh() {
      state.tasks = store.getTasks()
      state.logs = store.getLogs().slice(0, 50)
    },
    setProgress(patch) {
      Object.assign(state.progress, patch)
    },
    resetProgress(total) {
      state.progress = { done: 0, total, synced: 0, failed: 0, skipped: 0 }
    },
  }
}

export { state }
