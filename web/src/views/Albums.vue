<template>
  <div class="albums">
    <!-- ================= 顶栏 ================= -->
    <div class="np-row">
      <div class="np-flex1">
        <h2 class="pg-t">相册</h2>
        <p class="pg-s">把照片按主题、时间或关键词归到一起。规则相册可以一键重新匹配。</p>
      </div>
      <button class="btn primary" @click="openCreate">
        <AppIcon name="plus" :size="15" /> 新建相册
      </button>
    </div>

    <div v-if="loading" class="skeleton">
      <div v-for="i in 8" :key="i" class="sk" />
    </div>

    <div v-else-if="!albums.length" class="empty">
      <div class="empty-art"><AppIcon name="album" :size="34" /></div>
      <h3>还没有相册</h3>
      <p>可以按「时间区间」自动归类，也可以手动挑照片放进去。</p>
      <button class="btn primary" @click="openCreate">
        <AppIcon name="plus" :size="15" /> 新建第一个相册
      </button>
    </div>

    <div v-else class="grid">
      <div v-for="a in albums" :key="a.id" class="card" @click="open(a)">
        <div class="cover">
          <img v-if="a.cover_thumb" :src="withToken(a.cover_thumb)" :alt="a.name" loading="lazy" />
          <div v-else class="cover-ph"><AppIcon name="album" :size="26" /></div>
          <span class="cnt">{{ a.real_count ?? a.item_count ?? 0 }}</span>
        </div>
        <div class="meta">
          <b class="nm" :title="a.name">{{ a.name }}</b>
          <span class="np-muted sub">{{ ruleText(a) }}<template v-if="a.auto_add"> · 自动纳入</template></span>
        </div>
        <div class="ops">
          <button class="lnk" title="按规则重新匹配" @click.stop="reRun(a)">
            <AppIcon name="refresh" :size="14" />
          </button>
          <button class="lnk" title="编辑" @click.stop="openEdit(a)">
            <AppIcon name="sliders" :size="14" />
          </button>
          <button class="lnk danger" title="删除相册" @click.stop="remove(a)">
            <AppIcon name="trash" :size="14" />
          </button>
        </div>
      </div>
    </div>

    <!-- ================= 新建 / 编辑 ================= -->
    <el-dialog v-model="dlg" width="560px" align-center append-to-body
      :close-on-click-modal="false">
      <template #header>
        <div class="dlg-head">
          <AppIcon name="album" :size="17" />
          <span>{{ editing ? '编辑相册' : '新建相册' }}</span>
        </div>
      </template>

      <div class="fr">
        <span class="lb">名称</span>
        <input v-model="form.name" class="ipt" maxlength="60" placeholder="如：2026 春节" />
      </div>
      <div class="fr">
        <span class="lb">备注</span>
        <input v-model="form.remark" class="ipt" maxlength="120" placeholder="选填" />
      </div>
      <div class="fr">
        <span class="lb">归类方式</span>
        <div class="seg">
          <button v-for="r in RULE_TYPES" :key="r.v" class="seg-i"
            :class="{ on: form.ruleType === r.v }" @click="form.ruleType = r.v">{{ r.t }}</button>
        </div>
      </div>

      <div v-if="form.ruleType === 1" class="fr">
        <span class="lb">拍摄时间</span>
        <input v-model="form.start" class="ipt sm" type="date" />
        <span class="sep">至</span>
        <input v-model="form.end" class="ipt sm" type="date" />
      </div>
      <div v-else-if="form.ruleType === 2" class="fr">
        <span class="lb">关键词</span>
        <input v-model="form.keyword" class="ipt" placeholder="文件名或路径包含的文字" />
      </div>
      <div v-else-if="form.ruleType === 3" class="fr">
        <span class="lb">相册库</span>
        <el-select v-model="form.libraryId" placeholder="选择相册库" class="np-flex1">
          <el-option v-for="l in libraries" :key="l.id" :label="l.name" :value="l.id" />
        </el-select>
      </div>

      <div v-if="form.ruleType !== 0" class="tip">
        保存后可按规则把符合条件的照片收进相册；以后扫描到新照片，
        勾选「自动纳入」就会自动加进来。
      </div>
      <div v-else class="tip">手动相册：到照片页多选照片，点「加入相册」即可。</div>

      <label v-if="form.ruleType !== 0" class="ck">
        <input v-model="form.autoAdd" type="checkbox" /> 新扫描到的照片自动纳入
      </label>

      <template #footer>
        <button class="btn ghost" @click="dlg = false">取消</button>
        <button class="btn primary" :disabled="!form.name.trim() || saving" @click="save">
          <span v-if="saving" class="spin sm" /> {{ editing ? '保存' : '创建' }}
        </button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import AppIcon from '../components/AppIcon.vue'
