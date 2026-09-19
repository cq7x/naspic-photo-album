<template>
  <div class="gal" ref="rootEl">
    <!-- ================= 相册模式条 ================= -->
    <div v-if="albumId" class="album-bar">
      <button class="btn ghost sm" @click="backToAlbums">
        <AppIcon name="chevronLeft" :size="14" /> 全部相册
      </button>
      <b class="ab-nm">{{ albumName || '相册' }}</b>
      <div class="np-flex1" />
      <span class="np-muted">共 {{ items.length }} 项</span>
    </div>

    <!-- ================= 工具条 ================= -->
    <div class="toolbar">
      <div class="seg">
        <button v-for="s in SCOPES" :key="s.v" class="seg-i"
          :class="{ on: filter.scope === s.v }" @click="setScope(s.v)">
          {{ s.t }}
        </button>
      </div>

      <el-select v-model="filter.libraryId" placeholder="全部相册" clearable
        class="lib-sel" size="default">
        <el-option v-for="l in libraries" :key="l.id" :label="l.name" :value="l.id">
          <span class="opt-name">{{ l.name }}</span>
          <span class="opt-tag">{{ l.type === 1 ? '托管' : '挂载' }}</span>
        </el-option>
      </el-select>

      <div class="search">
        <AppIcon name="search" :size="15" />
        <input v-model="filter.keyword" placeholder="搜索文件名…" @keyup.enter="reload"
          @input="onSearchInput" />
        <button v-if="filter.keyword" class="clr" @click="filter.keyword = ''; reload()">
          <AppIcon name="close" :size="13" />
        </button>
      </div>

      <div class="np-flex1" />

      <div class="dens">
        <button v-for="d in DENS" :key="d.v" class="dens-i" :class="{ on: density === d.v }"
          :title="d.t" @click="density = d.v">{{ d.t }}</button>
      </div>

      <button class="btn ghost" :class="{ on: selectMode }" @click="toggleSelect">
        <AppIcon name="check" :size="15" />
        {{ selectMode ? '退出多选' : '多选' }}
      </button>

      <button class="btn primary" @click="openUpload">
        <AppIcon name="upload" :size="15" />
        上传
      </button>
    </div>

    <!-- ================= 多选条 ================= -->
    <transition name="slide">
      <div v-if="selectMode" class="selbar">
        <span class="sel-n">已选 <b>{{ selected.length }}</b></span>
        <div class="np-flex1" />
        <button class="btn ghost sm" :disabled="!selected.length" @click="openAlbumPicker">
          <AppIcon name="album" :size="14" /> 加入相册
        </button>
        <button v-if="albumId" class="btn ghost sm" :disabled="!selected.length"
          @click="removeSelectedFromAlbum">
          移出本相册
        </button>
        <button class="btn ghost sm" :disabled="!selected.length" @click="batchDownload">
          <AppIcon name="download" :size="14" /> 下载
        </button>
        <button class="btn danger sm" :disabled="!selected.length" @click="batchDelete">
          <AppIcon name="trash" :size="14" /> 删除
        </button>
        <button class="btn ghost sm" @click="selected = items.map((i) => i.id)">全选本页</button>
      </div>
    </transition>

    <!-- ================= 内容 ================= -->
    <div class="scroll" ref="scrollEl" @scroll.passive="onScroll">
      <!-- 概览条 -->
      <div v-if="stats && items.length" class="overview">
        <div class="ov-i">
          <b>{{ stats.total || 0 }}</b><span>媒体</span>
        </div>
        <div class="ov-d" />
        <div class="ov-i">
          <b>{{ stats.images || 0 }}</b><span>照片</span>
        </div>
        <div class="ov-d" />
        <div class="ov-i">
          <b>{{ stats.videos || 0 }}</b><span>视频</span>
        </div>
        <div class="ov-d" />
        <div class="ov-i">
          <b>{{ fmtSize(stats.bytes || 0) }}</b><span>占用</span>
        </div>
        <div class="np-flex1" />
        <span class="np-muted">{{ groups.length }} 个日期 · 共 {{ items.length }} 项</span>
      </div>

      <template v-for="g in groups" :key="g.key">
        <div class="dhead">
          <div class="dh-l">
            <span class="dh-d">{{ g.label }}</span>
            <span class="dh-n">{{ g.total }} 张</span>
          </div>
          <div class="dh-line" />
        </div>

        <div v-for="(row, ri) in g.rows" :key="g.key + '-' + ri" class="row"
          :style="{ height: row.h + 'px' }">
          <div v-for="m in row.items" :key="m.m.id" class="cell"
            :class="{ picked: selected.includes(m.m.id), 'is-video': m.m.media_type === 2 }"
            :style="{ width: m.w + 'px', height: row.h + 'px' }"
            @click="onCellClick(m.m)">
            <div v-if="isVideoNoPoster(m.m)" class="vid-ph">
              <AppIcon name="video" :size="26" />
            </div>
            <img v-else :src="cellSrc(m.m, row.h)" :alt="m.m.filename"
              loading="lazy" decoding="async" @error="onImgErr(m.m)" />

            <span v-if="m.m.media_type === 2" class="badge-v" title="视频">
              <AppIcon name="play" :size="12" />
            </span>
            <span v-if="m.m.favorite === 1" class="badge-f">
              <AppIcon name="star" :size="12" />
            </span>
            <!-- 视频：鼠标移上去给出「播放」图标，明确点进去是看视频 -->
            <span v-if="m.m.media_type === 2" class="vid-play">
              <span class="vp-ic"><AppIcon name="playFill" :size="20" /></span>
            </span>

            <!-- 三个操作：图标 + 悬停中文说明，避免只看到圈不知道是干嘛的 -->
            <div class="mask">
              <el-tooltip content="加入相册" placement="top" :show-after="150">
                <button class="m-btn" aria-label="加入相册" @click.stop="openAlbumPicker(m.m)">
                  <AppIcon name="album" :size="18" :stroke-width="2" />
                  <span class="m-txt">相册</span>
                </button>
              </el-tooltip>
              <el-tooltip content="下载原图" placement="top" :show-after="150">
                <button class="m-btn" aria-label="下载原图" @click.stop="download(m.m)">
                  <AppIcon name="download" :size="18" :stroke-width="2" />
                  <span class="m-txt">下载</span>
                </button>
              </el-tooltip>
              <el-tooltip content="查看详情" placement="top" :show-after="150">
                <button class="m-btn" aria-label="查看详情"
                  @click.stop="openLB(indexOf(m.m.id), true)">
                  <AppIcon name="info" :size="18" :stroke-width="2" />
                  <span class="m-txt">详情</span>
                </button>
              </el-tooltip>
            </div>

            <span v-if="selectMode" class="chk" :class="{ on: selected.includes(m.m.id) }">
              <AppIcon v-if="selected.includes(m.m.id)" name="check" :size="13" :stroke-width="3" />
            </span>
          </div>
        </div>
      </template>

      <!-- 骨架 -->
      <div v-if="loading && !items.length" class="skeleton">
        <div v-for="i in 12" :key="i" class="sk" />
      </div>

      <!-- 空态 -->
      <div v-else-if="!items.length && !loading" class="empty">
        <div class="empty-art">
          <AppIcon name="image" :size="34" />
        </div>
        <h3>{{ filter.keyword ? '没有找到匹配的照片' : '这里还空空如也' }}</h3>
        <p>
          {{
            filter.keyword
              ? '换个关键词试试，或清空搜索条件。'
              : '挂载本地只读目录建立索引，或直接上传照片到托管库。'
          }}
        </p>
        <div class="np-row" style="justify-content: center">
          <button class="btn ghost" @click="resetFilter">清空筛选</button>
          <button class="btn primary" @click="openUpload">
            <AppIcon name="upload" :size="15" /> 上传照片
          </button>
        </div>
      </div>

      <div v-if="loading && items.length" class="loading-more">
        <span class="spin" /> 加载中…
      </div>
      <div v-else-if="!hasMore && items.length" class="loading-more np-muted">
        — 已经到底了 —
      </div>
      <div ref="sentinel" style="height: 1px" />
    </div>

    <!-- ================= 上传对话框 ================= -->
    <el-dialog v-model="upDlg" width="620px" align-center class="up-dlg"
      :close-on-click-modal="false" append-to-body>
      <template #header>
        <div class="dlg-head">
          <AppIcon name="upload" :size="17" />
          <span>上传照片 / 视频</span>
        </div>
      </template>

      <div class="up-row">
        <span class="up-label">上传到</span>
        <el-select v-model="upLib" placeholder="选择相册库" class="np-flex1">
          <el-option v-for="l in writableLibs" :key="l.id" :label="l.name" :value="l.id">
            <span class="opt-name">{{ l.name }}</span>
            <span class="opt-tag">可写</span>
          </el-option>
        </el-select>
      </div>

      <div v-if="!writableLibs.length" class="up-warn">
        <AppIcon name="info" :size="15" />
        <span>还没有可写的托管库。挂载目录是只读索引不能上传，请先到「存储管理 → 新建托管库」创建一个。</span>
      </div>

      <div v-else class="drop" :class="{ over: dragOver }" @click="pickFiles"
        @dragover.prevent="dragOver = true" @dragleave.prevent="dragOver = false"
        @drop.prevent="onDrop">
        <input ref="fileInput" type="file" multiple hidden accept="image/*,video/*"
          @change="onPick" />
        <div class="drop-ic"><AppIcon name="upload" :size="22" /></div>
        <div class="drop-t">把照片拖到这里，或<b>点击选择</b>（可多选）</div>
        <div class="drop-s">同名自动重命名绝不覆盖 · 相同内容自动秒传</div>
      </div>

      <div v-if="queue.length" class="qlist">
        <div class="qlist-h">
          <span>已选择 {{ queue.length }} 个文件 · 共 {{ fmtSize(queue.reduce((s, q) => s + q.file.size, 0)) }}</span>
          <button class="lnk" @click="clearQueue">清空</button>
        </div>
        <div v-for="(q, i) in queue" :key="q.k" class="qi">
          <span class="qi-thumb" :class="{ vid: q.isVideo }">
            <img v-if="q.preview" :src="q.preview" alt="" />
            <AppIcon v-else :name="q.isVideo ? 'video' : 'image'" :size="14" />
          </span>
          <span class="qi-n" :title="q.file.name">{{ q.file.name }}</span>
          <span class="qi-s">{{ fmtSize(q.file.size) }}</span>
          <button class="lnk" @click="queue.splice(i, 1)">移除</button>
        </div>
        <div class="qlist-tip">
          点「开始上传」后任务转入后台，弹窗可以关掉，进度看右下角
        </div>
      </div>

      <template #footer>
        <button class="btn ghost" @click="upDlg = false">关闭</button>
        <button class="btn primary" :disabled="!upLib || !queue.length"
          @click="submitUpload">
          {{ `开始上传${queue.length ? `（${queue.length}）` : ''}` }}
        </button>
      </template>
    </el-dialog>

    <!-- ================= 加入相册 ================= -->
    <AlbumPicker v-model="pickerOpen" :media-ids="pickerIds" @done="onAlbumDone" />

    <!-- ================= 灯箱 ================= -->
    <transition name="lb">
      <div v-if="lb.visible" class="lightbox" tabindex="-1" ref="lbEl" @click.self="closeLB">
        <div class="lb-top">
          <span class="lb-name" :title="cur.filename">{{ cur.filename }}</span>
          <span class="lb-idx">{{ lb.index + 1 }} / {{ flat.length }}</span>
          <div class="np-flex1" />
          <div class="lb-tools">
            <button class="lb-b" title="加入相册 (F)" @click="openAlbumPicker(cur)">
              <AppIcon name="album" :size="16" />
            </button>
          <!-- 全屏播放：只对视频出现，走浏览器原生 Fullscreen API。
               全屏中按钮变成「退出全屏」，方便不用键盘的用户。 -->
          <button v-if="cur.media_type === 2" class="lb-b"
            :class="{ act: isFullscreen }"
            :title="isFullscreen ? '退出全屏 (Esc)' : '全屏播放'"
            @click="toggleFullscreen">
            <AppIcon :name="isFullscreen ? 'compress' : 'expand'" :size="16" />
          </button>
            <template v-if="cur.media_type !== 2">
            <button class="lb-b" title="缩小 (-)" @click="zoomOut">
              <AppIcon name="zoomOut" :size="16" />
            </button>
            <button class="lb-b wide" title="重置 (0)" @click="resetZoom">
              {{ Math.round(lb.scale * 100) }}%
            </button>
            <button class="lb-b" title="放大 (+)" @click="zoomIn">
              <AppIcon name="zoomIn" :size="16" />
            </button>
            </template>
            <button class="lb-b" :class="{ act: lb.showInfo }" title="信息 (I)"
              @click="lb.showInfo = !lb.showInfo">
              <AppIcon name="info" :size="16" />
            </button>
            <button class="lb-b" title="下载原图" @click="download(cur)">
              <AppIcon name="download" :size="16" />
            </button>
            <button class="lb-b danger" title="移除 (Delete)" @click="removeOne(cur)">
              <AppIcon name="trash" :size="16" />
            </button>
            <button class="lb-b" title="关闭 (Esc)" @click="closeLB">
              <AppIcon name="close" :size="16" />
            </button>
          </div>
        </div>

        <div class="lb-body">
          <div class="lb-stage" :class="{ grab: lb.scale > 1 }" @mousedown="startDrag"
            @mousemove="onDrag" @mouseup="endDrag" @mouseleave="endDrag">
            <!-- 注意：媒体元素必须在 loading 时也保持挂载。
                 之前 img 写在 v-else 里，而 v-if 的条件就是 lb.loading，
                 于是「加载中 → 不渲染 img → 永远加载不完 → 永远转圈」的死锁。
                 现在 spinner 只是叠在上层的覆盖层。 -->
            <!-- 视频：
                 · 先显示抽帧封面（poster），点画面才真正开始解码播放
                 · 点画面只在弹窗内播放/暂停，绝不进浏览器全屏
                 · controlslist 在弹窗内隐藏原生全屏按钮，避免两条全屏入口打架；
                   进入全屏后放开，用浏览器原生控件（含音量/进度/退出全屏） -->
            <video v-if="cur.media_type === 2" ref="videoEl" :src="fileUrl(cur.id)"
              :poster="videoPoster" controls preload="metadata" playsinline
              :controlslist="isFullscreen ? null : 'nofullscreen'"
              class="lb-media lb-video" :class="{ 'is-fs': isFullscreen }"
              :style="mediaStyle" @click.prevent.stop="togglePlay"
              @loadedmetadata="onVideoMeta"
              @loadeddata="lb.loading = false" @error="lb.loading = false" />
            <img v-else-if="cur.id" :key="cur.id" :src="lgUrl" :alt="cur.filename" class="lb-media"
              :style="mediaStyle" @load="lb.loading = false" @error="onLgErr" />
            <div v-if="lb.loading" class="lb-spin"><span class="spin" /></div>
          </div>

          <button class="lb-nav prev" @click="step(-1)"><AppIcon name="prev" :size="26" /></button>
          <button class="lb-nav next" @click="step(1)"><AppIcon name="next" :size="26" /></button>

          <transition name="panel">
            <aside v-if="lb.showInfo" class="lb-info">
              <div class="info-h">
                <AppIcon name="info" :size="15" /> 照片信息
              </div>
              <div class="kv"><span>文件名</span><b class="wrap">{{ cur.filename }}</b></div>
              <div class="kv"><span>拍摄时间</span><b>{{ fmtTime(cur.taken_at) }}</b></div>
              <div class="kv"><span>尺寸</span><b>{{ cur.width }} × {{ cur.height }}</b></div>
              <div class="kv"><span>大小</span><b>{{ fmtSize(cur.size_bytes) }}</b></div>
              <div class="kv">
                <span>格式</span><b>{{ (cur.ext || '').toUpperCase() }} · {{ cur.mime }}</b>
              </div>
              <div class="kv"><span>所属库</span><b>{{ detail.library_name || libName(cur.library_id) }}</b></div>
              <div class="kv"><span>路径</span><b class="wrap path">{{ cur.relative_path }}</b></div>
              <div class="kv" v-if="cur.camera_make || cur.camera_model">
                <span>设备</span>
                <b>{{ [cur.camera_make, cur.camera_model].filter(Boolean).join(' ') }}</b>
              </div>
              <div class="kv" v-if="detail.exif && Object.keys(detail.exif).length">
                <span>EXIF</span>
                <div class="exif">
                  <div v-for="(v, k) in detail.exif" :key="k"><i>{{ k }}</i>{{ v }}</div>
                </div>
              </div>
              <div class="kv"><span>哈希</span><b class="wrap path">{{ cur.hash }}</b></div>
            </aside>
          </transition>
        </div>
      </div>
    </transition>
  </div>
