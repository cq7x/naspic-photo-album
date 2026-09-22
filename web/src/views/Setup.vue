<template>
  <div class="lg">
    <!-- 左侧品牌区(同 Login 风格) -->
    <section class="lg-brand">
      <div class="brand-bg" />
      <div class="brand-in">
        <div class="lg-logo">
          <span class="lg-mark">
            <svg viewBox="0 0 24 24" width="20" height="20" fill="none" stroke="currentColor"
              stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
              <rect x="3" y="3" width="8" height="8" rx="2" />
              <rect x="13" y="3" width="8" height="8" rx="2" />
              <rect x="3" y="13" width="8" height="8" rx="2" />
              <path d="M17 13v8M13 17h8" />
            </svg>
          </span>
          <span class="lg-word">{{ APP_NAME }}</span>
        </div>

        <h2 class="lg-slogan">
          首次部署<br />连接你的数据库
        </h2>
        <p class="lg-desc">
          填写 MySQL 连接信息,测试通过后保存即自动建表并创建管理员账号
        </p>
      </div>
      <div class="brand-foot">v{{ APP_VERSION }} · Setup Wizard</div>
    </section>

    <!-- 右侧表单区 -->
    <section class="lg-form">
      <div class="lg-card np-in">
        <h1>数据库安装</h1>
        <p class="lg-sub">连接 compose 自带的 MySQL 容器或外部数据库</p>

        <form @submit.prevent="onSave">
          <label class="fld">
            <span>主机</span>
            <div class="ipt" :class="{ focus: focusHost }">
              <AppIcon name="user" :size="16" />
              <input v-model="form.host" placeholder="mysql 或 127.0.0.1"
                @focus="focusHost = true" @blur="focusHost = false" />
            </div>
          </label>

          <div class="row">
            <label class="fld">
              <span>端口</span>
              <div class="ipt" :class="{ focus: focusPort }">
                <AppIcon name="layers" :size="16" />
                <input v-model.number="form.port" type="number" placeholder="3306"
                  @focus="focusPort = true" @blur="focusPort = false" />
              </div>
            </label>
            <label class="fld">
              <span>库名</span>
              <div class="ipt" :class="{ focus: focusDB }">
                <AppIcon name="layers" :size="16" />
                <input v-model="form.db" placeholder="naspic"
                  @focus="focusDB = true" @blur="focusDB = false" />
              </div>
            </label>
          </div>

          <label class="fld">
            <span>账号</span>
            <div class="ipt" :class="{ focus: focusUser }">
              <AppIcon name="user" :size="16" />
              <input v-model="form.user" placeholder="naspic"
                @focus="focusUser = true" @blur="focusUser = false" />
            </div>
          </label>

          <label class="fld">
            <span>密码</span>
            <div class="ipt" :class="{ focus: focusPw }">
              <AppIcon name="star" :size="16" />
              <input v-model="form.password" :type="showPw ? 'text' : 'password'"
                placeholder="数据库密码"
                @focus="focusPw = true" @blur="focusPw = false" />
              <button type="button" class="eye" @click="showPw = !showPw">
                <AppIcon :name="showPw ? 'eyeOff' : 'eye'" :size="15" />
              </button>
            </div>
          </label>

          <div v-if="testResult" class="ok-msg">
            <AppIcon name="sparkle" :size="14" />
            连接成功 · {{ testResult.latency_ms }}ms · MySQL {{ testResult.version }}
          </div>
          <div v-if="errMsg" class="err">
            <AppIcon name="info" :size="14" /> {{ errMsg }}
          </div>

          <div class="btns">
            <button type="button" class="ghost" :disabled="testing || saving" @click="onTest">
              <span v-if="testing" class="spin" />
              {{ testing ? '测试中…' : '测试连接' }}
            </button>
            <button class="submit" type="submit" :disabled="!testedOk || saving">
              <span v-if="saving" class="spin" />
              {{ saving ? '保存中…' : '保存并初始化' }}
            </button>
          </div>
        </form>

        <div class="hint">
          <AppIcon name="info" :size="14" />
          <span>使用 compose 自带 MySQL 时,默认值已填好(mysql/3306/naspic/naspic),密码见 deploy/.env</span>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup>