import {
  listAlbums, createAlbum, updateAlbum, deleteAlbum, applyAlbumRule, listLibraries,
} from '../api'

const router = useRouter()
const albums = ref([])
const libraries = ref([])
const loading = ref(false)
const saving = ref(false)
const dlg = ref(false)
const editing = ref(null)

const RULE_TYPES = [
  { v: 0, t: '手动挑选' },
  { v: 1, t: '时间区间' },
  { v: 2, t: '关键词' },
  { v: 3, t: '整个库' },
]

const form = ref(blank())
function blank() {
  return {
    name: '', remark: '', ruleType: 0, start: '', end: '', keyword: '', libraryId: null,
    autoAdd: false,
  }
}

function withToken(u) {
  const t = localStorage.getItem('naspic.token') || ''
  return u + (u.indexOf('?') >= 0 ? '&' : '?') + 'token=' + encodeURIComponent(t)
}
function ruleText(a) {
  return (RULE_TYPES.find((r) => r.v === a.rule_type) || RULE_TYPES[0]).t
}

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

function openCreate() {
  editing.value = null
  form.value = blank()
  dlg.value = true
}
function openEdit(a) {
  editing.value = a
  const r = safeRule(a.rule_json)
  form.value = {
    name: a.name, remark: a.remark || '', ruleType: a.rule_type || 0,
    start: r.start || '', end: r.end || '', keyword: r.keyword || '',
    libraryId: r.library_id || null, autoAdd: !!a.auto_add,
  }
  dlg.value = true
}
function safeRule(s) {
  if (!s) return {}
  try { return typeof s === 'string' ? JSON.parse(s) : s } catch (e) { return {} }
}

