<template>
  <el-dialog v-model="visible" width="560px" align-center append-to-body
    :close-on-click-modal="false" class="ap-dlg" @closed="onClosed">
    <template #header>
      <div class="dlg-head">
        <AppIcon name="album" :size="17" />
        <span>加入相册{{ count ? `（已选 ${count} 项）` : '' }}</span>
      </div>
    </template>

    <!-- 新建相册：支持「按时间区间 / 关键词 / 整个库」直接归类 -->
    <div class="ap-new">
      <div class="ap-t">新建相册</div>
      <div class="ap-row">
        <input v-model="form.name" class="np-input" placeholder="相册名称，如「2026 春节」"
          maxlength="60" @keyup.enter="createAndAdd" />
      </div>
      <div class="ap-row">
        <span class="ap-lb">归类方式</span>
        <div class="ap-seg">
          <button v-for="r in RULE_TYPES" :key="r.v" class="seg-i"
            :class="{ on: form.ruleType === r.v }" @click="form.ruleType = r.v">{{ r.t }}</button>
        </div>
      </div>

      <div v-if="form.ruleType === 1" class="ap-row">
        <span class="ap-lb">拍摄时间</span>
        <input v-model="form.start" class="np-input sm" type="date" />
        <span class="ap-sep">至</span>
        <input v-model="form.end" class="np-input sm" type="date" />
      </div>
      <div v-else-if="form.ruleType === 2" class="ap-row">
        <span class="ap-lb">关键词</span>
        <input v-model="form.keyword" class="np-input" placeholder="文件名或路径包含的文字" />
      </div>
      <div v-else-if="form.ruleType === 3" class="ap-row">
        <span class="ap-lb">相册库</span>
        <el-select v-model="form.libraryId" placeholder="选择相册库" class="np-flex1">
          <el-option v-for="l in libraries" :key="l.id" :label="l.name" :value="l.id" />
        </el-select>
      </div>

      <div v-if="form.ruleType === 0" class="ap-hint">
        手动相册：创建后把当前选中的 {{ count }} 项放进去。
      </div>
      <div v-else class="ap-hint">
        规则相册：先按规则把符合条件的照片全部收进来，再额外加入当前选中的 {{ count }} 项。
      </div>

      <div class="ap-row end">
        <button class="btn primary sm" :disabled="!form.name.trim() || creating" @click="createAndAdd">
          <span v-if="creating" class="spin sm" /> 创建并加入
        </button>
      </div>
    </div>

    <div class="ap-div"><span>或加入已有相册</span></div>

    <div v-if="loading" class="ap-empty">加载中…</div>
    <div v-else-if="!albums.length" class="ap-empty">还没有相册，先在上面建一个吧。</div>
    <div v-else class="ap-list">
      <button v-for="a in albums" :key="a.id" class="ap-i" :disabled="busyId === a.id"
        @click="addTo(a)">
        <span class="ap-cover">
          <img v-if="a.cover_thumb" :src="withToken(a.cover_thumb)" alt="" />
          <AppIcon v-else name="album" :size="16" />
        </span>
        <span class="ap-meta">
          <b>{{ a.name }}</b>
          <span class="np-muted">{{ ruleText(a) }} · {{ a.real_count ?? a.item_count ?? 0 }} 项</span>
        </span>
        <span v-if="busyId === a.id" class="spin sm" />
        <AppIcon v-else name="plus" :size="15" />
      </button>
    </div>

    <template #footer>
      <button class="btn ghost" @click="visible = false">关闭</button>
    </template>
  </el-dialog>
</template>