</template>

<script setup>
import {
  computed, h, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch,
} from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import { useRouter } from 'vue-router'
import AppIcon from '../components/AppIcon.vue'
import AlbumPicker from '../components/AlbumPicker.vue'
import {
  listMedia, mediaStats, listLibraries, updateMedia, batchMedia,
  getMediaDetail, deleteMedia, thumbUrl, fileUrl, removeFromAlbum, listAlbums,
} from '../api'
// 上传任务交给全局队列（右下角面板），不再在组件内部跑
import { enqueue } from '../store/upload'

const props = defineProps({
  preset: { type: String, default: '' },
  albumId: { type: Number, default: 0 },
})
const emit = defineEmits(['stats'])

// 相册模式：/albums/:id 进来时只显示该相册内的照片
const router = useRouter()
const albumId = computed(() => Number(props.albumId) || 0)
const albumName = ref('')
const pickerOpen = ref(false)
const pickerIds = ref([])

async function loadAlbumName() {
  if (!albumId.value) { albumName.value = ''; return }
  try {
    const d = await listAlbums()
    const a = (d.items || []).find((x) => x.id === albumId.value)
    albumName.value = a ? a.name : ''
  } catch (e) { albumName.value = '' }
}
function backToAlbums() { router.push('/albums') }

// ---------------- 筛选 ----------------
const SCOPES = [
  { v: 'all', t: '全部' },
  { v: 'image', t: '照片' },
  { v: 'video', t: '视频' },
]
const filter = reactive({
  libraryId: null,
  scope: props.preset === 'fav' ? 'fav' : 'all',
  keyword: '',
})

