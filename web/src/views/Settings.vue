<template>
  <div class="settings">
    <div class="np-row">
      <div class="np-flex1">
        <h2 class="pg-t">设置</h2>
        <p class="pg-s">存储库、扫描缓存与管理员账号都在这里管理。</p>
      </div>
    </div>

    <div class="tabs">
      <button v-for="t in TABS" :key="t.v" class="tab-i" :class="{ on: tab === t.v }"
        @click="setTab(t.v)">
        <AppIcon :name="t.icon" :size="15" /> {{ t.t }}
      </button>
    </div>

    <!-- ================= 存储管理 ================= -->
    <div v-show="tab === 'storage'">
      <StorageManage />
    </div>

    <!-- ================= 账号设置 ================= -->
    <div v-show="tab === 'account'" class="pane">
      <el-card shadow="never" class="mb">
        <template #header>
          <div class="card-head">
            <span class="sec-ic"><AppIcon name="user" :size="16" /></span>
            <b>管理员账号</b>
            <span class="np-muted">{{ users.length }} 个</span>
            <div class="np-flex1" />
            <button class="btn ghost sm" @click="loadUsers">
              <AppIcon name="refresh" :size="14" /> 刷新
            </button>
            <button class="btn primary sm" @click="openCreate">
              <AppIcon name="plus" :size="14" /> 新增账号
            </button>
          </div>
        </template>

        <el-table :data="users" v-loading="uLoading" size="small" border>
          <el-table-column prop="username" label="用户名" min-width="120" />
          <el-table-column prop="nickname" label="昵称" min-width="120" />
          <el-table-column label="角色" width="100">
            <template #default="{ row }">
              <el-tag :type="row.role === 1 ? 'danger' : 'info'" size="small">
                {{ row.role === 1 ? '管理员' : '普通用户' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="90">
            <template #default="{ row }">
              <el-tag :type="row.status === 1 ? 'success' : 'info'" size="small">
                {{ row.status === 1 ? '正常' : '已禁用' }}
              </el-tag>
            </template>
          </el-table-column>
          <el-table-column label="最近登录" min-width="160">
            <template #default="{ row }">{{ fmtTime(row.last_login_at) }}</template>
          </el-table-column>
          <el-table-column label="操作" width="300" fixed="right">
            <template #default="{ row }">
              <el-button link type="primary" @click="openEdit(row)">编辑</el-button>
              <el-button link @click="openPwd(row)">重置密码</el-button>
              <el-button link @click="toggleStatus(row)">
                {{ row.status === 1 ? '禁用' : '启用' }}
              </el-button>
              <el-button link type="danger" :disabled="row.id === meId" @click="removeUser(row)">
                删除
              </el-button>
            </template>
          </el-table-column>
        </el-table>
      </el-card>

      <el-card shadow="never">
        <template #header>
          <div class="card-head">
            <span class="sec-ic"><AppIcon name="key" :size="16" /></span>
            <b>修改我的密码</b>
            <span class="np-muted" v-if="me.username">当前登录：{{ me.username }}</span>
          </div>
        </template>
        <div class="fr">
          <span class="lb">原密码</span>
          <input v-model="pwd.old" class="ipt" type="password" autocomplete="current-password" />
        </div>
        <div class="fr">
          <span class="lb">新密码</span>
          <input v-model="pwd.next" class="ipt" type="password" autocomplete="new-password"
            placeholder="至少 6 位" />
        </div>
        <div class="fr">
          <span class="lb">确认密码</span>
          <input v-model="pwd.next2" class="ipt" type="password" autocomplete="new-password" />
        </div>
        <div class="fr end">
          <button class="btn primary sm" :disabled="pwdBusy" @click="submitPwd">
            <span v-if="pwdBusy" class="spin sm" /> 修改密码
          </button>
        </div>
      </el-card>
    </div>

    <!-- ================= 手机同步设备 ================= -->
    <div v-show="tab === 'devices'" class="pane">
      <el-card shadow="never" class="mb">
        <template #header>
          <div class="card-head">
            <span class="sec-ic"><AppIcon name="phone" :size="16" /></span>
            <b>已连接设备</b>
            <span class="np-muted">{{ devices.length }} 台</span>
            <div class="np-flex1" />
            <button class="btn ghost sm" :disabled="dLoading || !devices.length" @click="checkAllLinks">
              <AppIcon name="sync" :size="14" /> 检查全部链接
            </button>
            <button class="btn ghost sm" @click="loadDevices">
              <AppIcon name="refresh" :size="14" /> 刷新
            </button>
          </div>
        </template>

        <el-table :data="devices" v-loading="dLoading" size="small" border>
          <el-table-column label="设备名" min-width="160">
            <template #default="{ row }">{{ row.device_name || ('设备 ' + row.id) }}</template>
          </el-table-column>
          <el-table-column label="平台" width="90">
            <template #default="{ row }">{{ row.platform === 2 ? 'iOS' : 'Android' }}</template>
          </el-table-column>
          <el-table-column label="版本" width="100">
            <template #default="{ row }">{{ row.app_version || '-' }}</template>
          </el-table-column>
          <el-table-column label="最近 IP" width="160">
            <template #default="{ row }">{{ row.last_ip || '-' }}</template>
          </el-table-column>
          <el-table-column label="最后在线" min-width="160">
            <template #default="{ row }">{{ fmtTime(row.last_seen_at) }}</template>
          </el-table-column>
          <el-table-column label="存储库 / 链接" min-width="230">
            <template #default="{ row }">
              <el-tag v-if="linkedLibOf(row)" type="success" size="small">
                已链接：{{ linkedLibOf(row).name }}
              </el-tag>
              <el-tag v-else type="warning" size="small">未创建存储库</el-tag>
              <el-button link type="primary" size="small" :loading="linkBusy[row.id]"
                @click="checkLink(row)">
                {{ linkedLibOf(row) ? '检查 / 修复' : '新建存储库' }}
              </el-button>
            </template>
          </el-table-column>
          <el-table-column label="状态" width="90">
            <template #default="{ row }">
              <el-tag :type="row.status === 1 ? 'success' : 'info'" size="small">
                {{ row.status === 1 ? '正常' : '已吊销' }}
              </el-tag>
            </template>
          </el-table-column>
        </el-table>
      </el-card>

      <el-card shadow="never">
        <template #header>
          <div class="card-head">
            <span class="sec-ic"><AppIcon name="sync" :size="16" /></span>
            <b>同步任务</b>
            <span class="np-muted">{{ syncTasks.length }} 个</span>
          </div>
        </template>

        <el-table :data="syncTasks" v-loading="tLoading" size="small" border>
          <el-table-column label="设备" min-width="140">
            <template #default="{ row }">
              {{ deviceNameOf(row.device_id) }}
            </template>
          </el-table-column>
          <el-table-column label="文件夹" min-width="160">
            <template #default="{ row }">{{ row.folder_path || row.folder_uri }}</template>
          </el-table-column>
          <el-table-column label="目标库" min-width="140">
            <template #default="{ row }">{{ libraryNameOf(row.target_library_id) }}</template>
          </el-table-column>
          <el-table-column label="状态" width="100">
            <template #default="{ row }">
              <el-tag :type="taskStatusType(row.status)" size="small">{{ taskStatusText(row.status) }}</el-tag>
            </template>
          </el-table-column>
          <el-table-column label="进度" min-width="200">
            <template #default="{ row }">
              <el-progress :percentage="taskPercent(row)" :stroke-width="6"
                :status="row.status === 3 ? 'exception' : ''" />
            </template>
          </el-table-column>
          <el-table-column label="总数" width="80">
            <template #default="{ row }">{{ row.total_count || 0 }}</template>
          </el-table-column>
          <el-table-column label="已同步" width="80">
            <template #default="{ row }">{{ row.synced_count || 0 }}</template>
          </el-table-column>
          <el-table-column label="失败" width="70">
            <template #default="{ row }">
              <span :style="{ color: (row.failed_count || 0) > 0 ? '#f56c6c' : '' }">
                {{ row.failed_count || 0 }}
              </span>
            </template>
          </el-table-column>
          <el-table-column label="跳过" width="70">
            <template #default="{ row }">{{ row.skipped_count || 0 }}</template>
          </el-table-column>
          <el-table-column label="最近同步" min-width="160">
            <template #default="{ row }">{{ fmtTime(row.last_sync_at) }}</template>
          </el-table-column>
        </el-table>
      </el-card>
    </div>

    <!-- ================= 关于 ================= -->
    <div v-show="tab === 'about'" class="pane">
      <el-card shadow="never">
        <template #header>
          <div class="card-head">
            <span class="sec-ic"><AppIcon name="info" :size="16" /></span>
            <b>关于 Naspic</b>
          </div>
        </template>
        <div class="kv"><span>版本</span><b>{{ appVersion }}</b></div>
        <div class="kv"><span>数据库</span><b>{{ dbDriver || 'sqlite' }}</b></div>
        <div class="kv"><span>服务地址</span><b>{{ origin }}</b></div>
        <p class="np-muted tip">
          照片索引、缩略图缓存、扫描指纹全部存在服务端；挂载目录只做只读索引，
          平台不会修改或删除宿主机原图。
        </p>
      </el-card>
    </div>

    <!-- ================= 新增 / 编辑账号 ================= -->
    <el-dialog v-model="dlg" width="460px" align-center append-to-body
      :close-on-click-modal="false">
      <template #header>
        <div class="dlg-head">
          <AppIcon name="user" :size="17" />
          <span>{{ editing ? '编辑账号' : '新增账号' }}</span>
        </div>
      </template>
      <div class="fr">
        <span class="lb">用户名</span>
        <input v-model="form.username" class="ipt" :disabled="!!editing"
          placeholder="至少 3 个字符" />
      </div>
      <div v-if="!editing" class="fr">
        <span class="lb">初始密码</span>
        <input v-model="form.password" class="ipt" type="password" placeholder="至少 6 位" />
      </div>
      <div class="fr">
        <span class="lb">昵称</span>
        <input v-model="form.nickname" class="ipt" placeholder="选填" />
      </div>
      <div class="fr">
        <span class="lb">角色</span>
        <el-select v-model="form.role" class="np-flex1">
          <el-option :value="1" label="管理员（可管理存储与账号）" />
          <el-option :value="2" label="普通用户（仅浏览）" />
        </el-select>
      </div>
      <template #footer>
        <button class="btn ghost" @click="dlg = false">取消</button>
        <button class="btn primary" :disabled="saving" @click="saveUser">
          <span v-if="saving" class="spin sm" /> 保存
        </button>
      </template>
    </el-dialog>

    <!-- ================= 重置密码 ================= -->
    <el-dialog v-model="pwdDlg" width="400px" align-center append-to-body
      :close-on-click-modal="false">
      <template #header>
        <div class="dlg-head">
          <AppIcon name="key" :size="17" />
          <span>重置「{{ pwdTarget.username }}」的密码</span>
        </div>
      </template>
      <div class="fr">
        <span class="lb">新密码</span>
        <input v-model="newPwd" class="ipt" type="password" placeholder="至少 6 位" />
      </div>
      <template #footer>
        <button class="btn ghost" @click="pwdDlg = false">取消</button>
        <button class="btn primary" :disabled="saving" @click="submitReset">确定</button>
      </template>
    </el-dialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref, computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import AppIcon from '../components/AppIcon.vue'
import StorageManage from './StorageManage.vue'
import {
  listAdminUsers, createAdminUser, updateAdminUser, deleteAdminUser,
  resetAdminPassword, changeMyPassword, myProfile,
  listDevices, listSyncTasks, listLibraries, ensureDeviceLibrary,
} from '../api'
import { APP_VERSION } from '../version'

const route = useRoute()
const router = useRouter()

const TABS = [
  { v: 'storage', t: '存储管理', icon: 'server' },
  { v: 'devices', t: '手机同步', icon: 'phone' },
  { v: 'account', t: '账号设置', icon: 'user' },
  { v: 'about', t: '关于', icon: 'info' },
]
const tab = ref(route.query.tab === 'account' || route.query.tab === 'about'
  ? route.query.tab : 'storage')
function setTab(v) {
  tab.value = v
  onTabChange(v)
  router.replace({ query: { ...route.query, tab: v } })
}

// ---------------- 账号 ----------------
const users = ref([])
const uLoading = ref(false)
const me = ref({})
const meId = ref(Number(localStorage.getItem('naspic.uid') || '0'))

async function loadUsers() {
  uLoading.value = true
  try {
    const d = await listAdminUsers()
    users.value = d.items || []
  } catch (e) {
    ElMessage.error(e.message || '加载账号失败')
  } finally {
    uLoading.value = false
  }
}

const dlg = ref(false)
const editing = ref(null)
const saving = ref(false)
const form = reactive({ username: '', password: '', nickname: '', role: 2 })

function openCreate() {
  editing.value = null
  Object.assign(form, { username: '', password: '', nickname: '', role: 2 })
  dlg.value = true
}
function openEdit(row) {
  editing.value = row
  Object.assign(form, {
    username: row.username, password: '', nickname: row.nickname || '', role: row.role,
  })
  dlg.value = true
}

async function saveUser() {
  if (!editing.value) {
    if ((form.username || '').trim().length < 3) return ElMessage.warning('用户名至少 3 个字符')
    if ((form.password || '').length < 6) return ElMessage.warning('密码至少 6 位')
  }
  saving.value = true
  try {
    if (editing.value) {
      await updateAdminUser(editing.value.id, {
        nickname: form.nickname, role: form.role,
      })
    } else {
      await createAdminUser({
        username: form.username.trim(), password: form.password,
        nickname: form.nickname, role: form.role,
      })
    }
    ElMessage.success('已保存')
    dlg.value = false
    loadUsers()
  } catch (e) {
    ElMessage.error(e.message || '保存失败')
  } finally {
    saving.value = false
  }
}

async function toggleStatus(row) {
  try {
    await updateAdminUser(row.id, { status: row.status === 1 ? 2 : 1 })
    ElMessage.success(row.status === 1 ? '已禁用' : '已启用')
    loadUsers()
  } catch (e) {
    ElMessage.error(e.message || '操作失败')
  }
}

async function removeUser(row) {
  try {
    await ElMessageBox.confirm(`删除账号「${row.username}」？`, '删除账号',
      { type: 'warning', confirmButtonText: '删除', cancelButtonText: '取消' })
  } catch (e) { return }
  try {
    await deleteAdminUser(row.id)
    ElMessage.success('已删除')
    loadUsers()
  } catch (e) {
    ElMessage.error(e.message || '删除失败')
  }
}

const pwdDlg = ref(false)
const pwdTarget = ref({})
const newPwd = ref('')
function openPwd(row) {
  pwdTarget.value = row
  newPwd.value = ''
  pwdDlg.value = true
}
async function submitReset() {
  if (newPwd.value.length < 6) return ElMessage.warning('密码至少 6 位')
  saving.value = true
  try {
    await resetAdminPassword(pwdTarget.value.id, newPwd.value)
    ElMessage.success('密码已重置')
    pwdDlg.value = false
  } catch (e) {
    ElMessage.error(e.message || '重置失败')
  } finally {
    saving.value = false
  }
}

// ---------------- 改自己的密码 ----------------
const pwd = reactive({ old: '', next: '', next2: '' })
const pwdBusy = ref(false)
async function submitPwd() {
  if (pwd.next.length < 6) return ElMessage.warning('新密码至少 6 位')
  if (pwd.next !== pwd.next2) return ElMessage.warning('两次输入的新密码不一致')
  pwdBusy.value = true
  try {
    await changeMyPassword(pwd.old, pwd.next)
    ElMessage.success('密码已修改，下次登录生效')
    pwd.old = ''
    pwd.next = ''
    pwd.next2 = ''
  } catch (e) {
    ElMessage.error(e.message || '修改失败')
  } finally {
    pwdBusy.value = false
  }
}

// ---------------- 关于 ----------------
const appVersion = ref(APP_VERSION || '-')
const dbDriver = ref(localStorage.getItem('naspic.db') || '')
const origin = ref(typeof location !== 'undefined' ? location.origin : '')

function fmtTime(t) {
  if (!t) return '—'
  const d = new Date(t)
  return isNaN(d.getTime()) ? t : d.toLocaleString('zh-CN')
}

// ---------------- 手机同步设备 ----------------
const devices = ref([])
const dLoading = ref(false)
const syncTasks = ref([])
const tLoading = ref(false)
const libMap = ref({})
const libByName = computed(() => {
  const m = {}
  for (const id in libMap.value) {
    const l = libMap.value[id]
    if (l && l.type === 1) m[l.name] = l
  }
  return m
})
const devMap = ref({})
const linkBusy = reactive({})
let syncTimer = null

async function loadDevices() {
  dLoading.value = true
  try {
    const d = await listDevices()
    devices.value = d || []
    devMap.value = Object.fromEntries(devices.value.map((x) => [x.id, x]))
  } catch (e) {
    ElMessage.error(e.message || '加载设备失败')
  } finally {
    dLoading.value = false
  }
}

async function loadSyncTasks() {
  tLoading.value = true
  try {
    const d = await listSyncTasks()
    syncTasks.value = d || []
  } catch (e) {
    ElMessage.error(e.message || '加载同步任务失败')
  } finally {
    tLoading.value = false
  }
}

async function loadLibMap() {
  try {
    const libs = await listLibraries()
    libMap.value = Object.fromEntries((libs || []).map((x) => [x.id, x]))
  } catch (e) { /* 忽略 */ }
}

function deviceNameOf(id) {
  return devMap.value[id]?.device_name || ('设备 ' + id)
}
function libraryNameOf(id) {
  return libMap.value[id]?.name || ('库 ' + id)
}
// 设备名对应的已存在托管库（web 端是否已为该手机建好存储库）
function linkedLibOf(row) {
  return (row && row.device_name && libByName.value[row.device_name]) || null
}
// 链接检查：web 端没有该手机的存储库就先新建一个；已有则确认可达
async function checkLink(row) {
  linkBusy[row.id] = true
  try {
    const d = await ensureDeviceLibrary(row.id)
    const name = d.name || (d.library && d.library.name) || ''
    ElMessage.success((d.created ? '已新建' : '已确认') + `存储库「${name}」`)
    loadLibMap()
    loadDevices()
  } catch (e) {
    ElMessage.error(e.message || '检查连接失败')
  } finally {
    linkBusy[row.id] = false
  }
}
// 一键检查全部设备的链接与存储库
async function checkAllLinks() {
  if (!devices.value.length) return
  for (const d of devices.value) {
    if (linkBusy[d.id]) continue
    await checkLink(d)
  }
}
function taskStatusText(s) {
  return { 0: '空闲', 1: '同步中', 2: '已完成', 3: '失败', 4: '已暂停' }[s] || '—'
}
function taskStatusType(s) {
  return { 0: 'info', 1: 'warning', 2: 'success', 3: 'danger', 4: '' }[s] || 'info'
}
function taskPercent(row) {
  const total = row.total_count || 0
  if (!total) return 0
  const done = (row.synced_count || 0) + (row.failed_count || 0) + (row.skipped_count || 0)
  return Math.min(100, Math.floor((done / total) * 100))
}

function startSyncPolling() {
  stopSyncPolling()
  loadDevices()
  loadSyncTasks()
  loadLibMap()
  syncTimer = setInterval(() => {
    if (tab.value === 'devices') {
      loadDevices()
      loadSyncTasks()
    }
  }, 5000)
}
function stopSyncPolling() {
  if (syncTimer) {
    clearInterval(syncTimer)
    syncTimer = null
  }
}

// 切换到设备 tab 时启动轮询
function onTabChange(v) {
  if (v === 'devices') startSyncPolling()
  else stopSyncPolling()
}

onMounted(async () => {
  loadUsers()
  if (tab.value === 'devices') startSyncPolling()
  try {
    const p = await myProfile()
    me.value = p
    meId.value = p.id
  } catch (e) { /* 未登录会被拦截器处理 */ }
})
</script>

<style scoped>
.np-row { display: flex; align-items: flex-start; gap: 12px; margin-bottom: 14px; }
.pg-t { margin: 0 0 3px; font-size: 19px; font-weight: 700; }
.pg-s { margin: 0; font-size: 12.5px; color: var(--text-weak); }

.tabs {
  display: flex;
  gap: 4px;
  padding: 4px;
  margin-bottom: 14px;
  background: var(--bg-subtle);
  border-radius: var(--r-md);
  width: fit-content;
}
.tab-i {
  display: flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 14px;
  border: none;
  background: transparent;
  border-radius: var(--r-sm);
  color: var(--text-weak);
  font-size: 13px;
  cursor: pointer;
}
.tab-i:hover { color: var(--text); }
.tab-i.on { background: var(--bg-surface); color: var(--brand); font-weight: 600; box-shadow: var(--shadow-xs); }

.mb { margin-bottom: 14px; }
.card-head { display: flex; align-items: center; gap: 8px; font-weight: 650; }
.sec-ic {
  width: 26px; height: 26px; border-radius: 8px;
  display: grid; place-items: center;
  background: var(--bg-subtle); color: var(--brand);
}
.fr { display: flex; align-items: center; gap: 9px; margin-bottom: 10px; }
.fr.end { justify-content: flex-end; margin-bottom: 0; }
.lb { width: 76px; flex-shrink: 0; font-size: 12.5px; color: var(--text-weak); }
.ipt {
  flex: 1; min-width: 0; height: 32px; padding: 0 10px;
  border: 1px solid var(--border); border-radius: var(--r-sm);
  background: var(--bg-surface); color: var(--text); font-size: 13px;
}
.kv { display: flex; gap: 10px; padding: 6px 0; font-size: 13px; border-bottom: 1px solid var(--border); }
.kv span { width: 90px; color: var(--text-weak); flex-shrink: 0; }
.tip { font-size: 12.5px; line-height: 1.7; margin: 12px 0 0; }
.dlg-head { display: flex; align-items: center; gap: 8px; font-weight: 650; }
.pane { animation: fade 0.18s ease; }
@keyframes fade { from { opacity: 0; transform: translateY(3px); } }
</style>