<script setup>
import { computed, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import AppIcon from './AppIcon.vue'
import { listAlbums, createAlbum, addToAlbum, applyAlbumRule, listLibraries } from '../api'

const props = defineProps({
  modelValue: { type: Boolean, default: false },
  mediaIds: { type: Array, default: () => [] },
})
const emit = defineEmits(['update:modelValue', 'done'])

const visible = ref(props.modelValue)
watch(() => props.modelValue, (v) => {
  visible.value = v
  if (v) { load(); loadLibs() }
})
watch(visible, (v) => emit('update:modelValue', v))

const albums = ref([])
const libraries = ref([])
const loading = ref(false)
const creating = ref(false)
const busyId = ref(0)
const count = computed(() => props.mediaIds.length)

const RULE_TYPES = [
  { v: 0, t: '手动挑选' },
  { v: 1, t: '时间区间' },
  { v: 2, t: '关键词' },
  { v: 3, t: '整个库' },
]

const form = ref(blankForm())
function blankForm() {
  return { name: '', ruleType: 0, start: '', end: '', keyword: '', libraryId: null }
}
function onClosed() { form.value = blankForm() }

async function load() {
  loading.value = true
  try {
    const d = await listAlbums()
    albums.value = d.items || []
  } catch (e) {
    ElMessage.error(e.message || '加载相册失败')
  } finally {
    loading.value = false
  }
}
async function loadLibs() {
  try { libraries.value = await listLibraries() } catch (e) { libraries.value = [] }
}

function withToken(u) {
  const t = localStorage.getItem('naspic.token') || ''
  return u + (u.indexOf('?') >= 0 ? '&' : '?') + 'token=' + encodeURIComponent(t)
}

function ruleText(a) {
  return (RULE_TYPES.find((r) => r.v === a.rule_type) || RULE_TYPES[0]).t
}

async function createAndAdd() {
  const f = form.value
  if (!f.name.trim()) return
  creating.value = true
  try {
    const res = await createAlbum({
      name: f.name.trim(),
      rule_type: f.ruleType,
      rule: {
        start: f.start || '', end: f.end || '', keyword: f.keyword || '',
        library_id: f.libraryId || 0,
      },
    })
    const album = res.album || res
    // 规则相册先按规则收一遍，再把当前选中项塞进去
    if (f.ruleType !== 0) {
      try { await applyAlbumRule(album.id) } catch (e) { /* 规则没命中不影响后续 */ }
    }
    if (count.value) {
      await addToAlbum(album.id, props.mediaIds)
    }
    ElMessage.success(`已创建「${album.name}」`)
    emit('done', { albumId: album.id })
    visible.value = false
  } catch (e) {
    ElMessage.error(e.message || '创建失败')
  } finally {
    creating.value = false
  }
}

async function addTo(a) {
  if (!count.value) return
  busyId.value = a.id
  try {
    const r = await addToAlbum(a.id, props.mediaIds)
    ElMessage.success(`已加入「${a.name}」${r.added || count.value} 项`)
    emit('done', { albumId: a.id })
    visible.value = false
  } catch (e) {
    ElMessage.error(e.message || '加入失败')
  } finally {
    busyId.value = 0
  }
}
</script>

<style scoped>
.dlg-head { display: flex; align-items: center; gap: 8px; font-weight: 650; }
.ap-new {
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--bg-subtle);
}
.ap-t { font-size: 12.5px; font-weight: 650; margin-bottom: 10px; }
.ap-row { display: flex; align-items: center; gap: 8px; margin-bottom: 8px; }
.ap-row.end { justify-content: flex-end; margin-bottom: 0; margin-top: 10px; }
.ap-lb { width: 62px; flex-shrink: 0; font-size: 12.5px; color: var(--text-weak); }
.ap-sep { color: var(--text-weak); font-size: 12.5px; }
.np-input {
  flex: 1;
  min-width: 0;
  height: 32px;
  padding: 0 10px;
  border: 1px solid var(--border);
  border-radius: var(--r-sm);
  background: var(--bg-surface);
  color: var(--text);
  font-size: 13px;
}
.np-input.sm { flex: 0 0 140px; }
.ap-seg { display: flex; gap: 4px; flex-wrap: wrap; }
.seg-i {
  height: 28px;
  padding: 0 10px;
  border: 1px solid var(--border);
  background: var(--bg-surface);
  color: var(--text-weak);
  border-radius: var(--r-sm);
  font-size: 12.5px;
  cursor: pointer;
}
.seg-i.on { border-color: var(--brand); color: var(--brand); font-weight: 600; }
.ap-hint { font-size: 12px; color: var(--text-weak); line-height: 1.6; margin-top: 6px; }

.ap-div {
  display: flex;
  align-items: center;
  gap: 10px;
  margin: 14px 0 10px;
  color: var(--text-weak);
  font-size: 12px;
}
.ap-div::before,
.ap-div::after {
  content: '';
  flex: 1;
  height: 1px;
  background: var(--border);
}
.ap-empty { text-align: center; color: var(--text-weak); font-size: 13px; padding: 18px 0; }
.ap-list { max-height: 260px; overflow: auto; display: grid; gap: 4px; }
.ap-i {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 8px;
  border: none;
  background: transparent;
  border-radius: var(--r-sm);
  cursor: pointer;
  text-align: left;
  color: var(--text);
}
.ap-i:hover { background: var(--bg-hover); }
.ap-cover {
  width: 40px;
  height: 40px;
  border-radius: 8px;
  overflow: hidden;
  flex-shrink: 0;
  display: grid;
  place-items: center;
  background: var(--bg-subtle);
  color: var(--text-weak);
}
.ap-cover img { width: 100%; height: 100%; object-fit: cover; display: block; }
.ap-meta { flex: 1; min-width: 0; display: flex; flex-direction: column; gap: 2px; }
.ap-meta b { font-size: 13px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.ap-meta span { font-size: 11.5px; }
</style>