const items = ref([])
const libraries = ref([])
const stats = ref(null)
const loading = ref(false)
const hasMore = ref(true)
const nextCursor = ref(null)
const sentinel = ref(null)
const scrollEl = ref(null)
const rootEl = ref(null)

const selectMode = ref(false)
const selected = ref([])

// 密度（对齐行高）
const DENS = [
  { v: 's', t: '紧凑', px: 8, h: 150 },
  { v: 'm', t: '标准', px: 12, h: 210 },
  { v: 'l', t: '大图', px: 16, h: 290 },
]
const density = ref(localStorage.getItem('naspic.density') || 'm')
watch(density, (v) => localStorage.setItem('naspic.density', v))
const targetH = computed(() => (DENS.find((d) => d.v === density.value) || DENS[1]).h)
const GAP = 8

// ---------------- 数据 ----------------
function buildParams() {
  const p = { limit: 150 }
  if (filter.libraryId) p.library_id = filter.libraryId
  if (filter.scope === 'image') p.media_type = 1
  if (filter.scope === 'video') p.media_type = 2
  if (albumId.value) p.album_id = albumId.value
  const kw = filter.keyword.trim()
  if (kw) p.keyword = kw
  if (nextCursor.value && nextCursor.value.cursor_time) {
    p.cursor_time = nextCursor.value.cursor_time
    p.cursor_id = nextCursor.value.cursor_id
  }
  return p
}

async function load(reset) {
  if (loading.value) return
  if (reset) {
    nextCursor.value = null
    hasMore.value = true
    items.value = []
  }
  if (!hasMore.value) return
  loading.value = true
  try {
    const data = await listMedia(buildParams())
    const list = data.items || []
    items.value = reset ? list : items.value.concat(list)
    hasMore.value = !!data.has_more
    nextCursor.value = data.next_cursor && data.next_cursor.cursor_time
      ? data.next_cursor
      : null
  } catch (e) {
    ElMessage.error(e.message || '加载失败')
  } finally {
    loading.value = false
  }
}

async function loadLibraries() {
  try {
    libraries.value = await listLibraries()
  } catch { /* ignore */ }
}

async function loadStats() {
  try {
    const s = await mediaStats(filter.libraryId || 0)
    stats.value = s
    emit('stats', s)
  } catch { /* ignore */ }
}

function reload() {
  loadStats()
  load(true)
}

let searchTimer = null
function onSearchInput() {
  clearTimeout(searchTimer)
  searchTimer = setTimeout(reload, 350)
}

function setScope(v) {
  filter.scope = v
  reload()
}
function resetFilter() {
  filter.libraryId = null
  filter.keyword = ''
  filter.scope = 'all'
  reload()
}

watch(() => filter.libraryId, () => reload())
watch(selectMode, () => { selected.value = [] })

// ---------------- 分组 + 对齐布局 ----------------
const wrapW = ref(1000)
let ro = null

const flat = computed(() => items.value)

const groups = computed(() => {
  const map = new Map()
  for (const m of items.value) {
    const key = dateKey(m.taken_at || m.created_at)
    if (!map.has(key)) map.set(key, [])
    map.get(key).push(m)
  }
  const out = []
  for (const [key, list] of map) {
    out.push({
      key,
      label: fmtDateLabel(key),
      total: list.length,
      rows: justify(list, wrapW.value, targetH.value, GAP),
    })
  }
  return out
})

function justify(list, cw, th, gap) {
  if (!list.length || cw <= 0) return []
  const out = []
  let row = []
  let sum = 0
  const flush = (isLast) => {
    if (!row.length) return
    const gaps = gap * (row.length - 1)
    let h = (cw - gaps) / sum
    if (isLast && h > th * 1.3) h = th * 1.3
    if (h < 90) h = 90
    out.push({
      h,
      items: row.map((it) => ({ m: it.m, w: Math.round(it.r * h) })),
    })
    row = []
    sum = 0
  }
  for (const m of list) {
    const r = m.width && m.height ? m.width / m.height : 1.5
    row.push({ m, r })
    sum += r
    const h = (cw - gap * (row.length - 1)) / sum
    if (h <= th) flush(false)
  }
  flush(true)
  return out
}

function dateKey(t) {
  if (!t) return '未知日期'
  const d = new Date(t)
  if (isNaN(d.getTime())) return '未知日期'
  return `${d.getFullYear()}-${String(d.getMonth() + 1).padStart(2, '0')}-${String(d.getDate()).padStart(2, '0')}`
}
function fmtDateLabel(key) {
  if (key === '未知日期') return key
  const [y, m, d] = key.split('-')
  const today = new Date()
  const tk = dateKey(today.toISOString())
  if (key === tk) return '今天'
  const yest = new Date(today.getTime() - 86400000)
  if (key === dateKey(yest.toISOString())) return '昨天'
  const sameYear = String(today.getFullYear()) === y
  return sameYear ? `${Number(m)} 月 ${Number(d)} 日` : `${y} 年 ${Number(m)} 月 ${Number(d)} 日`
}
// 缩略图加载失败时自动退回原图直链，避免出现破图（只回退一次，防止死循环）
function cellSrc(m, rowH) {
  // 视频缩略图就是抽帧封面；拿不到封面时不要退回 fileUrl（<img> 放不了视频，只会破图）
  if (m._err) return m.media_type === 2 ? '' : fileUrl(m.id)
  return thumbUrl(m.id, rowH > 260 ? 'md' : 'sm')
}
function isVideoNoPoster(m) {
  return m.media_type === 2 && m._err === true
}
function onImgErr(m) {
  if (!m._err) m._err = true
}

function libName(id) {
  const l = libraries.value.find((x) => x.id === id)
  return l ? l.name : id
}
function indexOf(id) {
  return flat.value.findIndex((x) => x.id === id)
}

// ---------------- 交互 ----------------
function onCellClick(m) {
  if (selectMode.value) {
    const i = selected.value.indexOf(m.id)
    if (i >= 0) selected.value.splice(i, 1)
    else selected.value.push(m.id)
    return
  }
  openLB(indexOf(m.id))
}

async function openAlbumPicker(m) {
  const ids = m && m.id ? [m.id] : selected.value.slice()
  if (!ids.length) return ElMessage.warning('请先选择照片')
  pickerIds.value = ids
  pickerOpen.value = true
}

async function removeSelectedFromAlbum() {
  if (!albumId.value || !selected.value.length) return
  try {
    await removeFromAlbum(albumId.value, selected.value.slice())
    ElMessage.success('已移出相册')
    selectMode.value = false
    reload()
  } catch (e) {
    ElMessage.error(e.message || '操作失败')
  }
}

function onAlbumDone() {
  selectMode.value = false
  reload()
}

function download(m) {
  const a = document.createElement('a')
  a.href = fileUrl(m.id, true)
  a.download = m.filename || ''
  document.body.appendChild(a)
  a.click()
  a.remove()
}


