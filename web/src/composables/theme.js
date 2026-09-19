import { ref, watchEffect } from 'vue'

const KEY = 'naspic.theme' // 'light' | 'dark' | 'auto'

function read() {
  try {
    return localStorage.getItem(KEY) || 'auto'
  } catch {
    return 'auto'
  }
}

const mode = ref(read())
const media = typeof window !== 'undefined' && window.matchMedia
  ? window.matchMedia('(prefers-color-scheme: dark)')
  : null

const isDark = ref(false)

function resolve() {
  const m = mode.value
  const dark = m === 'dark' || (m === 'auto' && !!media && media.matches)
  isDark.value = dark
  const root = document.documentElement
  root.classList.toggle('dark', dark)
  root.style.colorScheme = dark ? 'dark' : 'light'
}

function setMode(m) {
  mode.value = m
  try {
    localStorage.setItem(KEY, m)
  } catch {
    /* ignore */
  }
  resolve()
}

function toggle() {
  setMode(isDark.value ? 'light' : 'dark')
}

if (media) {
  if (media.addEventListener) media.addEventListener('change', resolve)
  else if (media.addListener) media.addListener(resolve)
}

watchEffect(resolve)

export function useTheme() {
  return { mode, isDark, setMode, toggle }
}
