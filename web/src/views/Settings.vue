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
import { onMounted, reactive, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ElMessage, ElMessageBox } from 'element-plus'
import AppIcon from '../components/AppIcon.vue'
import StorageManage from './StorageManage.vue'
import {
  listAdminUsers, createAdminUser, updateAdminUser, deleteAdminUser,
  resetAdminPassword, changeMyPassword, myProfile,
} from '../api'
import { APP_VERSION } from '../version'

const route = useRoute()
const router = useRouter()

const TABS = [
  { v: 'storage', t: '存储管理', icon: 'server' },
  { v: 'account', t: '账号设置', icon: 'user' },
  { v: 'about', t: '关于', icon: 'info' },
]
const tab = ref(route.query.tab === 'account' || route.query.tab === 'about'
  ? route.query.tab : 'storage')
function setTab(v) {
  tab.value = v
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

onMounted(async () => {
  loadUsers()
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