// 只有"托管库"（平台真正拥有的文件）才允许连源文件一起删。
// 挂载目录一律除外——那是别人/别的设备的目录，平台只建索引，绝不碰原图。
function canDeleteSource(m) {
  const l = libraries.value.find((x) => x.id === m?.library_id)
  return !!l && l.type === 1 && !!l.writable
}

// 带「同时删除源文件」勾选的确认框。
// 挂载目录的条目不显示该选项（不可选），从源头上避免误删宿主机原图。
function confirmDelete(opts) {
  const { title, desc, deletable } = opts
  const physical = ref(false)
  const nodes = [h('div', { style: 'line-height:1.7' }, desc)]
  if (deletable) {
    nodes.push(
      h('label', {
        style:
          'display:flex;align-items:center;gap:7px;margin-top:10px;font-size:13px;cursor:pointer',
      }, [
        h('input', {
          type: 'checkbox',
          style: 'width:15px;height:15px;accent-color:#f56c6c;cursor:pointer',
          onChange: (e) => { physical.value = e.target.checked },
        }),
        h('span', null, '同时删除源文件（不可恢复）'),
      ])
    )
  } else {
    nodes.push(
      h('div', {
        style: 'margin-top:10px;font-size:12px;color:var(--el-color-info)',
      }, '挂载目录：源文件受保护，只会移除索引记录。')
    )
  }
  return ElMessageBox({
    title,
    message: h('div', null, nodes),
    showCancelButton: true,
    confirmButtonText: '删除',
    cancelButtonText: '取消',
    type: 'warning',
    confirmButtonClass: 'el-button--danger',
  }).then(() => physical.value)
}

async function batchDelete() {
  const ids = selected.value.slice()
  if (!ids.length) return
  const list = ids.map((id) => flat.value.find((x) => x.id === id)).filter(Boolean)
  const deletable = list.filter(canDeleteSource).length
  const mounted = list.length - deletable
  let physical = false
  try {
    physical = await confirmDelete({
      title: '批量删除',
      desc: `将移除选中的 ${ids.length} 项索引记录。`
        + (mounted ? `其中 ${mounted} 项来自挂载目录，不会删除宿主机原图。` : ''),
      deletable: deletable > 0,
    })
  } catch {
    return
  }
  try {
    const r = await batchMedia(ids, 'delete', physical)
    const pd = r?.data?.physical_deleted || 0
    ElMessage.success(physical ? `已移除；删除源文件 ${pd} 个` : '已移除索引')
    selectMode.value = false
    reload()
  } catch (e) {
    ElMessage.error(e.message || '操作失败')
  }
}

function batchDownload() {
  selected.value.forEach((id, i) => {
    setTimeout(() => {
      const a = document.createElement('a')
      a.href = fileUrl(id, true)
      document.body.appendChild(a)
      a.click()
      a.remove()
    }, i * 220)
  })
}

async function removeOne(m) {
  const deletable = canDeleteSource(m)
  let physical = false
  try {
    physical = await confirmDelete({
      title: '删除照片',
      desc: `「${m.filename}」`,
      deletable,
    })
  } catch {
    return
  }
  try {
    await deleteMedia(m.id, physical)
    ElMessage.success(physical ? '已删除（含源文件）' : '已移除索引')
    closeLB()
    reload()
  } catch (e) {
    ElMessage.error(e.message || '操作失败')
  }
}

function toggleSelect() {
  selectMode.value = !selectMode.value
}

// ---------------- 灯箱 ----------------
const lb = reactive({ visible: false, index: 0, scale: 1, showInfo: false, loading: false })
// 信息面板开合会挤压视频可用区域，重算一次矩形
watch(() => lb.showInfo, () => nextTick(fitVideo))
const lbEl = ref(null)
const drag = reactive({ on: false, x: 0, y: 0, dx: 0, dy: 0 })
const detail = ref({})
const lgFail = ref(false)

const cur = computed(() => flat.value[lb.index] || {})
const lgUrl = computed(() => (lgFail.value ? fileUrl(cur.value.id) : `/api/v1/media/${cur.value.id}/thumb?size=lg&token=${encodeURIComponent(localStorage.getItem('naspic.token') || '')}`))

const mediaStyle = computed(() => ({
  transform: `translate(${drag.dx}px, ${drag.dy}px) scale(${lb.scale})`,
  // 视频不参与缩放，光标别再显示成「带加号的放大镜」，否则点哪儿都像要放大
  cursor:
    cur.value.media_type === 2
      ? 'default'
      : lb.scale > 1
        ? drag.on
          ? 'grabbing'
          : 'grab'
        : 'zoom-in',
}))
// 视频封面：先显示抽帧出来的静态图，点播放才开始解码
const videoPoster = computed(() =>
  cur.value.media_type === 2 && cur.value.id ? thumbUrl(cur.value.id, 'md') : ''
)

// ---------------- 视频全屏（浏览器原生 Fullscreen API） ----------------
const videoEl = ref(null)
const isFullscreen = ref(false)

// 各浏览器前缀（Safari 还要 webkitFullscreenElement）
function fsElement() {
  return document.fullscreenElement || document.webkitFullscreenElement || null
}
function fsExit() {
  const d = document
  const p = d.exitFullscreen ? d.exitFullscreen()
    : d.webkitExitFullscreen ? d.webkitExitFullscreen() : null
  return p && p.catch ? p : Promise.resolve()
}

function onFullscreenChange() {
  const on = !!fsElement()
  isFullscreen.value = on
  if (on) {
    // 清掉 JS 算的内联 px 尺寸，让 .is-fs 的铺满规则接管
    if (videoEl.value) {
      videoEl.value.style.width = ''
      videoEl.value.style.height = ''
    }
    return
  }
  // 退出全屏：回到弹窗内播放，重新按可用区域算尺寸
  nextTick(fitVideo)
  if (!on && videoEl.value && lb.visible === false) {
    try { videoEl.value.pause() } catch (e) { /* noop */ }
  }
}

async function enterFullscreen() {
  const el = videoEl.value
  if (!el) return
  // iOS Safari：<video> 不支持 requestFullscreen，只能走 webkitEnterFullscreen
  if (!el.requestFullscreen && el.webkitEnterFullscreen) {
    try {
      el.webkitEnterFullscreen()
      isFullscreen.value = true
    } catch (e) {
      fsFail(e)
    }
    return
  }
  if (!el.requestFullscreen) {
    fsFail(new Error('unsupported'))
    return
  }
  try {
    const p = el.requestFullscreen()
    if (p && p.catch) await p
  } catch (e) {
    fsFail(e)
  }
}

// 全屏失败的原因很多：被 iframe 策略拦、非用户手势触发、浏览器设置禁用……
// 统一给一句人话，别让用户对着控制台发呆。
function fsFail(err) {
  const name = (err && err.name) || ''
  let msg = '当前浏览器不允许进入全屏播放'
  if (name === 'NotAllowedError') {
    msg = '浏览器拒绝了全屏请求：请直接点击「全屏播放」按钮（不能由脚本自动触发），或检查是否禁止了本站全屏权限'
  } else if (name === 'NotSupportedError' || name === 'unsupported') {
    msg = '该浏览器不支持全屏播放，可改用右下角的原生播放器或系统播放器打开'
  } else if (name === 'SecurityError') {
    msg = '浏览器安全策略禁止了全屏（常见于被嵌套的页面），请在新标签页中打开本页再试'
  }
  isFullscreen.value = false
  ElMessage({
    type: 'warning',
    message: msg,
    duration: 5000,
    showClose: true,
  })
}

function exitFullscreen() {
  fsExit().catch(() => { /* 没在全屏时调用会 reject，忽略即可 */ })
}
function toggleFullscreen() {
  if (isFullscreen.value) exitFullscreen()
  else enterFullscreen()
}

// 点画面：只在弹窗内播放/暂停，不触发任何全屏行为。
// 注意必须 preventDefault —— 否则浏览器自己的「点击画面切换播放」会在我们
// pause() 之后再 toggle 一次，表现就是「点了暂停没反应、还在播」。
function togglePlay() {
  const el = videoEl.value
  if (!el) return
  if (el.paused) {
    const p = el.play()
    if (p && p.catch) p.catch(() => { /* 自动播放被拦时静默，用户再点一次即可 */ })
  } else {
    el.pause()
  }
}

