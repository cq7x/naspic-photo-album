<template>
  <div class="shell" :class="{ 'is-mini': mini, 'is-login': isLogin }">
    <!-- ============ 登录页：不渲染外壳 ============ -->
    <router-view v-if="isLogin" />

    <template v-else>
      <!-- ============ 侧边栏 ============ -->
      <aside class="side">
        <div class="side-top">
          <div class="brand" :title="APP_NAME">
            <span class="brand-mark">
              <svg viewBox="0 0 24 24" width="17" height="17" fill="none"
                stroke="currentColor" stroke-width="2" stroke-linecap="round"
                stroke-linejoin="round">
                <rect x="3" y="3" width="8" height="8" rx="2" />
                <rect x="13" y="3" width="8" height="8" rx="2" />
                <rect x="3" y="13" width="8" height="8" rx="2" />
                <path d="M17 13v8M13 17h8" />
              </svg>
            </span>
            <span class="brand-text">{{ APP_NAME }}</span>
          </div>
          <!-- 收起后按钮跟着变窄，位置不变，避免"按钮跑没了" -->
          <button class="icon-btn mini-btn" :title="mini ? '展开侧边栏' : '收起侧边栏'"
            @click="mini = !mini">
            <AppIcon :name="mini ? 'chevronRight' : 'chevronLeft'" :size="17" />
          </button>
        </div>

        <nav class="nav">
          <router-link v-for="n in navs" :key="n.to" :to="n.to" class="nav-item"
            :class="{ active: isActive(n) }">
            <AppIcon :name="n.icon" :size="18" />
            <span class="nav-label">{{ n.label }}</span>
            <span v-if="n.key === 'albums' && albumCount" class="nav-badge">{{ albumCount }}</span>
            <!-- 收起态：图标右侧浮出中文说明，不用猜图标是什么意思 -->
            <span v-if="mini" class="nav-tip">{{ n.label }}</span>
          </router-link>
        </nav>

        <div class="side-foot">
          <div class="quota" v-if="!mini">
            <div class="quota-line">
              <span class="np-muted">已收录</span>
              <b>{{ statsTotal }}</b>
            </div>
            <div class="quota-bar">
              <i :style="{ width: '100%' }" />
            </div>
            <div class="np-muted quota-sub">{{ fmtSize(statsBytes) }} · {{ libCount }} 个库</div>
          </div>

          <div class="me" :class="{ center: mini }">
            <span class="avatar">{{ initial }}</span>
            <div class="me-info" v-if="!mini">
              <b>{{ username }}</b>
              <span class="np-muted">{{ roleText }}</span>
            </div>
            <button class="icon-btn" title="退出登录" @click="logout">
              <AppIcon name="logout" :size="16" />
            </button>
          </div>
        </div>
      </aside>

      <!-- ============ 右侧 ============ -->
      <div class="main">
        <header class="topbar">
          <!-- 顶栏也放一个，收起后侧边栏按钮变窄不好找时这里永远可点 -->
          <button class="icon-btn" :title="mini ? '展开侧边栏' : '收起侧边栏'"
            @click="mini = !mini">
            <AppIcon name="panel" :size="18" />
          </button>
          <div class="crumb">
            <h1>{{ pageTitle }}</h1>
            <span class="np-muted" v-if="pageSub">{{ pageSub }}</span>
          </div>
          <div class="np-flex1" />
          <span class="ver-chip">v{{ APP_VERSION }}</span>
          <button class="icon-btn" :title="isDark ? '切换到浅色' : '切换到深色'"
            @click="toggle">
            <AppIcon :name="isDark ? 'sun' : 'moon'" :size="17" />
          </button>
        </header>

        <main class="content">
          <router-view v-slot="{ Component }">
            <transition name="page" mode="out-in">
              <component :is="Component" :preset="routePreset" @stats="onStats" />
            </transition>
          </router-view>
        </main>
      </div>
    </template>

    <!-- 上传任务面板：全局常驻，弹窗关了、切页面了上传照样在后台跑 -->
    <UploadDock v-if="!isLogin" />
  </div>
</template>

<script setup>
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppIcon from './components/AppIcon.vue'
import UploadDock from './components/UploadDock.vue'
import { useTheme } from './composables/theme'
import { APP_NAME, APP_VERSION } from './version'
import { listLibraries, mediaStats, listAlbums } from './api'

const route = useRoute()
const router = useRouter()
const { isDark, toggle } = useTheme()