import { reactive, ref, onMounted } from 'vue'
import { useRouter } from 'vue-router'
import AppIcon from '../components/AppIcon.vue'
import { APP_NAME, APP_VERSION } from '../version'
import { setupStatus, setupTest, setupSave } from '../api'

const router = useRouter()
const testing = ref(false)
const saving = ref(false)
const showPw = ref(false)
const testedOk = ref(false)
const testResult = ref(null)
const errMsg = ref('')

const focusHost = ref(false)
const focusPort = ref(false)
const focusDB = ref(false)
const focusUser = ref(false)
const focusPw = ref(false)

const form = reactive({
  host: 'mysql',
  port: 3306,
  db: 'naspic',
  user: 'naspic',
  password: '',
})

onMounted(async () => {
  try {
    const data = await setupStatus()
    if (!data || !data.need_setup) {
      router.replace('/login')
      return
    }
    if (data.defaults) {
      Object.assign(form, data.defaults)
    }
    // 降级模式:显示上一次连接失败原因,让用户知道为什么进了安装页
    if (data.boot_error) {
      errMsg.value = '上一次连接失败: ' + data.boot_error
    }
  } catch (e) {
    router.replace('/login')
  }
})

async function onTest() {
  testing.value = true
  errMsg.value = ''
  testResult.value = null
  testedOk.value = false
  try {
    const data = await setupTest({ ...form })
    testResult.value = data
    testedOk.value = true
  } catch (e) {
    errMsg.value = e.message || '连接失败,请检查参数'
  } finally {
    testing.value = false
  }
}

async function onSave() {
  if (!testedOk.value) {
    errMsg.value = '请先点击「测试连接」'
    return
  }
  saving.value = true
  errMsg.value = ''
  try {
    const data = await setupSave({ ...form })
    // 保存成功,后端已热加载;带默认账号提示跳登录页
    if (data && data.admin) {
      router.replace({
        path: '/login',
        query: { setup_done: '1' },
      })
    } else {
      router.replace('/login')
    }
  } catch (e) {
    errMsg.value = e.message || '保存失败,请重试'
    testedOk.value = false
  } finally {
    saving.value = false
  }
}
</script>

<style scoped>
.lg {
  display: flex;
  height: 100%;
  background: var(--bg-app);
}