// ---- 视频适配：按「长边最大化」精确算出画面矩形 ----
// 元数据到位后才能知道真实比例（videoWidth/videoHeight），这时才算尺寸；
// 窗口缩放、信息面板开合、密度切换都会改变可用区域，统一走 ResizeObserver 重算。
function fitVideo() {
  const el = videoEl.value
  if (!el || !el.videoWidth || !el.videoHeight) return
  if (isFullscreen.value) return // 全屏交给 CSS 铺满
  const stage = el.parentElement
  if (!stage) return
  const cs = getComputedStyle(stage)
  const aw = stage.clientWidth - parseFloat(cs.paddingLeft || 0) - parseFloat(cs.paddingRight || 0)
  const ah = stage.clientHeight - parseFloat(cs.paddingTop || 0) - parseFloat(cs.paddingBottom || 0)
  if (aw <= 0 || ah <= 0) return
  const ar = el.videoWidth / el.videoHeight
  let w = aw
  let h = w / ar
  if (h > ah) { h = ah; w = h * ar }
  el.style.width = Math.floor(w) + 'px'
  el.style.height = Math.floor(h) + 'px'
}
function onVideoMeta() {
  lb.loading = false
  fitVideo()
}

// 浏览器只发 error 事件、不 reject Promise 的情况（Safari/老 Chrome）
function onFullscreenError() {
  if (isFullscreen.value) return // 已经成功了就别误报
  fsFail({ name: 'NotAllowedError' })
}

function openLB(i, withInfo = false) {
  if (i < 0) return
  lb.index = i
  lb.scale = 1
  lb.showInfo = withInfo
  armLoading()
  lgFail.value = false
  drag.dx = 0
  drag.dy = 0
  lb.visible = true
  nextTick(() => {
    if (lbEl.value && lbEl.value.focus) lbEl.value.focus()
    fitVideo()
  })
  loadDetail()
}
function closeLB() {
  // 先退出全屏再关弹窗：否则页面元素被移除了，全屏状态还挂着，
  // 浏览器会退回一个空白全屏（体验很糟）
  if (isFullscreen.value) exitFullscreen()
  if (videoEl.value) {
    try { videoEl.value.pause() } catch (e) { /* noop */ }
  }
  lb.visible = false
  clearTimeout(lbTimer)
}
function step(d) {
  const n = lb.index + d
  if (n < 0 || n >= flat.value.length) return
  // 切下一个前先退出全屏并暂停，避免上一条视频在后台继续响
  if (isFullscreen.value) exitFullscreen()
  if (videoEl.value) {
    try { videoEl.value.pause() } catch (e) { /* noop */ }
  }
  lb.index = n
  lb.scale = 1
  drag.dx = 0
  drag.dy = 0
  lgFail.value = false
  armLoading()
  loadDetail()
  nextTick(fitVideo)
  if (n > flat.value.length - 20) load(false)
}

// armLoading 开启加载态，并配一个兜底定时器。
// 图片若命中浏览器缓存，load 事件可能早于监听器绑定触发，
// 那样 loading 会永远停在 true；有这个兜底，最多转 10 秒就会让位给图片。
let lbTimer = null
function armLoading() {
  lb.loading = true
  clearTimeout(lbTimer)
  lbTimer = setTimeout(() => {
    lb.loading = false
  }, 10000)
}

// onLgErr 大图加载失败：退回原图直链重试，只退一次，避免来回横跳
function onLgErr() {
  lb.loading = false
  if (!lgFail.value) {
    lgFail.value = true
  }
}
function zoomIn() { lb.scale = Math.min(5, +(lb.scale + 0.25).toFixed(2)) }
function zoomOut() { lb.scale = Math.max(0.25, +(lb.scale - 0.25).toFixed(2)) }
function resetZoom() { lb.scale = 1; drag.dx = 0; drag.dy = 0 }

function startDrag(e) {
  if (lb.scale <= 1) return
  drag.on = true
  drag.x = e.clientX
  drag.y = e.clientY
}
function onDrag(e) {
  if (!drag.on) return
  drag.dx += e.clientX - drag.x
  drag.dy += e.clientY - drag.y
  drag.x = e.clientX
  drag.y = e.clientY
}
function endDrag() { drag.on = false }

async function loadDetail() {
  const id = cur.value.id
  if (!id) return
  try {
    detail.value = await getMediaDetail(id)
  } catch {
    detail.value = {}
  }
}

function onKey(e) {
  if (!lb.visible) return
  if (e.key === 'Escape') {
    // 第一次 ESC：只退出全屏，弹窗留着（"再按一次才返回相册"）。
    // 注意：多数浏览器在全屏状态下会自己吃掉 ESC 用来退出全屏，
    // 这一路 keydown 根本不会派发到页面；这里兜住的是「派发了」的那些浏览器，
    // 两种情况最终效果一致：第一次退全屏，第二次关弹窗。
    if (isFullscreen.value) {
      exitFullscreen()
      return
    }
    closeLB()
  }
  else if (e.key === 'ArrowLeft') step(-1)
  else if (e.key === 'ArrowRight') step(1)
  else if (e.key === '+' || e.key === '=') zoomIn()
  else if (e.key === '-') zoomOut()
  else if (e.key === '0') resetZoom()
  else if (e.key === 'i' || e.key === 'I') lb.showInfo = !lb.showInfo
  else if (e.key === 'f' || e.key === 'F') openAlbumPicker(cur.value)
}

// ---------------- 上传 ----------------
const upDlg = ref(false)
const upLib = ref(null)
const dragOver = ref(false)
const fileInput = ref(null)
const queue = ref([]) // 弹窗里的「待上传清单」，点开始上传后交给全局任务队列

const writableLibs = computed(() => libraries.value.filter((l) => l.writable))

function openUpload() {
  if (!libraries.value.length) loadLibraries()
  if (!upLib.value && writableLibs.value.length) upLib.value = writableLibs.value[0].id
  upDlg.value = true
}
function pickFiles() { fileInput.value && fileInput.value.click() }
function onPick(e) { addFiles(e.target.files); e.target.value = '' }
function onDrop(e) { dragOver.value = false; addFiles(e.dataTransfer.files) }

// 本地即时预览：图片直接 objectURL（零成本、渲染同步出现）；
// 视频走 canvas 抽帧（异步，拿到后回填，不阻塞列表出现）。
function makePreview(f) {
  const isVideo = (f.type || '').indexOf('video/') === 0 ||
    /\.(mp4|mov|m4v|mkv|avi|webm|flv|3gp|ts|mpg|mpeg)$/i.test(f.name || '')
  const item = { file: f, pct: 0, state: 'wait', msg: '', preview: '', isVideo, dedup: false }
  if (!isVideo && /^image\//.test(f.type || '')) {
    try { item.preview = URL.createObjectURL(f) } catch (e) { item.preview = '' }
    return item
  }
  if (isVideo) grabVideoFrame(f, 1).then((u) => { if (u) item.preview = u })
  return item
}

function grabVideoFrame(file, seekSec) {
  return new Promise((resolve) => {
    let url = ''
    try { url = URL.createObjectURL(file) } catch (e) { return resolve('') }
    const v = document.createElement('video')
    v.preload = 'metadata'
    v.muted = true
    v.playsInline = true
    let done = false
    const finish = (out) => {
      if (done) return
      done = true
      clearTimeout(timer)
      try { URL.revokeObjectURL(url) } catch (e) { /* noop */ }
      resolve(out || '')
    }
    const timer = setTimeout(() => finish(''), 8000)
    const shot = () => {
      try {
        const vw = v.videoWidth || 160
        const vh = v.videoHeight || 90
        const W = 160
        const c = document.createElement('canvas')
        c.width = W
        c.height = Math.max(1, Math.round((W * vh) / vw))
        c.getContext('2d').drawImage(v, 0, 0, c.width, c.height)
        finish(c.toDataURL('image/jpeg', 0.72))
      } catch (e) { finish('') }
    }
    v.onloadeddata = () => {
      try {
        const d = v.duration || 1
        v.currentTime = Math.min(seekSec || 1, d * 0.1)
      } catch (e) { shot() }
      setTimeout(shot, 1500) // 部分格式 seeked 不触发，兜底直接截
    }
    v.onseeked = shot
    v.onerror = () => finish('')
    v.src = url
  })
}

let seq = 0
// 选完只列清单，等用户点「开始上传」再交给后台任务队列。
// 之前是选完自动开传，一批选错就得等全部传完才能改，很别扭。
function addFiles(list) {
  const arr = Array.from(list || [])
  if (!arr.length) return
  for (const f of arr) {
    const it = makePreview(f)
    it.k = 'q' + ++seq
    queue.value.push(it)
  }
}

