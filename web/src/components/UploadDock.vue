<template>
  <transition name="dock">
    <div v-if="hasTasks" class="ud" :class="{ mini: up.collapsed }">
      <!-- 收起态：一个小胶囊，点开还原 -->
      <button v-if="up.collapsed" class="ud-pill" @click="up.collapsed = false">
        <span class="ud-ring" :style="{ '--p': st.pct }" />
        <span class="ud-pill-t">
          {{ up.running ? `上传中 ${st.done}/${st.total}` : `完成 ${st.done}/${st.total}` }}
        </span>
        <span v-if="st.failed" class="ud-badge err">{{ st.failed }}</span>
      </button>

      <!-- 展开态 -->
      <div v-else class="ud-card">
        <div class="ud-head">
          <span class="ud-ic" :class="{ spin: up.running }">
            <AppIcon :name="up.running ? 'upload' : 'check'" :size="15" />
          </span>
          <div class="ud-t">
            <b>{{ up.running ? '正在上传' : '上传完成' }}</b>
            <span class="np-muted">
              {{ st.done }}/{{ st.total }}
              <template v-if="st.failed"> · 失败 {{ st.failed }}</template>
              <template v-if="up.running && up.speed"> · {{ up.speed }}</template>
            </span>
          </div>
          <div class="np-flex1" />
          <button class="ud-b" title="最小化" @click="up.collapsed = true">
            <AppIcon name="chevronDown" :size="15" />
          </button>
          <button class="ud-b" title="关闭面板（后台继续上传）" @click="onClose">
            <AppIcon name="close" :size="15" />
          </button>
        </div>

        <div class="ud-bar">
          <i :class="{ done: !up.running }" :style="{ width: st.pct + '%' }" />
        </div>

        <div class="ud-list">
          <div v-for="t in up.tasks" :key="t.k" class="ud-i" :class="t.state">
            <span class="ud-thumb" :class="{ vid: t.isVideo }">
              <img v-if="t.preview" :src="t.preview" alt="" />
              <AppIcon v-else :name="t.isVideo ? 'video' : 'image'" :size="13" />
            </span>
            <div class="ud-meta">
              <span class="ud-n" :title="t.name">{{ t.name }}</span>
              <span class="ud-sub" :class="t.state">
                <template v-if="t.state === 'err'">{{ t.msg || '失败' }}</template>
                <template v-else-if="t.state === 'run'">{{ fmtSize(t.loaded) }} / {{ fmtSize(t.file.size) }}</template>
                <template v-else-if="t.state === 'ok'">{{ t.dedup ? '秒传' : '已完成' }}</template>
                <template v-else-if="t.state === 'canceled'">已取消</template>
                <template v-else>等待中</template>
              </span>
            </div>
            <span class="ud-pct" v-if="t.state === 'run' || t.state === 'wait'">{{ t.pct }}%</span>
            <button v-if="t.state === 'run'" class="ud-x" title="取消" @click="cancelTask(t)">
              <AppIcon name="close" :size="13" />
            </button>
            <button v-else-if="t.state === 'err'" class="ud-x" title="重试" @click="retryTask(t)">
              <AppIcon name="refresh" :size="13" />
            </button>
            <button v-else class="ud-x" title="从列表移除" @click="drop(t)">
              <AppIcon name="close" :size="13" />
            </button>
          </div>
        </div>

        <div class="ud-foot">
          <button v-if="st.failed" class="lnk" @click="retryFailed">重试失败项</button>
          <button v-if="up.running" class="lnk danger" @click="cancelAll">取消全部</button>
          <div class="np-flex1" />
          <button class="lnk" v-if="st.done" @click="clearFinished">清除已完成</button>
        </div>
      </div>
    </div>
  </transition>
</template>

<script setup>
import { computed } from 'vue'
import AppIcon from './AppIcon.vue'
import {
  up, upStats, hasTasks, cancelTask, cancelAll, retryTask, retryFailed,
  clearFinished, fmtSize, closeDock,
} from '../store/upload'

const st = upStats

function drop(t) {
  const i = up.tasks.indexOf(t)
  if (i >= 0) up.tasks.splice(i, 1)
}
function onClose() {
  // 还在传就只收起面板（任务继续跑），传完了才清列表
  if (up.running) up.collapsed = true
  else closeDock()
}
</script>

<style scoped>
.ud {
  position: fixed;
  right: 18px;
  bottom: 18px;
  z-index: 2200;
  font-size: 13px;
}