/* ---------- 品牌侧(同 Login) ---------- */
.lg-brand {
  flex: 1.1;
  position: relative;
  overflow: hidden;
  display: flex;
  flex-direction: column;
  padding: 44px 52px 30px;
  color: #fff;
}
.brand-bg {
  position: absolute;
  inset: 0;
  background: linear-gradient(150deg, #2b5cff 0%, #6b3dff 45%, #ff4f81 100%);
}
.brand-bg::after {
  content: '';
  position: absolute;
  inset: 0;
  background:
    radial-gradient(900px 420px at 8% 12%, rgba(255, 255, 255, 0.26), transparent 62%),
    radial-gradient(700px 460px at 92% 88%, rgba(0, 0, 0, 0.28), transparent 60%);
}
.brand-in {
  position: relative;
  z-index: 1;
  flex: 1;
  display: flex;
  flex-direction: column;
  justify-content: center;
  max-width: 460px;
}
.lg-logo { display: flex; align-items: center; gap: 11px; }
.lg-mark {
  width: 38px;
  height: 38px;
  display: grid;
  place-items: center;
  border-radius: 11px;
  background: rgba(255, 255, 255, 0.2);
  backdrop-filter: blur(8px);
  border: 1px solid rgba(255, 255, 255, 0.3);
}
.lg-word { font-size: 20px; font-weight: 700; letter-spacing: 0.4px; }
.lg-slogan {
  margin: 34px 0 12px;
  font-size: 40px;
  line-height: 1.24;
  font-weight: 750;
  letter-spacing: 0.5px;
}
.lg-desc {
  margin: 0 0 30px;
  font-size: 14.5px;
  line-height: 1.7;
  color: rgba(255, 255, 255, 0.85);
}
.brand-foot {
  position: relative;
  z-index: 1;
  font-size: 12px;
  color: rgba(255, 255, 255, 0.62);
}

/* ---------- 表单侧 ---------- */
.lg-form {
  flex: 1;
  display: grid;
  place-items: center;
  padding: 30px;
}
.lg-card {
  width: 100%;
  max-width: 392px;
}
.lg-card h1 {
  margin: 0 0 6px;
  font-size: 25px;
  font-weight: 700;
  letter-spacing: 0.3px;
}
.lg-sub {
  margin: 0 0 26px;
  color: var(--text-weak);
  font-size: 13.5px;
}

.fld { display: block; margin-bottom: 16px; }
.fld > span {
  display: block;
  margin-bottom: 7px;
  font-size: 12.5px;
  font-weight: 600;
  color: var(--text-sub);
}
.row { display: flex; gap: 12px; }
.row .fld { flex: 1; }
.ipt {
  display: flex;
  align-items: center;
  gap: 9px;
  height: 44px;
  padding: 0 13px;
  border-radius: var(--r-md);
  border: 1px solid var(--border);
  background: var(--bg-surface);
  color: var(--text-weak);
  transition: border-color 0.16s, box-shadow 0.16s;
}
.ipt.focus {
  border-color: var(--brand);
  box-shadow: 0 0 0 3px var(--brand-ring);
}
.ipt input {
  flex: 1;
  min-width: 0;
  border: none;
  outline: none;
  background: transparent;
  color: var(--text);
  font-size: 14px;
  font-family: inherit;
}
.ipt input::placeholder { color: var(--text-weak); }
.eye {
  border: none;
  background: transparent;
  color: var(--text-weak);
  cursor: pointer;
  display: grid;
  place-items: center;
  padding: 3px;
}
.eye:hover { color: var(--text-sub); }

.ok-msg {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 9px 12px;
  margin-bottom: 14px;
  border-radius: var(--r-sm);
  background: rgba(34, 197, 94, 0.12);
  color: #16a34a;
  font-size: 12.5px;
}
.err {
  display: flex;
  align-items: center;
  gap: 7px;
  padding: 9px 12px;
  margin-bottom: 14px;
  border-radius: var(--r-sm);
  background: rgba(240, 68, 56, 0.1);
  color: var(--danger);
  font-size: 12.5px;
}

.btns { display: flex; gap: 10px; }
.ghost, .submit {
  flex: 1;
  height: 44px;
  margin-top: 6px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  border: none;
  border-radius: var(--r-md);
  font-size: 14.5px;
  font-weight: 650;
  font-family: inherit;
  cursor: pointer;
  transition: all 0.16s;
}
.ghost {
  background: var(--bg-subtle);
  color: var(--text-sub);
}
.ghost:hover:not(:disabled) {
  background: var(--bg-surface);
}
.submit {
  background: var(--brand);
  color: #fff;
  box-shadow: 0 6px 18px rgba(43, 92, 255, 0.3);
}
.submit:hover:not(:disabled) {
  background: var(--brand-hover);
  transform: translateY(-1px);
  box-shadow: 0 8px 22px rgba(43, 92, 255, 0.38);
}
.ghost:disabled, .submit:disabled { opacity: 0.6; cursor: not-allowed; }
.spin {
  width: 14px;
  height: 14px;
  border: 2px solid rgba(255, 255, 255, 0.4);
  border-top-color: #fff;
  border-radius: 50%;
  animation: spin 0.7s linear infinite;
}
.ghost .spin { border-color: var(--text-weak); border-top-color: var(--text); }
@keyframes spin { to { transform: rotate(360deg); } }

.hint {
  display: flex;
  gap: 8px;
  margin-top: 20px;
  padding: 11px 13px;
  border-radius: var(--r-md);
  background: var(--bg-subtle);
  color: var(--text-weak);
  font-size: 12px;
  line-height: 1.65;
}
.hint b { color: var(--text-sub); }

@media (max-width: 900px) {
  .lg-brand { display: none; }
}
</style>