function revokePreviews() {
  queue.value.forEach((q) => {
    if (q.preview && q.preview.indexOf('blob:') === 0) {
      try { URL.revokeObjectURL(q.preview) } catch (e) { /* noop */ }
    }
  })
}
function clearQueue() {
  revokePreviews()
  queue.value = []
}

// 上传过程中每成功一个就刷新一次列表（去抖），新照片随传随出现，
// 不用等全部传完才刷新 —— 之前「传完半天才在列表里看到」就是这个原因。
let refreshTimer = null
function refreshSoon() {
  if (refreshTimer) clearTimeout(refreshTimer)
  refreshTimer = setTimeout(() => {
    refreshTimer = null
    if (loading.value) return refreshSoon()
    // 用户已经翻到下面去了就别把他拽回顶部，等他滚回来自然能看到
    const el = scrollEl.value
    if (el && el.scrollTop > 600) return
    reload()
  }, 800)
}

// 点「开始上传」：把清单交给全局任务队列，弹窗立刻可以关掉。
// 上传在后台跑，进度看右下角面板，每成功一个列表就自动刷新。
function submitUpload() {
  if (!upLib.value) return ElMessage.warning('请选择上传到哪个相册库')
  const items = queue.value.filter((q) => q.state === 'wait' || q.state === 'err')
  if (!items.length) return
  // preview 交给任务面板继续用，这里不能 revoke，否则缩略图会变成裂图
  enqueue(items.map((q) => ({ file: q.file, preview: q.preview, isVideo: q.isVideo })), upLib.value)
  queue.value = []
  upDlg.value = false
  ElMessage.success(`已加入上传队列 ${items.length} 个文件，可在右下角查看进度`)
}

// ---------------- 工具 ----------------
function fmtSize(b) {
  if (!b) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(u.length - 1, Math.floor(Math.log(b) / Math.log(1024)))
  return (b / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1) + ' ' + u[i]
}
function fmtTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  return isNaN(d.getTime()) ? t : d.toLocaleString('zh-CN')
}

// ---------------- 生命周期 ----------------
function onScroll() {
  const el = scrollEl.value
  if (!el) return
  if (el.scrollHeight - el.scrollTop - el.clientHeight < 900) load(false)
}

let io = null
onMounted(() => {
  loadLibraries()
  loadStats()
  load(true)
  loadAlbumName()
  window.addEventListener('keydown', onKey)
  // 全屏状态变化（含用户按 ESC、点原生控件退出）统一在这里同步
  document.addEventListener('fullscreenchange', onFullscreenChange)
  document.addEventListener('webkitfullscreenchange', onFullscreenChange)
  // 全屏请求被拒时浏览器只发这个事件，Promise 不一定 reject，必须单独兜
  document.addEventListener('fullscreenerror', onFullscreenError)
  document.addEventListener('webkitfullscreenerror', onFullscreenError)
  if (rootEl.value) {
    wrapW.value = Math.max(320, rootEl.value.clientWidth - 44)
    if (window.ResizeObserver) {
      ro = new ResizeObserver(() => {
        wrapW.value = Math.max(320, rootEl.value.clientWidth - 44)
        // 可用区域变了（窗口缩放、信息面板开合）就重算视频矩形
        fitVideo()
      })
      ro.observe(rootEl.value)
    } else {
      window.addEventListener('resize', () => {
        wrapW.value = Math.max(320, rootEl.value.clientWidth - 44)
        fitVideo()
      })
    }
  }
})
onBeforeUnmount(() => {
  window.removeEventListener('keydown', onKey)
  window.removeEventListener('naspic:upload-progress', refreshSoon)
  document.removeEventListener('fullscreenchange', onFullscreenChange)
  document.removeEventListener('webkitfullscreenchange', onFullscreenChange)
  document.removeEventListener('fullscreenerror', onFullscreenError)
  document.removeEventListener('webkitfullscreenerror', onFullscreenError)
  if (ro) ro.disconnect()
  if (io) io.disconnect()
  if (refreshTimer) clearTimeout(refreshTimer)
  revokePreviews()
})
</script>

<style scoped>
.gal {
  display: flex;
  flex-direction: column;
  height: 100%;
  min-height: 0;
}

/* ---------------- 工具条 ---------------- */
.toolbar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px 22px;
  background: var(--bg-surface);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
  flex-wrap: wrap;
}

.seg {
  display: flex;
  padding: 3px;
  gap: 2px;
  background: var(--bg-subtle);
  border-radius: var(--r-sm);
}
.seg-i {
  border: none;
  background: transparent;
  color: var(--text-sub);
  font-size: 13px;
  font-weight: 500;
  padding: 5px 13px;
  border-radius: 6px;
  cursor: pointer;
  transition: all 0.16s;
}
.seg-i:hover { color: var(--text); }
.seg-i.on {
  background: var(--bg-surface);
  color: var(--brand);
  font-weight: 600;
  box-shadow: var(--shadow-xs);
}

.lib-sel { width: 158px; }

.search {
  display: flex;
  align-items: center;
  gap: 7px;
  height: 34px;
  padding: 0 10px;
  min-width: 200px;
  border-radius: var(--r-sm);
  background: var(--bg-subtle);
  border: 1px solid transparent;
  color: var(--text-weak);
  transition: border-color 0.16s, background 0.16s;
}
.search:focus-within {
  border-color: var(--brand);
  background: var(--bg-surface);
  box-shadow: 0 0 0 3px var(--brand-ring);
}
.search input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  color: var(--text);
  font-size: 13px;
  font-family: inherit;
}
.search input::placeholder { color: var(--text-weak); }
.clr {
  border: none;
  background: transparent;
  color: var(--text-weak);
  cursor: pointer;
  display: grid;
  place-items: center;
  padding: 2px;
  border-radius: 4px;
}
.clr:hover { color: var(--text); background: var(--bg-hover); }

.dens {
  display: flex;
  gap: 3px;
  padding: 3px 4px;
  background: var(--bg-subtle);
  border-radius: var(--r-sm);
}
.dens-i {
  height: 26px;
  padding: 0 9px;
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 2px;
  border: none;
  background: transparent;
  border-radius: 5px;
  cursor: pointer;
  font-size: 12.5px;
  color: var(--text-weak);
  white-space: nowrap;
  transition: background 0.16s, color 0.16s;
}
.dens-i:hover { color: var(--text-sub); }
.dens-i.on {
  background: var(--bg-surface);
  box-shadow: var(--shadow-xs);
  color: var(--brand);
  font-weight: 600;
}

/* ---------------- 按钮 ---------------- */
.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 34px;
  padding: 0 14px;
  border-radius: var(--r-sm);
  border: 1px solid var(--border);
  background: var(--bg-surface);
  color: var(--text);
  font-size: 13px;
  font-weight: 500;
  font-family: inherit;
  cursor: pointer;
  transition: all 0.16s;
  white-space: nowrap;
}
.btn:hover:not(:disabled) { background: var(--bg-hover); border-color: var(--border-strong); }
.btn:disabled { opacity: 0.45; cursor: not-allowed; }
.btn.primary {
  background: var(--brand);
  border-color: var(--brand);
  color: #fff;
  font-weight: 600;
  box-shadow: 0 2px 8px rgba(43, 92, 255, 0.25);
}
.btn.primary:hover:not(:disabled) {
  background: var(--brand-hover);
  border-color: var(--brand-hover);
}
.btn.ghost.on {
  background: var(--brand-soft);
  border-color: var(--brand);
  color: var(--brand);
  font-weight: 600;
}
.btn.danger { color: var(--danger); border-color: var(--border); }
.btn.danger:hover:not(:disabled) {
  background: rgba(240, 68, 56, 0.08);
  border-color: var(--danger);
}
.btn.sm { height: 30px; padding: 0 11px; font-size: 12.5px; }

/* ---------------- 多选条 ---------------- */
.selbar {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 9px 22px;
  background: var(--brand-soft);
  border-bottom: 1px solid var(--border);
  flex-shrink: 0;
}
.sel-n { font-size: 13px; color: var(--text-sub); }
.sel-n b { color: var(--brand); font-size: 15px; }
.slide-enter-active, .slide-leave-active { transition: all 0.2s ease; }
.slide-enter-from, .slide-leave-to { opacity: 0; transform: translateY(-6px); }

/* ---------------- 滚动区 ---------------- */
.scroll {
  flex: 1;
  min-height: 0;
  overflow-y: auto;
  padding: 16px 22px 60px;
}