async function save() {
  if (!form.value.name.trim()) return
  saving.value = true
  try {
    const payload = {
      name: form.value.name.trim(),
      remark: form.value.remark,
      rule_type: form.value.ruleType,
      rule: {
        start: form.value.start || '', end: form.value.end || '',
        keyword: form.value.keyword || '', library_id: form.value.libraryId || 0,
      },
      auto_add: form.value.autoAdd,
    }
    let albumId
    if (editing.value) {
      await updateAlbum(editing.value.id, payload)
      albumId = editing.value.id
    } else {
      const res = await createAlbum(payload)
      const a = res.album || res
      albumId = a.id
    }
    if (payload.rule_type !== 0) await applyAlbumRule(albumId)
    ElMessage.success(editing.value ? '已保存' : '相册已创建')
    dlg.value = false
    load()
  } catch (e) {
    ElMessage.error(e.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function reRun(a) {
  if (a.rule_type === 0) return ElMessage.info('手动相册没有规则可跑')
  try {
    const r = await applyAlbumRule(a.id, undefined, false)
    ElMessage.success(`已匹配 ${r.matched || 0} 张`)
    load()
  } catch (e) {
    ElMessage.error(e.message || '匹配失败')
  }
}

async function remove(a) {
  try {
    await ElMessageBox.confirm(
      `删除相册「${a.name}」？只删除归类关系，照片本身不受影响。`, '删除相册',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' }
    )
  } catch (e) { return }
  try {
    await deleteAlbum(a.id)
    ElMessage.success('已删除')
    load()
  } catch (e) {
    ElMessage.error(e.message || '删除失败')
  }
}

function open(a) { router.push(`/albums/${a.id}`) }

onMounted(async () => {
  load()
  try { libraries.value = await listLibraries() } catch (e) { libraries.value = [] }
})
</script>

<style scoped>
.np-row { display: flex; align-items: flex-start; gap: 12px; margin-bottom: 16px; }
.pg-t { margin: 0 0 3px; font-size: 19px; font-weight: 700; }
.pg-s { margin: 0; font-size: 12.5px; color: var(--text-weak); }

.skeleton { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 14px; }
.sk {
  height: 190px;
  border-radius: var(--r-md);
  background: linear-gradient(100deg, var(--bg-subtle) 30%, var(--bg-hover) 50%, var(--bg-subtle) 70%);
  background-size: 200% 100%;
  animation: sk 1.2s infinite;
}
@keyframes sk { to { background-position: -200% 0; } }

.empty { text-align: center; padding: 60px 20px; color: var(--text-weak); }
.empty-art {
  width: 66px; height: 66px; margin: 0 auto 14px;
  display: grid; place-items: center; border-radius: var(--r-full);
  background: var(--bg-subtle); color: var(--text-weak);
}
.empty h3 { margin: 0 0 6px; color: var(--text); font-size: 16px; }
.empty p { margin: 0 0 16px; font-size: 13px; }

.grid { display: grid; grid-template-columns: repeat(auto-fill, minmax(180px, 1fr)); gap: 14px; }
.card {
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  overflow: hidden;
  background: var(--bg-surface);
  cursor: pointer;
  transition: transform 0.16s, box-shadow 0.16s;
}
.card:hover { transform: translateY(-2px); box-shadow: var(--shadow-sm); }
.cover { position: relative; aspect-ratio: 4 / 3; background: var(--bg-subtle); }
.cover img { width: 100%; height: 100%; object-fit: cover; display: block; }
.cover-ph { width: 100%; height: 100%; display: grid; place-items: center; color: var(--text-weak); }
.cnt {
  position: absolute; right: 7px; bottom: 7px;
  padding: 1px 7px; border-radius: var(--r-full);
  background: rgba(0, 0, 0, 0.55); color: #fff; font-size: 11px;
}
.meta { padding: 9px 11px 4px; display: flex; flex-direction: column; gap: 2px; }
.nm { font-size: 13.5px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.sub { font-size: 11.5px; }
.ops { display: flex; gap: 2px; padding: 4px 8px 8px; }
.ops .lnk {
  border: none; background: transparent; cursor: pointer;
  color: var(--text-weak); padding: 4px 6px; border-radius: 6px;
}
.ops .lnk:hover { background: var(--bg-hover); color: var(--text); }
.ops .lnk.danger:hover { color: var(--danger); }

.fr { display: flex; align-items: center; gap: 9px; margin-bottom: 10px; }
.lb { width: 62px; flex-shrink: 0; font-size: 12.5px; color: var(--text-weak); }
.ipt {
  flex: 1; min-width: 0; height: 32px; padding: 0 10px;
  border: 1px solid var(--border); border-radius: var(--r-sm);
  background: var(--bg-surface); color: var(--text); font-size: 13px;
}
.ipt.sm { flex: 0 0 140px; }
.sep { color: var(--text-weak); font-size: 12.5px; }
.seg { display: flex; gap: 4px; flex-wrap: wrap; }
.seg-i {
  height: 28px; padding: 0 10px; border: 1px solid var(--border);
  background: var(--bg-surface); color: var(--text-weak);
  border-radius: var(--r-sm); font-size: 12.5px; cursor: pointer;
}
.seg-i.on { border-color: var(--brand); color: var(--brand); font-weight: 600; }
.tip { font-size: 12px; color: var(--text-weak); line-height: 1.6; }
.ck { display: flex; align-items: center; gap: 7px; margin-top: 10px; font-size: 13px; cursor: pointer; }
.dlg-head { display: flex; align-items: center; gap: 8px; font-weight: 650; }
</style>
