<template>
  <div class="storage-page np-in">
    <!-- ================= 顶部操作 ================= -->
    <el-card shadow="never" class="mb">
      <template #header>
        <div class="card-head">
          <span class="sec-ic"><AppIcon name="server" :size="16" /></span>
          <b>存储库</b>
          <span class="np-muted">{{ libraries.length }} 个</span>
          <div class="np-flex1" />
          <button class="btn primary sm" @click="openManagedDialog">
            <AppIcon name="plus" :size="14" /> 新建托管库
          </button>
          <button class="btn ghost sm" @click="openMountDialog">
            <AppIcon name="folder" :size="14" /> 添加挂载目录
          </button>
          <button class="icon-btn" title="刷新" @click="refreshAll">
            <AppIcon name="refresh" :size="16" />
          </button>
        </div>
      </template>

      <!-- 驱动列表 -->
      <el-table :data="libraries" v-loading="loading" size="small" border>
        <el-table-column prop="name" label="名称" min-width="140" />
        <el-table-column label="类型" width="110">
          <template #default="{ row }">
            <el-tag :type="row.type === 1 ? 'success' : 'warning'" size="small">
              {{ row.type === 1 ? '托管存储' : '挂载目录' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="storage_root" label="路径" min-width="200" show-overflow-tooltip />
        <el-table-column label="读写" width="90">
          <template #default="{ row }">
            <el-tag :type="row.writable ? 'danger' : 'info'" size="small">
              {{ row.writable ? '可写' : '只读' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="media_count" label="媒体数" width="90" />
        <el-table-column label="监听" width="110">
          <template #default="{ row }">
            <el-tag v-if="row.driver_kind === 'mounted' && row.degraded" type="warning" size="small">
              轮询(已降级)
            </el-tag>
            <span v-else-if="row.driver_kind === 'mounted'">inotify</span>
            <span v-else>-</span>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="380" fixed="right">
          <template #default="{ row }">
            <el-button link type="primary" @click="scan(row, false)">增量扫描</el-button>
            <el-button link type="warning" @click="scan(row, true)">重建索引</el-button>
            <el-button link @click="openCache(row)">缓存</el-button>
            <el-button link @click="showJobs(row)">进度</el-button>
            <el-button link @click="showPerm(row)">权限</el-button>
            <el-button link type="danger" @click="removeLib(row)">删除</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-card>

    <!-- ================= 手机设备与同步任务 ================= -->
    <el-card shadow="never" class="mb">
      <template #header>
        <div class="card-head">
          <span class="sec-ic"><AppIcon name="devices" :size="16" /></span>
          <b>手机设备与文件夹同步</b>
          <div class="np-flex1" />
          <button class="icon-btn" title="刷新" @click="loadSync">
            <AppIcon name="refresh" :size="16" />
          </button>
        </div>
      </template>

      <el-empty v-if="devices.length === 0" description="暂无设备接入" />
      <el-collapse v-else>
        <el-collapse-item v-for="d in devices" :key="d.id"
          :name="String(d.id)">
          <template #title>
            <span class="dev-title">
              {{ d.device_name || d.device_uuid }}
              <el-tag size="small" type="info">{{ d.platform === 2 ? 'iOS' : 'Android' }}</el-tag>
              <el-tag size="small">v{{ d.app_version || '-' }}</el-tag>
              <span class="dim">最近活跃：{{ fmtTime(d.last_seen_at) }}</span>
            </span>
          </template>
          <el-table :data="taskMap[d.id] || []" size="small" border>
            <el-table-column prop="folder_path" label="手机文件夹" min-width="180" show-overflow-tooltip />
            <el-table-column label="类型" width="100">
              <template #default="{ row }">
                {{ row.folder_type === 1 ? '系统相册' : '自定义' }}
              </template>
            </el-table-column>
            <el-table-column label="启用" width="70">
              <template #default="{ row }">
                <el-switch :model-value="row.enabled === 1" disabled size="small" />
              </template>
            </el-table-column>
            <el-table-column label="策略" width="180">
              <template #default="{ row }">
                <el-tag size="small">{{ row.wifi_only === 1 ? '仅WiFi' : '允许流量' }}</el-tag>
                <el-tag size="small" type="info">{{ row.upload_original === 1 ? '原图' : '压缩' }}</el-tag>
                <el-tag size="small" type="info" v-if="row.include_subdir === 1">含子目录</el-tag>
              </template>
            </el-table-column>
            <el-table-column label="进度" min-width="200">
              <template #default="{ row }">
                <el-progress :percentage="progressOf(row)" :stroke-width="10" />
                <span class="dim">
                  成功 {{ row.synced_count }} / 跳过 {{ row.skipped_count }} / 失败 {{ row.failed_count }}
                </span>
              </template>
            </el-table-column>
            <el-table-column label="状态" width="100">
              <template #default="{ row }">
                {{ taskStatusText(row.status) }}
              </template>
            </el-table-column>
            <el-table-column label="操作" width="150">
              <template #default="{ row }">
                <el-button link @click="showRecords(row)">失败列表</el-button>
                <el-button link type="danger" @click="removeTask(row)">删除</el-button>
              </template>
            </el-table-column>
          </el-table>
        </el-collapse-item>
      </el-collapse>
    </el-card>

    <!-- ================= 添加挂载目录 ================= -->
    <el-dialog v-model="mountVisible" title="添加挂载本地目录" width="600">
      <el-form :model="mountForm" label-width="120px">
        <el-form-item label="库名称">
          <el-input v-model="mountForm.library_name" placeholder="留空则使用目录名" />
        </el-form-item>
        <el-form-item label="宿主机路径">
          <el-input v-model="mountForm.host_path" placeholder="/photos 或 /mnt/usb/travel" />
          <div class="tip">
            必须是<strong>绝对路径</strong>，且该路径要能通过 <code>docker -v</code> 挂进容器，
            内外路径保持一致并以只读方式挂载（<code>:ro</code>）。<br />
            若提示"目录在容器内不可见"，说明容器启动时没有挂载它，需加参数后重建容器。
          </div>
          <div v-if="mountedPaths.length" class="tip" style="margin-top: 4px">
            当前已挂载：
            <el-tag v-for="p in mountedPaths" :key="p" size="small" style="margin: 2px 4px 2px 0">{{ p }}</el-tag>
          </div>
        </el-form-item>
        <el-form-item label="访问模式">
          <el-radio-group v-model="mountForm.mode">
            <el-radio :value="1">只读（推荐）</el-radio>
            <el-radio :value="2">读写（高危）</el-radio>
          </el-radio-group>
          <div class="tip">只读模式下平台绝不修改、删除宿主机原图</div>
        </el-form-item>
        <el-form-item label="允许删除" v-if="mountForm.mode === 2">
          <el-switch v-model="mountForm.allow_delete" />
          <div class="tip danger">开启后可从平台删除宿主机源文件，请谨慎</div>
        </el-form-item>
        <el-form-item label="变更检测">
          <el-radio-group v-model="mountForm.watch_mode">
            <el-radio :value="1">inotify</el-radio>
            <el-radio :value="2">定时轮询</el-radio>
          </el-radio-group>
          <div class="tip">
            Docker 挂载目录的 inotify 常常收不到宿主机侧的文件事件，因此<strong>无论选哪种模式</strong>，
            都会按下方周期兜底跑一次增量扫描。周期越小，新照片出现越快，磁盘开销也越大。
          </div>
        </el-form-item>
        <el-form-item label="轮询周期(秒)">
          <el-input-number v-model="mountForm.poll_interval_sec" :min="60" :step="60" />
          <div class="tip">新照片最长在 1 个周期后自动入库，也可随时手动点「增量扫描」立即生效</div>
        </el-form-item>
        <el-form-item label="递归子目录">
          <el-switch v-model="mountForm.recursive" />
          <div class="tip">开启后自动索引所有层级的子文件夹（不限深度）</div>
        </el-form-item>
        <el-form-item label="忽略规则">
          <el-select v-model="mountForm.ignore_rules" multiple filterable allow-create
            default-first-option style="width: 100%" placeholder="输入 glob 后回车，如 **/@eaDir/**">
          </el-select>
        </el-form-item>
        <el-form-item label="仅包含扩展名">
          <el-select v-model="mountForm.include_exts" multiple filterable allow-create
            default-first-option style="width: 100%" placeholder="留空=全部支持格式">
          </el-select>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="mountVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitMount">确定</el-button>
      </template>
    </el-dialog>

    <!-- ================= 新建托管库 ================= -->
    <el-dialog v-model="managedVisible" title="新建托管存储库" width="480">
      <el-form :model="managedForm" label-width="100px">
        <el-form-item label="库名称"><el-input v-model="managedForm.name" /></el-form-item>
        <el-form-item label="存储路径">
          <el-input v-model="managedForm.storage_root" placeholder="留空自动使用 data_dir/managed/<名称>" />
          <div class="tip" style="margin-top:6px">
            建议留空：自动落在数据卷内，容器重建也不丢。
            自定义路径必须是<strong>已挂载进容器且已存在的目录</strong>，否则照片会写进容器临时层，重启就没了。
          </div>
        </el-form-item>
      </el-form>
      <template #footer>
        <el-button @click="managedVisible = false">取消</el-button>
        <el-button type="primary" :loading="submitting" @click="submitManaged">确定</el-button>
      </template>
    </el-dialog>

    <!-- ================= 扫描进度 & 日志 ================= -->
    <el-drawer v-model="jobsVisible" :title="`扫描任务 · ${currentLib?.name || ''}`" size="45%">
      <el-table :data="jobs" size="small" border>
        <el-table-column prop="id" label="#" width="60" />
        <el-table-column label="类型" width="90">
          <template #default="{ row }">{{ row.type === 2 ? '全量重建' : '增量' }}</template>
        </el-table-column>
        <el-table-column label="状态" width="90">
          <template #default="{ row }">
            <el-tag :type="jobStatusType(row.status)" size="small">{{ jobStatusText(row.status) }}</el-tag>
          </template>
        </el-table-column>
        <el-table-column label="进度" min-width="160">
          <template #default="{ row }">
            <el-progress :percentage="jobPercent(row)" :stroke-width="10" />
            <span class="dim">已扫 {{ row.scanned }} · 新增 {{ row.added }} ·
              跳过(缓存) {{ row.skipped || 0 }} · 缺失 {{ row.missing }} · 失败 {{ row.failed }}</span>
          </template>
        </el-table-column>
        <el-table-column label="耗时" width="110">
          <template #default="{ row }">{{ costText(row) }}</template>
        </el-table-column>
        <el-table-column label="操作" width="80">
          <template #default="{ row }">
            <el-button link @click="showLogs(row)">日志</el-button>
          </template>
        </el-table-column>
      </el-table>
    </el-drawer>

    <!-- ================= 扫描缓存 ================= -->
    <el-drawer v-model="cacheVisible" :title="`扫描缓存 · ${cacheLib?.name || ''}`" size="42%">
      <el-alert type="info" :closable="false" show-icon class="mb"
        description="两类缓存都是为了加速，随时可清：清掉不会丢失任何照片索引，只是下一次扫描/浏览会慢一些。" />

      <div class="cache-grid mb">
        <div class="cache-card">
          <span class="cc-ic"><AppIcon name="image" :size="16" /></span>
          <div class="cc-body">
            <b>缩略图缓存</b>
            <span class="dim">{{ cacheInfo.thumb?.files || 0 }} 个文件 · {{ fmtSize(cacheInfo.thumb?.bytes || 0) }}</span>
          </div>
          <el-button size="small" :loading="cacheBusy" @click="clearCache('thumbs')">清理</el-button>
        </div>
        <div class="cache-card">
          <span class="cc-ic"><AppIcon name="db" :size="16" /></span>
          <div class="cc-body">
            <b>扫描指纹缓存</b>
            <span class="dim">{{ cacheInfo.scan_cache?.rows || 0 }} 条记录</span>
          </div>
          <el-button size="small" :loading="cacheBusy" @click="clearCache('scan')">清理</el-button>
        </div>
      </div>

      <div class="tip mb">
        <strong>扫描指纹缓存</strong>记录每个文件的大小与修改时间。文件没变就跳过哈希计算，
        几万张照片的重复扫描可从分钟级降到秒级。添加/修改照片后，只有变动的文件会被重新处理。
      </div>

      <el-descriptions :column="1" size="small" border class="mb">
        <el-descriptions-item label="最近一次扫描">
          {{ fmtTime(cacheInfo.last_scan?.started_at) || '暂无记录' }}
        </el-descriptions-item>
        <el-descriptions-item label="上次扫描结果">
          <template v-if="cacheInfo.last_scan?.id">
            扫描 {{ cacheInfo.last_scan.scanned }} ·
            新增 {{ cacheInfo.last_scan.added }} ·
            缓存跳过 {{ cacheInfo.last_scan.skipped || 0 }} ·
            缺失 {{ cacheInfo.last_scan.missing }} ·
            失败 {{ cacheInfo.last_scan.failed }}
          </template>
          <template v-else>-</template>
        </el-descriptions-item>
      </el-descriptions>

      <div class="cache-actions">
        <el-button type="primary" :loading="cacheBusy" @click="rescan(false)">增量扫描</el-button>
        <el-button :loading="cacheBusy" @click="rescan(false, true)">忽略缓存重扫</el-button>
        <el-button type="warning" :loading="cacheBusy" @click="rescan(true, true)">全量重建（忽略缓存）</el-button>
        <el-button type="danger" plain :loading="cacheBusy" @click="clearCache('all')">清空全部缓存</el-button>
      </div>

      <!-- 视频拍摄时间补采：历史视频入库时用的是文件 mtime（复制/导出会乱），
           跑一次 ffprobe 把容器里的 creation_time 读回来，时间轴排序才准 -->
      <div class="cache-actions taken-fix">
        <div class="np-flex1">
          <b>补采视频拍摄时间</b>
          <div class="tip">
            早期入库的视频用的是文件修改时间，复制、导出后时间会乱，时间轴排序就不对。
            这里对每个视频跑一次 ffprobe，把容器里真正的拍摄时间读回来（不重算哈希、不重建缩略图）。
          </div>
          <div v-if="taken.running" class="taken-prog">
            已处理 {{ taken.done }}/{{ taken.total }} · 更新 {{ taken.updated }} 条
          </div>
        </div>
        <el-button :loading="taken.busy" :disabled="taken.running" @click="startRefreshTaken">
          {{ taken.running ? '补采中…' : '开始补采' }}
        </el-button>
      </div>
    </el-drawer>

    <el-dialog v-model="logsVisible" title="扫描日志" width="700">
      <el-table :data="logs" size="small" height="360" border>
        <el-table-column label="级别" width="80">
          <template #default="{ row }">
            <el-tag :type="row.level === 3 ? 'danger' : row.level === 2 ? 'warning' : 'info'" size="small">
              {{ row.level === 3 ? '错误' : row.level === 2 ? '警告' : '信息' }}
            </el-tag>
          </template>
        </el-table-column>
        <el-table-column prop="path" label="路径" min-width="180" show-overflow-tooltip />
        <el-table-column prop="message" label="内容" min-width="240" show-overflow-tooltip />
      </el-table>
    </el-dialog>

    <!-- ================= 权限配置 ================= -->
    <el-dialog v-model="permVisible" :title="`权限 · ${currentLib?.name || ''}`" width="640">
      <el-alert type="warning" :closable="false" show-icon class="mb"
        description="挂载目录可同时授权给多个用户或多个群组；只读授权的用户无法上传与删除。" />
      <el-table :data="permissions" size="small" border>
        <el-table-column label="主体" min-width="160">
          <template #default="{ row }">
            {{ row.subject_type === 1 ? '用户' : '群组' }} · {{ subjectName(row) }}
          </template>
        </el-table-column>
        <el-table-column label="权限" width="160">
          <template #default="{ row }">
            <el-select :model-value="row.permission" size="small"
              @change="(v) => changePerm(row, v)">
              <el-option :value="1" label="只读浏览" />
              <el-option :value="2" label="可上传" />
              <el-option :value="3" label="管理" />
            </el-select>
          </template>
        </el-table-column>
        <el-table-column label="操作" width="80">
          <template #default="{ row }">
            <el-button link type="danger" @click="removePerm(row)">移除</el-button>
          </template>
        </el-table-column>
      </el-table>

      <el-divider content-position="left">新增授权</el-divider>
      <el-form :inline="true">
        <el-form-item label="主体类型">
          <el-select v-model="permForm.subject_type" style="width: 110px">
            <el-option :value="1" label="用户" />
            <el-option :value="2" label="群组" />
          </el-select>
        </el-form-item>
        <el-form-item label="主体">
          <el-select v-model="permForm.subject_id" filterable style="width: 180px">
            <el-option v-for="o in subjectOptions" :key="o.id" :value="o.id" :label="o.name" />
          </el-select>
        </el-form-item>
        <el-form-item label="权限">
          <el-select v-model="permForm.permission" style="width: 130px">
            <el-option :value="1" label="只读浏览" />
            <el-option :value="2" label="可上传" />
            <el-option :value="3" label="管理" />
          </el-select>
        </el-form-item>
        <el-form-item>
          <el-button type="primary" @click="addPerm">添加</el-button>
        </el-form-item>
      </el-form>
    </el-dialog>

    <!-- ================= 失败列表 ================= -->
    <el-dialog v-model="recordsVisible" title="同步失败列表" width="760">
      <el-table :data="records" size="small" height="400" border>
        <el-table-column prop="local_path" label="手机文件" min-width="240" show-overflow-tooltip />
        <el-table-column prop="retry_count" label="重试" width="70" />
        <el-table-column prop="last_error" label="错误" min-width="220" show-overflow-tooltip />
      </el-table>
    </el-dialog>
  </div>
</template>

<script setup>
import { ref, reactive, computed, onMounted, onBeforeUnmount } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import AppIcon from '../components/AppIcon.vue'
import {
  listLibraries, createLibrary, createMountDir, deleteLibrary,
  triggerScan, listScanJobs, scanJobLogs,
  libraryCache, clearLibraryCache,
  listDevices, listSyncTasks, deleteSyncTask, listSyncRecords,
  listPermissions, upsertPermission, deletePermission, listUsers, listGroups,
  refreshTaken, refreshTakenStatus,
} from '../api'

// ---------- 存储库 ----------
const libraries = ref([])
const loading = ref(false)
const refreshing = ref(false)

async function loadLibraries() {
  loading.value = true
  try {
    libraries.value = await listLibraries()
  } finally {
    loading.value = false
  }
}

// 已挂载的路径，新增时给用户做参考（避免反复踩"容器内不可见"的坑）
const mountedPaths = computed(() =>
  libraries.value.filter((l) => l.driver_kind === 'mounted').map((l) => l.storage_root)
)

async function refreshAll() {
  refreshing.value = true
  try {
    await Promise.all([loadLibraries(), loadSync()])
  } finally {
    refreshing.value = false
  }
}

// ---------- 挂载目录 ----------
const mountVisible = ref(false)
const submitting = ref(false)
const mountForm = reactive({
  library_name: '', host_path: '', mode: 1, allow_delete: false,
  watch_mode: 2, poll_interval_sec: 300, recursive: true,
  ignore_rules: [], include_exts: [],
})

function openMountDialog() {
  Object.assign(mountForm, {
    library_name: '', host_path: '', mode: 1, allow_delete: false,
    watch_mode: 2, poll_interval_sec: 300, recursive: true,
    ignore_rules: ['**/@eaDir/**', '**/.thumbnails/**'], include_exts: [],
  })
  mountVisible.value = true
}

async function submitMount() {
  if (!mountForm.host_path) return ElMessage.warning('请填写宿主机路径')
  submitting.value = true
  try {
    const r = await createMountDir({ ...mountForm })
    ElMessage.success('已添加，正在后台建立索引')
    mountVisible.value = false
    await loadLibraries()
    // 顺手打开扫描任务，让用户能直接看到索引进度，而不是干等
    if (r && r.library) setTimeout(() => showJobs(r.library), 500)
  } catch (e) {
    ElMessage.error(e.message || '添加失败')
  } finally {
    submitting.value = false
  }
}

// ---------- 托管库 ----------
const managedVisible = ref(false)
const managedForm = reactive({ name: '', storage_root: '' })
function openManagedDialog() {
  managedForm.name = ''
  managedForm.storage_root = ''
  managedVisible.value = true
}
async function submitManaged() {
  if (!managedForm.name) return ElMessage.warning('请填写库名称')
  submitting.value = true
  try {
    await createLibrary({ name: managedForm.name, type: 1, storage_root: managedForm.storage_root })
    ElMessage.success('创建成功')
    managedVisible.value = false
    await loadLibraries()
  } catch (e) {
    ElMessage.error(e.message || '创建失败')
  } finally {
    submitting.value = false
  }
}

async function removeLib(row) {
  await ElMessageBox.confirm(
    row.type === 2
      ? '只会删除平台索引，宿主机原图不会有任何变动。确认删除？'
      : '将删除该库索引记录。确认删除？',
    '提示', { type: 'warning' }
  )
  await deleteLibrary(row.id)
  ElMessage.success('已删除')
  loadLibraries()
}

async function scan(row, full) {
  try {
    await triggerScan(row.id, full)
    ElMessage.success(full ? '已提交全量重建索引任务' : '已提交增量扫描任务')
    setTimeout(() => showJobs(row), 300)
  } catch (e) {
    ElMessage.error(e.message || '提交失败')
  }
}

// ---------- 扫描缓存 ----------
const cacheVisible = ref(false)
const cacheBusy = ref(false)
const cacheLib = ref(null)
const cacheInfo = ref({})

async function openCache(row) {
  cacheLib.value = row
  cacheInfo.value = {}
  cacheVisible.value = true
  await loadCache()
}

async function loadCache() {
  if (!cacheLib.value) return
  try {
    cacheInfo.value = await libraryCache(cacheLib.value.id)
  } catch (e) {
    ElMessage.error(e.message || '读取缓存信息失败')
  }
}

async function clearCache(scope) {
  const tip = {
    thumbs: '清理缩略图缓存后，浏览时会按需重新生成（第一次打开会慢一点）。',
    scan: '清理扫描指纹后，下一次扫描会重新计算全部文件的哈希（更慢但结果最准）。',
    all: '将同时清理缩略图与扫描指纹缓存。',
  }[scope] || ''
  try {
    await ElMessageBox.confirm(tip + '照片和索引数据不受影响。继续？', '清理缓存',
      { type: 'warning', confirmButtonText: '清理', cancelButtonText: '取消' })
  } catch {
    return
  }
  cacheBusy.value = true
  try {
    const r = await clearLibraryCache(cacheLib.value.id, scope)
    const d = r?.data || r || {}
    ElMessage.success(
      `已清理：${d.removed_files || 0} 个缩略图（${fmtSize(d.removed_bytes || 0)}）· ${d.removed_rows || 0} 条指纹`
    )
    await loadCache()
  } catch (e) {
    ElMessage.error(e.message || '清理失败')
  } finally {
    cacheBusy.value = false
  }
}

// ---------- 补采视频拍摄时间 ----------
const taken = reactive({ busy: false, running: false, total: 0, done: 0, updated: 0 })
let takenTimer = null

async function startRefreshTaken() {
  taken.busy = true
  try {
    await refreshTaken(cacheLib.value ? cacheLib.value.id : 0)
    taken.running = true
    ElMessage.success('已在后台开始补采，可稍后回来看进度')
    pollTaken()
  } catch (e) {
    ElMessage.error(e.message || '启动失败（服务端可能未安装 ffprobe）')
  } finally {
    taken.busy = false
  }
}

function pollTaken() {
  if (takenTimer) clearInterval(takenTimer)
  takenTimer = setInterval(async () => {
    try {
      const s = await refreshTakenStatus()
      taken.running = !!s.running
      taken.total = s.total || 0
      taken.done = s.done || 0
      taken.updated = s.updated || 0
      if (!s.running) {
        clearInterval(takenTimer)
        takenTimer = null
        if (s.error) ElMessage.error('补采失败：' + s.error)
        else ElMessage.success(`补采完成：更新 ${s.updated || 0} 条`)
      }
    } catch (e) {
      clearInterval(takenTimer)
      takenTimer = null
      taken.running = false
    }
  }, 2000)
}

async function rescan(full, force) {
  cacheBusy.value = true
  try {
    await triggerScan(cacheLib.value.id, full, force)
    ElMessage.success(
      full ? '已提交全量重建（忽略缓存）' : (force ? '已提交增量扫描（忽略缓存）' : '已提交增量扫描')
    )
    setTimeout(() => showJobs(cacheLib.value), 300)
  } catch (e) {
    ElMessage.error(e.message || '提交失败')
  } finally {
    cacheBusy.value = false
  }
}

function fmtSize(b) {
  if (!b) return '0 B'
  const u = ['B', 'KB', 'MB', 'GB', 'TB']
  const i = Math.min(u.length - 1, Math.floor(Math.log(b) / Math.log(1024)))
  return (b / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1) + ' ' + u[i]
}

// ---------- 扫描任务与日志 ----------
const jobsVisible = ref(false)
const logsVisible = ref(false)
const jobs = ref([])
const logs = ref([])
const currentLib = ref(null)

async function showJobs(row) {
  currentLib.value = row
  jobs.value = await listScanJobs(row.id)
  jobsVisible.value = true
}
async function showLogs(row) {
  logs.value = await scanJobLogs(row.id)
  logsVisible.value = true
}
function jobPercent(row) {
  const total = row.scanned + row.failed || 0
  if (row.status === 3) return 100
  if (!total) return 0
  return Math.min(99, Math.floor((row.scanned / Math.max(total, row.scanned + 1)) * 100))
}
function jobStatusText(s) {
  return { 1: '排队', 2: '运行中', 3: '完成', 4: '失败', 5: '已取消' }[s] || '-'
}
function jobStatusType(s) {
  return { 1: 'info', 2: 'primary', 3: 'success', 4: 'danger', 5: 'info' }[s] || 'info'
}
function costText(row) {
  if (!row.started_at || !row.finished_at) return '-'
  const ms = new Date(row.finished_at) - new Date(row.started_at)
  return ms > 60000 ? (ms / 60000).toFixed(1) + ' 分钟' : Math.floor(ms / 1000) + ' 秒'
}

// ---------- 手机同步 ----------
const devices = ref([])
const taskMap = ref({})
const syncLoading = ref(false)

async function loadSync() {
  syncLoading.value = true
  try {
    devices.value = await listDevices()
    const map = {}
    for (const d of devices.value) {
      map[d.id] = await listSyncTasks(d.id)
    }
    taskMap.value = map
  } finally {
    syncLoading.value = false
  }
}

function progressOf(row) {
  const total = (row.synced_count || 0) + (row.failed_count || 0) + (row.skipped_count || 0)
  if (!row.total_count) return 0
  return Math.min(100, Math.floor((total / row.total_count) * 100))
}
function taskStatusText(s) {
  return { 1: '空闲', 2: '扫描中', 3: '同步中', 4: '已暂停', 5: '异常' }[s] || '-'
}

const recordsVisible = ref(false)
const records = ref([])
async function showRecords(row) {
  records.value = await listSyncRecords(row.id, 4)
  recordsVisible.value = true
}
async function removeTask(row) {
  await ElMessageBox.confirm('删除该同步任务？手机本地文件不受影响。', '提示', { type: 'warning' })
  await deleteSyncTask(row.id)
  ElMessage.success('已删除')
  loadSync()
}

// ---------- 权限 ----------
const permVisible = ref(false)
const permissions = ref([])
const users = ref([])
const groups = ref([])
const permForm = reactive({ subject_type: 1, subject_id: null, permission: 1 })

const subjectOptions = computed(() =>
  permForm.subject_type === 1
    ? users.value.map((u) => ({ id: u.id, name: u.nickname || u.username }))
    : groups.value.map((g) => ({ id: g.id, name: g.name }))
)

async function showPerm(row) {
  currentLib.value = row
  permissions.value = await listPermissions(1, row.id)
  users.value = await listUsers()
  groups.value = await listGroups()
  permForm.subject_id = null
  permVisible.value = true
}
function subjectName(row) {
  if (row.subject_type === 1) {
    const u = users.value.find((x) => x.id === row.subject_id)
    return u ? u.nickname || u.username : row.subject_id
  }
  const g = groups.value.find((x) => x.id === row.subject_id)
  return g ? g.name : row.subject_id
}
async function addPerm() {
  if (!permForm.subject_id) return ElMessage.warning('请选择主体')
  await upsertPermission({
    subject_type: permForm.subject_type,
    subject_id: permForm.subject_id,
    resource_type: 1,
    resource_id: currentLib.value.id,
    permission: permForm.permission,
  })
  permissions.value = await listPermissions(1, currentLib.value.id)
  ElMessage.success('已授权')
}
async function changePerm(row, v) {
  await upsertPermission({ ...row, permission: v })
  ElMessage.success('已更新')
}
async function removePerm(row) {
  await deletePermission(row.id)
  permissions.value = await listPermissions(1, currentLib.value.id)
}

// ---------- 工具 ----------
function fmtTime(t) {
  if (!t) return '从未'
  const d = new Date(t)
  return isNaN(d.getTime()) ? t : d.toLocaleString('zh-CN')
}

onMounted(refreshAll)
</script>


onBeforeUnmount(() => {
  if (takenTimer) clearInterval(takenTimer)
})

<style scoped>
.storage-page {
  padding: 16px 22px 40px;
  overflow-y: auto;
  height: 100%;
  box-sizing: border-box;
}
.mb { margin-bottom: 16px; }
.card-head {
  display: flex;
  align-items: center;
  gap: 9px;
}
.card-head b {
  font-size: 14.5px;
  font-weight: 650;
}
.sec-ic {
  width: 28px;
  height: 28px;
  display: grid;
  place-items: center;
  border-radius: var(--r-sm);
  background: var(--brand-soft);
  color: var(--brand);
}
.tip {
  color: var(--text-weak);
  font-size: 12px;
  line-height: 1.6;
}
.danger { color: var(--danger); }
.dim { color: var(--text-weak); font-size: 12px; }
.dev-title {
  display: inline-flex;
  align-items: center;
  gap: 8px;
  font-weight: 500;
}

/* 缓存卡片 */
.cache-grid {
  display: grid;
  grid-template-columns: 1fr 1fr;
  gap: 12px;
}
@media (max-width: 900px) {
  .cache-grid { grid-template-columns: 1fr; }
}
.cache-card {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 12px;
  border: 1px solid var(--border);
  border-radius: var(--r-md);
  background: var(--bg-subtle);
}
.cc-ic {
  width: 32px;
  height: 32px;
  flex-shrink: 0;
  display: grid;
  place-items: center;
  border-radius: var(--r-sm);
  background: var(--brand-soft);
  color: var(--brand);
}
.cc-body {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  gap: 3px;
  font-size: 13px;
}
.cache-actions {
  display: flex;
  flex-wrap: wrap;
  gap: 8px;
  padding-top: 4px;
}
/* 补采拍摄时间：一行说明 + 按钮 */
.taken-fix {
  align-items: flex-start;
  margin-top: 16px;
  padding-top: 14px;
  border-top: 1px solid var(--border);
}
.taken-fix > div {
  min-width: 240px;
}
.taken-prog {
  margin-top: 6px;
  font-size: 12px;
  font-weight: 600;
  color: var(--brand);
}

/* 复用主按钮样式（与 Gallery 保持一致） */
.btn {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  height: 30px;
  padding: 0 12px;
  border-radius: var(--r-sm);
  border: 1px solid var(--border);
  background: var(--bg-surface);
  color: var(--text);
  font-size: 12.5px;
  font-weight: 500;
  font-family: inherit;
  cursor: pointer;
  transition: all 0.16s;
  white-space: nowrap;
}
.btn:hover { background: var(--bg-hover); border-color: var(--border-strong); }
.btn.primary {
  background: var(--brand);
  border-color: var(--brand);
  color: #fff;
  font-weight: 600;
}
.btn.primary:hover { background: var(--brand-hover); border-color: var(--brand-hover); }
.btn.ghost:hover { color: var(--brand); border-color: var(--brand); }
.icon-btn {
  width: 30px;
  height: 30px;
  display: grid;
  place-items: center;
  border: none;
  border-radius: var(--r-sm);
  background: transparent;
  color: var(--text-sub);
  cursor: pointer;
}
.icon-btn:hover { background: var(--bg-hover); color: var(--text); }

</style>