.overview {
  display: flex;
  align-items: center;
  gap: 18px;
  padding: 12px 18px;
  margin-bottom: 14px;
  border-radius: var(--r-lg);
  background: var(--bg-surface);
  border: 1px solid var(--border);
  box-shadow: var(--shadow-xs);
}
.ov-i { display: flex; flex-direction: column; line-height: 1.25; }
.ov-i b { font-size: 19px; font-weight: 700; }
.ov-i span { font-size: 12px; color: var(--text-weak); }
.ov-d { width: 1px; height: 26px; background: var(--border); }

.dhead {
  display: flex;
  align-items: center;
  gap: 14px;
  margin: 20px 0 8px;
  position: sticky;
  top: -16px;
  z-index: 3;
  padding: 6px 0;
}
.dhead::before {
  content: '';
  position: absolute;
  inset: -6px -22px;
  background: var(--bg-app);
  opacity: 0.94;
  z-index: -1;
  backdrop-filter: blur(6px);
}
.dh-l { display: flex; align-items: baseline; gap: 9px; flex-shrink: 0; }
.dh-d { font-size: 15px; font-weight: 700; letter-spacing: 0.3px; }
.dh-n { font-size: 12px; color: var(--text-weak); }
.dh-line { flex: 1; height: 1px; background: var(--border); }

/* ---------------- 对齐行 ---------------- */
.row {
  display: flex;
  gap: 8px;
  margin-bottom: 8px;
}
.cell {
  position: relative;
  border-radius: var(--r-md);
  overflow: hidden;
  background: var(--bg-subtle);
  cursor: pointer;
  flex-shrink: 0;
  container-type: inline-size;
  transition: transform 0.16s cubic-bezier(0.4, 0, 0.2, 1), box-shadow 0.16s;
}
/* 窄格子放不下"图标+文字"，退化成纯图标圆钮（仍然有图标和悬停提示） */
@container (max-width: 230px) {
  .m-txt { display: none; }
  .m-btn { width: 30px; padding: 0; justify-content: center; }
}
.cell::after {
  content: '';
  position: absolute;
  inset: 0;
  border-radius: inherit;
  box-shadow: inset 0 0 0 1px rgba(0, 0, 0, 0.05);
  pointer-events: none;
}
.cell:hover { transform: translateY(-2px); box-shadow: var(--shadow-md); z-index: 2; }
.cell img {
  width: 100%;
  height: 100%;
  object-fit: cover;
  display: block;
  background: var(--bg-subtle);
}
.cell.picked { outline: 3px solid var(--brand); outline-offset: -3px; }

.album-bar {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 8px 12px;
  margin-bottom: 10px;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--bg-subtle);
  font-size: 13px;
}
.ab-nm { font-size: 14px; }

/* 视频：悬停只保留中间的播放圆圈，底部胶囊与右上角角标都让位，避免图标打架 */
.cell.is-video:hover .mask { opacity: 0; visibility: hidden; }
.badge-v, .badge-f {
  position: absolute;
  top: 7px;
  width: 24px;
  height: 24px;
  display: grid;
  place-items: center;
  border-radius: var(--r-full);
  color: #fff;
  backdrop-filter: blur(6px);
  pointer-events: none;
  transition: opacity 0.18s;
}
.cell.is-video:hover .badge-v { opacity: 0; }
.badge-v { right: 7px; background: rgba(0, 0, 0, 0.5); }
.badge-f { left: 7px; background: rgba(255, 185, 60, 0.92); }

/* 视频：抽不到封面时的占位块，避免出现破图 */
.vid-ph {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  background: var(--bg-subtle);
  color: var(--text-weak);
}

/* 视频：悬停时给出播放图标 */
.vid-play {
  position: absolute;
  inset: 0;
  display: grid;
  place-items: center;
  opacity: 0;
  pointer-events: none;
  transition: opacity 0.18s;
}
.cell:hover .vid-play { opacity: 1; }
.vp-ic {
  width: 46px;
  height: 46px;
  border-radius: var(--r-full);
  display: grid;
  place-items: center;
  color: #fff;
  background: rgba(0, 0, 0, 0.46);
  backdrop-filter: blur(4px);
  box-shadow: 0 2px 10px rgba(0, 0, 0, 0.25);
}
@container (max-width: 230px) { .vp-ic { width: 36px; height: 36px; } }