const mini = ref(localStorage.getItem('naspic.sidebar') === 'mini')
watch(mini, (v) => localStorage.setItem('naspic.sidebar', v ? 'mini' : ''))

const isLogin = computed(() => route.path === '/login' || route.path === '/setup')

const navs = [
  { key: 'photos', to: '/photos', label: '照片', icon: 'grid', match: ['/photos'] },
  { key: 'albums', to: '/albums', label: '相册', icon: 'album', match: ['/albums'] },
  { key: 'settings', to: '/settings?tab=storage', label: '设置', icon: 'settings', match: ['/settings'] },
]

function isActive(n) {
  return n.match.some((m) => route.path === m)
}

const TITLES = {
  '/photos': ['照片', '按时间轴浏览全部媒体'],
  '/albums': ['相册', '按主题、时间或关键词归类照片'],
  '/settings': ['设置', '存储库、缓存与管理员账号'],
}
const pageTitle = computed(() => (TITLES[route.path] || ['Naspic', ''])[0])
const pageSub = computed(() => (TITLES[route.path] || ['', ''])[1])

const username = ref(localStorage.getItem('naspic.user') || 'admin')
const role = ref(Number(localStorage.getItem('naspic.role') || '1'))
const roleText = computed(() => (role.value === 2 ? '管理员' : '普通用户'))
const initial = computed(() => (username.value || 'A').trim().charAt(0).toUpperCase())

const statsTotal = ref(0)
const statsBytes = ref(0)
const albumCount = ref(0)
const libCount = ref(0)

function onStats(s) {
  if (!s) return
  statsTotal.value = s.total || 0
  statsBytes.value = s.bytes || 0
}

async function loadSide() {
  try {
    const [libs, st] = await Promise.all([listLibraries(), mediaStats(0)])
    libCount.value = (libs || []).length
    onStats(st)
  } catch {
    /* 未登录或接口异常时忽略 */
  }
  try {
    const d = await listAlbums()
    albumCount.value = (d.items || []).length
  } catch {
    /* 忽略 */
  }
}

function fmtSize(b) {
  if (!b) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(u.length - 1, Math.floor(Math.log(b) / Math.log(1024)))
  return (b / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1) + ' ' + u[i]
}

function logout() {
  localStorage.removeItem('naspic.token')
  localStorage.removeItem('naspic.user')
  localStorage.removeItem('naspic.role')
  localStorage.removeItem('naspic.uid')
  router.push('/login')
}

onMounted(loadSide)
</script>

<style scoped>
.shell {
  display: flex;
  height: 100%;
  background: var(--bg-app);
}
.shell.is-login {
  display: block;
}

/* ---------------- 侧边栏 ---------------- */
.side {
  width: var(--sidebar-w);
  flex-shrink: 0;
  display: flex;
  flex-direction: column;
  background: var(--bg-surface);
  border-right: 1px solid var(--border);
  transition: width 0.22s cubic-bezier(0.4, 0, 0.2, 1);
}
.is-mini .side {
  width: var(--sidebar-w-mini);
}