/* ---------- 收起胶囊 ---------- */
.ud-pill {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 8px 14px 8px 10px;
  border: 1px solid var(--border);
  border-radius: var(--r-full);
  background: var(--bg-surface);
  color: var(--text);
  box-shadow: var(--shadow-md);
  cursor: pointer;
  transition: transform 0.16s, box-shadow 0.16s;
}
.ud-pill:hover {
  transform: translateY(-1px);
  box-shadow: var(--shadow-lg);
}
.ud-pill-t {
  font-weight: 600;
  white-space: nowrap;
}
/* 环形进度：conic-gradient 画的，比 svg 省事 */
.ud-ring {
  width: 18px;
  height: 18px;
  border-radius: 50%;
  flex-shrink: 0;
  background: conic-gradient(var(--brand) calc(var(--p) * 1%), var(--border) 0);
  position: relative;
}
.ud-ring::after {
  content: '';
  position: absolute;
  inset: 4px;
  border-radius: 50%;
  background: var(--bg-surface);
}
.ud-badge {
  font-size: 11px;
  font-weight: 700;
  padding: 1px 6px;
  border-radius: var(--r-full);
  background: var(--danger-soft, #fde8e8);
  color: var(--danger, #e0483d);
}

/* ---------- 展开卡片 ---------- */
.ud-card {
  width: 330px;
  max-height: 60vh;
  display: flex;
  flex-direction: column;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--bg-surface);
  box-shadow: var(--shadow-lg);
  overflow: hidden;
}
.ud-head {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 11px 10px 11px 13px;
  border-bottom: 1px solid var(--border);
}
.ud-ic {
  width: 26px;
  height: 26px;
  flex-shrink: 0;
  border-radius: var(--r-sm);
  display: grid;
  place-items: center;
  color: #fff;
  background: linear-gradient(135deg, #4f7dff, #7a4dff);
}
.ud-ic.spin {
  background: linear-gradient(135deg, #4f7dff, #ff5f8f);
}
.ud-t {
  display: flex;
  flex-direction: column;
  line-height: 1.3;
  min-width: 0;
}
.ud-t b {
  font-size: 13px;
  font-weight: 650;
}
.ud-t span {
  font-size: 11.5px;
}
.ud-b {
  width: 24px;
  height: 24px;
  flex-shrink: 0;
  display: grid;
  place-items: center;
  border: none;
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--text-sub);
  cursor: pointer;
}
.ud-b:hover {
  background: var(--bg-hover);
  color: var(--text);
}
/* 图标库没有 chevronDown，把右箭头转 90° 当向下用 */
.rot {
  display: grid;
  place-items: center;
  transform: rotate(90deg);
}

.ud-bar {
  height: 3px;
  background: var(--border);
  overflow: hidden;
}
.ud-bar i {
  display: block;
  height: 100%;
  background: linear-gradient(90deg, #4f7dff, #ff5f8f);
  transition: width 0.3s ease;
}
.ud-bar i.done {
  background: #31b26a;
}

.ud-list {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 4px 0;
}
.ud-i {
  display: flex;
  align-items: center;
  gap: 9px;
  padding: 7px 10px 7px 13px;
}
.ud-i:hover {
  background: var(--bg-hover);
}
.ud-thumb {
  width: 30px;
  height: 30px;
  flex-shrink: 0;
  border-radius: var(--r-sm);
  overflow: hidden;
  display: grid;
  place-items: center;
  background: var(--bg-subtle);
  color: var(--text-weak);
}
.ud-thumb img {
  width: 100%;
  height: 100%;
  object-fit: cover;
}
.ud-meta {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  line-height: 1.3;
}
.ud-n {
  font-size: 12.5px;
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.ud-sub {
  font-size: 11px;
  color: var(--text-weak);
}
.ud-sub.err {
  color: var(--danger, #e0483d);
}
.ud-sub.ok {
  color: #31b26a;
}
.ud-pct {
  font-size: 11.5px;
  font-variant-numeric: tabular-nums;
  color: var(--text-sub);
  flex-shrink: 0;
}
.ud-x {
  width: 20px;
  height: 20px;
  flex-shrink: 0;
  display: grid;
  place-items: center;
  border: none;
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--text-weak);
  cursor: pointer;
}
.ud-x:hover {
  background: var(--bg-active);
  color: var(--text);
}

.ud-foot {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 13px;
  border-top: 1px solid var(--border);
  background: var(--bg-subtle);
}
.lnk {
  border: none;
  background: none;
  padding: 0;
  font-size: 12px;
  color: var(--brand);
  cursor: pointer;
}
.lnk:hover {
  text-decoration: underline;
}
.lnk.danger {
  color: var(--danger, #e0483d);
}

/* ---------- 动画 ---------- */
.dock-enter-active,
.dock-leave-active {
  transition: opacity 0.2s ease, transform 0.2s ease;
}
.dock-enter-from,
.dock-leave-to {
  opacity: 0;
  transform: translateY(10px) scale(0.97);
}
</style>