.mask {
  position: absolute;
  inset: 0;
  display: flex;
  align-items: flex-end;
  justify-content: center;
  gap: 6px;
  flex-wrap: wrap;
  padding: 6px 6px 9px;
  opacity: 0;
  visibility: hidden;
  background: linear-gradient(to top, rgba(0, 0, 0, 0.62) 0%, rgba(0, 0, 0, 0) 60%);
  transition: opacity 0.18s, visibility 0.18s;
}
.cell:hover .mask, .cell.picked .mask { opacity: 1; visibility: visible; }
/* 图标 + 中文文字的胶囊按钮：一眼就知道是干嘛的，不用猜圆圈 */
.m-btn {
  height: 30px;
  padding: 0 10px;
  display: inline-flex;
  align-items: center;
  gap: 4px;
  border: 1px solid rgba(255, 255, 255, 0.34);
  border-radius: var(--r-full);
  background: rgba(16, 18, 22, 0.58);
  backdrop-filter: blur(8px);
  -webkit-backdrop-filter: blur(8px);
  color: #fff;
  cursor: pointer;
  transition: transform 0.14s, background 0.14s, border-color 0.14s, color 0.14s;
}
.m-btn:hover {
  transform: translateY(-1px);
  background: rgba(255, 255, 255, 0.94);
  border-color: transparent;
  color: #1a1d23;
}
.m-btn.act { background: #ff5f8f; border-color: #ff5f8f; color: #fff; }
.m-txt { font-size: 12px; font-weight: 500; line-height: 1; white-space: nowrap; }

.chk {
  position: absolute;
  top: 7px;
  right: 7px;
  width: 21px;
  height: 21px;
  border-radius: var(--r-full);
  border: 2px solid rgba(255, 255, 255, 0.9);
  background: rgba(0, 0, 0, 0.28);
  display: grid;
  place-items: center;
  color: #fff;
  transition: all 0.16s;
}
.chk.on { background: var(--brand); border-color: #fff; }

/* ---------------- 骨架 / 空态 ---------------- */
.skeleton {
  display: grid;
  grid-template-columns: repeat(auto-fill, minmax(160px, 1fr));
  gap: 10px;
}
.sk {
  height: 160px;
  border-radius: var(--r-md);
  background: linear-gradient(100deg, var(--bg-subtle) 30%, var(--bg-hover) 50%, var(--bg-subtle) 70%);
  background-size: 220% 100%;
  animation: sh 1.3s infinite linear;
}
@keyframes sh {
  from { background-position: 180% 0; }
  to { background-position: -20% 0; }
}

.empty {
  padding: 70px 20px;
  text-align: center;
}
.empty-art {
  width: 76px;
  height: 76px;
  margin: 0 auto 16px;
  display: grid;
  place-items: center;
  border-radius: var(--r-xl);
  background: var(--brand-soft);
  color: var(--brand);
}
.empty h3 { margin: 0 0 6px; font-size: 16px; font-weight: 650; }
.empty p { margin: 0 0 18px; color: var(--text-weak); font-size: 13px; }

.loading-more {
  display: flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  padding: 22px 0;
  font-size: 13px;
  color: var(--text-weak);
}
.spin {
  width: 15px;
  height: 15px;
  border: 2px solid var(--border-strong);
  border-top-color: var(--brand);
  border-radius: 50%;
  animation: sp 0.7s linear infinite;
  display: inline-block;
}
.spin.sm { width: 13px; height: 13px; border-width: 2px; }
@keyframes sp { to { transform: rotate(360deg); } }

/* ---------------- 上传对话框 ---------------- */
.dlg-head { display: flex; align-items: center; gap: 8px; font-weight: 650; }
.up-row { display: flex; align-items: center; gap: 10px; margin-bottom: 12px; }
.up-label { font-size: 13px; color: var(--text-sub); flex-shrink: 0; }
.up-warn {
  display: flex;
  gap: 8px;
  padding: 11px 13px;
  border-radius: var(--r-md);
  background: rgba(247, 144, 9, 0.1);
  color: var(--warning);
  font-size: 13px;
  line-height: 1.6;
}
.drop {
  border: 1.5px dashed var(--border-strong);
  border-radius: var(--r-lg);
  padding: 30px 20px;
  text-align: center;
  cursor: pointer;
  transition: all 0.18s;
  background: var(--bg-subtle);
}
.drop:hover, .drop.over {
  border-color: var(--brand);
  background: var(--brand-soft);
}
.drop-ic {
  width: 46px;
  height: 46px;
  margin: 0 auto 10px;
  display: grid;
  place-items: center;
  border-radius: var(--r-full);
  background: var(--brand);
  color: #fff;
}
.drop-t { font-size: 14px; color: var(--text); }
.drop-t b { color: var(--brand); }
.drop-s { margin-top: 5px; font-size: 12px; color: var(--text-weak); }

.qlist {
  margin-top: 14px;
  max-height: 210px;
  overflow-y: auto;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
}
.qlist-h {
  display: flex;
  justify-content: space-between;
  padding: 8px 12px;
  font-size: 12.5px;
  color: var(--text-sub);
  background: var(--bg-subtle);
  border-bottom: 1px solid var(--border);
}

/* 提示：上传转后台，弹窗可以关 */
.qlist-tip {
  padding: 8px 12px;
  font-size: 11.5px;
  color: var(--text-weak);
  text-align: center;
  border-top: 1px solid var(--border);
}
.lnk {
  border: none;
  background: transparent;
  color: var(--brand);
  font-size: 12px;
  cursor: pointer;
  padding: 0;
  font-family: inherit;
}
.qi {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 7px 12px;
  font-size: 12.5px;
  border-bottom: 1px solid var(--border);
}
.qi:last-child { border-bottom: none; }
.qi-thumb {
  width: 30px;
  height: 30px;
  border-radius: 6px;
  overflow: hidden;
  flex-shrink: 0;
  display: grid;
  place-items: center;
  background: var(--bg-subtle);
  color: var(--text-weak);
}
.qi-thumb img { width: 100%; height: 100%; object-fit: cover; display: block; }
.qi-thumb.vid { color: var(--brand); }
.qi-n { flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.qi-s { color: var(--text-weak); flex-shrink: 0; }
.qi-bar {
  width: 90px;
  height: 5px;
  border-radius: 3px;
  background: var(--border);
  overflow: hidden;
  flex-shrink: 0;
}
.qi-bar i { display: block; height: 100%; background: var(--brand); transition: width 0.2s; }
.qi-bar i.ok { background: var(--success); }
.qi-bar i.err { background: var(--danger); }
.qi-st { width: 62px; text-align: right; color: var(--text-weak); flex-shrink: 0; }
.qi-st.ok { color: var(--success); }
.qi-st.err { color: var(--danger); }

/* ---------------- 灯箱 ---------------- */
.lightbox {
  position: fixed;
  inset: 0;
  z-index: 2000;
  display: flex;
  flex-direction: column;
  background: rgba(10, 12, 16, 0.94);
  backdrop-filter: blur(10px);
  outline: none;
  color: #f2f4f7;
}
.lb-enter-active, .lb-leave-active { transition: opacity 0.2s ease; }
.lb-enter-from, .lb-leave-to { opacity: 0; }

.lb-top {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 12px 18px;
  flex-shrink: 0;
  background: linear-gradient(to bottom, rgba(0, 0, 0, 0.5), transparent);
}
.lb-name {
  font-size: 14px;
  font-weight: 600;
  max-width: 40%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.lb-idx { font-size: 12px; color: rgba(255, 255, 255, 0.55); }
.lb-tools {
  display: flex;
  gap: 4px;
  padding: 4px;
  border-radius: var(--r-full);
  background: rgba(255, 255, 255, 0.1);
  backdrop-filter: blur(10px);
}
.lb-b {
  width: 34px;
  height: 34px;
  display: grid;
  place-items: center;
  border: none;
  border-radius: var(--r-full);
  background: transparent;
  color: rgba(255, 255, 255, 0.82);
  cursor: pointer;
  transition: all 0.15s;
}
.lb-b:hover { background: rgba(255, 255, 255, 0.16); color: #fff; }
.lb-b.act { background: #ffc44d; color: #4a3400; }
.lb-b.danger:hover { background: rgba(240, 68, 56, 0.85); color: #fff; }
.lb-b.wide { width: auto; padding: 0 12px; font-size: 12px; font-weight: 600; }

.lb-body { flex: 1; min-height: 0; position: relative; display: flex; }
.lb-stage {
  flex: 1;
  min-width: 0;
  display: grid;
  place-items: center;
  overflow: hidden;
  padding: 8px 70px 16px;
  position: relative;
}
/* 加载指示只是叠在媒体之上的覆盖层，不能参与布局，
   否则会顶掉 img/video 的位置（也避免再次出现"永远转圈"） */
.lb-spin {
  position: absolute;
  left: 50%;
  top: 50%;
  transform: translate(-50%, -50%);
  z-index: 2;
  pointer-events: none;
}
.lb-spin .spin {
  width: 26px;
  height: 26px;
  border-width: 3px;
  border-color: rgba(255, 255, 255, 0.25);
  border-top-color: #fff;
}
.lb-media {
  max-width: 100%;
  max-height: 100%;
  object-fit: contain;
  border-radius: var(--r-sm);
  box-shadow: 0 18px 50px rgba(0, 0, 0, 0.6);
  transition: transform 0.1s linear;
  user-select: none;
}
/* 视频：尺寸由 JS 精确计算（fitVideo，长边最大化），不依赖百分比高度——
   实测 height:100% 在 grid + place-items:center 里会被按 auto 处理，
   结果就是「宽度铺满 × 固有比例」，直接撑出屏幕、播控条看不见。
   .lb-media 的 max-width/max-height 继续生效，作为 JS 没跑到时的兜底。 */
.lb-media.lb-video {
  object-fit: contain;
  box-shadow: none;
  background: transparent;
}
.lb-media.lb-video.is-fs {
  /* 全屏：铺满整个屏幕。!important 用来压过 fitVideo 写的内联 px 尺寸 */
  position: fixed;
  inset: 0 !important;
  width: 100% !important;
  height: 100% !important;
  max-width: none;
  max-height: none;
  background: #000;
  border-radius: 0;
  box-shadow: none;
}
.lb-spin { display: grid; place-items: center; height: 100%; }
.lb-spin .spin { width: 26px; height: 26px; border-width: 3px; border-color: rgba(255,255,255,0.25); border-top-color: #fff; }

.lb-nav {
  position: absolute;
  top: 50%;
  transform: translateY(-50%);
  width: 46px;
  height: 46px;
  display: grid;
  place-items: center;
  border: none;
  border-radius: var(--r-full);
  background: rgba(255, 255, 255, 0.1);
  color: rgba(255, 255, 255, 0.8);
  cursor: pointer;
  backdrop-filter: blur(8px);
  transition: all 0.16s;
  z-index: 4;
}
.lb-nav:hover { background: rgba(255, 255, 255, 0.22); color: #fff; }
.lb-nav.prev { left: 14px; }
.lb-nav.next { right: 14px; }

.lb-info {
  width: 300px;
  flex-shrink: 0;
  padding: 18px 20px;
  overflow-y: auto;
  background: rgba(255, 255, 255, 0.06);
  border-left: 1px solid rgba(255, 255, 255, 0.1);
}
.panel-enter-active, .panel-leave-active { transition: all 0.22s ease; }
.panel-enter-from, .panel-leave-to { transform: translateX(20px); opacity: 0; }

.info-h {
  display: flex;
  align-items: center;
  gap: 7px;
  font-size: 13px;
  font-weight: 650;
  margin-bottom: 14px;
  padding-bottom: 10px;
  border-bottom: 1px solid rgba(255, 255, 255, 0.1);
}
.kv {
  display: flex;
  gap: 10px;
  font-size: 12.5px;
  padding: 6px 0;
  align-items: flex-start;
}
.kv > span { width: 62px; flex-shrink: 0; color: rgba(255, 255, 255, 0.5); }
.kv > b { flex: 1; min-width: 0; font-weight: 500; }
.kv .wrap { word-break: break-all; }
.kv .path { color: rgba(255, 255, 255, 0.6); font-size: 11.5px; }
.exif {
  flex: 1;
  display: grid;
  grid-template-columns: 1fr;
  gap: 3px;
  font-size: 11.5px;
}
.exif i {
  color: rgba(255, 255, 255, 0.45);
  font-style: normal;
  margin-right: 6px;
}

@media (max-width: 900px) {
  .lb-info { display: none; }
  .lb-stage { padding: 8px 16px 16px; }
  .toolbar { padding: 10px 14px; }
  .scroll { padding: 12px 14px 50px; }
}
</style>