.side-top {
  height: var(--topbar-h);
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 0 14px;
  flex-shrink: 0;
}
.brand {
  display: flex;
  align-items: center;
  gap: 10px;
  flex: 1;
  min-width: 0;
}
.brand-mark {
  width: 30px;
  height: 30px;
  flex-shrink: 0;
  border-radius: 9px;
  display: grid;
  place-items: center;
  color: #fff;
  background: linear-gradient(135deg, #4f7dff 0%, #7a4dff 55%, #ff5f8f 100%);
  box-shadow: 0 4px 12px rgba(79, 125, 255, 0.35);
}
.brand-text {
  font-size: 16px;
  font-weight: 700;
  letter-spacing: 0.2px;
  white-space: nowrap;
  overflow: hidden;
}
.is-mini .brand-text,
.is-mini .nav-label,
.is-mini .nav-badge {
  display: none;
}
.is-mini .side-top,
.is-mini .nav-item,
.is-mini .me {
  justify-content: center;
  padding-left: 0;
  padding-right: 0;
}

.nav {
  padding: 6px 10px;
  display: flex;
  flex-direction: column;
  gap: 2px;
  flex: 1;
  overflow-y: auto;
}
.nav-item {
  display: flex;
  align-items: center;
  gap: 11px;
  padding: 9px 12px;
  border-radius: var(--r-sm);
  color: var(--text-sub);
  font-size: 14px;
  font-weight: 500;
  position: relative;
  transition: background 0.16s, color 0.16s;
}
.nav-item:hover {
  background: var(--bg-hover);
  color: var(--text);
}
.nav-item.active {
  background: var(--brand-soft);
  color: var(--brand);
  font-weight: 600;
}
.nav-item.active::before {
  content: '';
  position: absolute;
  left: -10px;
  top: 50%;
  transform: translateY(-50%);
  width: 3px;
  height: 18px;
  border-radius: 0 3px 3px 0;
  background: var(--brand);
}
.nav-label {
  flex: 1;
  white-space: nowrap;
  overflow: hidden;
}
.nav-badge {
  font-size: 11px;
  font-weight: 600;
  padding: 1px 7px;
  border-radius: var(--r-full);
  background: var(--bg-active);
  color: var(--text-sub);
}

/* 收起态：悬停浮出中文说明 */
.nav-tip {
  position: absolute;
  left: calc(100% + 10px);
  top: 50%;
  transform: translateY(-50%) translateX(-4px);
  padding: 5px 10px;
  border-radius: var(--r-sm);
  background: var(--text);
  color: var(--bg-surface);
  font-size: 12px;
  font-weight: 500;
  white-space: nowrap;
  opacity: 0;
  pointer-events: none;
  z-index: 40;
  box-shadow: var(--shadow-md);
  transition: opacity 0.16s, transform 0.16s;
}
.nav-item:hover .nav-tip {
  opacity: 1;
  transform: translateY(-50%) translateX(0);
}

.side-foot {
  padding: 10px;
  border-top: 1px solid var(--border);
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.quota {
  padding: 10px 12px;
  border-radius: var(--r-md);
  background: var(--bg-subtle);
}
.quota-line {
  display: flex;
  justify-content: space-between;
  align-items: baseline;
  font-size: 13px;
}
.quota-line b {
  font-size: 17px;
  font-weight: 700;
}
.quota-bar {
  height: 5px;
  margin: 7px 0 6px;
  border-radius: var(--r-full);
  background: var(--border);
  overflow: hidden;
}
.quota-bar i {
  display: block;
  height: 100%;
  border-radius: var(--r-full);
  background: linear-gradient(90deg, #4f7dff, #ff5f8f);
  transition: width 0.5s ease;
}
.quota-sub {
  font-size: 11px;
}

.me {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 4px 6px 4px 4px;
}
.avatar {
  width: 32px;
  height: 32px;
  flex-shrink: 0;
  border-radius: var(--r-full);
  display: grid;
  place-items: center;
  font-weight: 700;
  font-size: 14px;
  color: #fff;
  background: linear-gradient(135deg, #ff8a5b, #ff5f8f);
}
.me-info {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  line-height: 1.25;
}
.me-info b {
  font-size: 13px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}

/* ---------------- 右侧 ---------------- */
.main {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
}
.topbar {
  height: var(--topbar-h);
  flex-shrink: 0;
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 0 22px;
  background: var(--bg-surface);
  border-bottom: 1px solid var(--border);
}
.crumb {
  display: flex;
  align-items: baseline;
  gap: 10px;
  min-width: 0;
}
.crumb h1 {
  margin: 0;
  font-size: 18px;
  font-weight: 650;
  letter-spacing: 0.2px;
}
.ver-chip {
  font-size: 11px;
  font-weight: 600;
  color: var(--text-weak);
  padding: 3px 9px;
  border-radius: var(--r-full);
  background: var(--bg-subtle);
  border: 1px solid var(--border);
}

.content {
  flex: 1;
  min-height: 0;
  display: flex;
  flex-direction: column;
  overflow: hidden;
  position: relative;
}
.content > * {
  flex: 1;
  min-height: 0;
  padding: 20px 24px;
  overflow-y: auto;
}

/* ---------------- 通用图标按钮 ---------------- */
.icon-btn {
  width: 32px;
  height: 32px;
  display: grid;
  place-items: center;
  border: none;
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--text-sub);
  cursor: pointer;
  transition: background 0.16s, color 0.16s;
}
.icon-btn:hover {
  background: var(--bg-hover);
  color: var(--text);
}
.mini-btn {
  flex-shrink: 0;
}

/* ---------------- 页面切换 ---------------- */
.page-enter-active,
.page-leave-active {
  transition: opacity 0.18s ease, transform 0.18s ease;
}
.page-enter-from {
  opacity: 0;
  transform: translateY(6px);
}
.page-leave-to {
  opacity: 0;
}

@media (max-width: 860px) {
  .side {
    position: absolute;
    z-index: 30;
    height: 100%;
    box-shadow: var(--shadow-md);
  }
}
</style>
