<template>
  <div class="lg">
    <!-- 左侧品牌区 -->
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
          把照片<br />留在自己手里
        </h2>
        <p class="lg-desc">
          私有云相册 · 只读挂载不碰原图 · 智能缩略图 · 手机自动备份
        </p>

        <ul class="lg-feats">
          <li><AppIcon name="layers" :size="15" /><span>原生目录索引，不复制不改写</span></li>
          <li><AppIcon name="sparkle" :size="15" /><span>libvips 高速缩略图，浏览秒开</span></li>
          <li><AppIcon name="devices" :size="15" /><span>手机相册 Wi-Fi 下自动同步</span></li>
        </ul>
      </div>
      <div class="brand-foot">v{{ APP_VERSION }} · Self-hosted Photo Vault</div>
    </section>

    <!-- 右侧表单区 -->
    <section class="lg-form">
      <div class="lg-card np-in">
        <h1>登录</h1>
        <p class="lg-sub">使用管理员账号进入你的相册</p>

        <form @submit.prevent="onLogin">
          <label class="fld">
            <span>用户名</span>
            <div class="ipt" :class="{ focus: focusU }">
              <AppIcon name="user" :size="16" />
              <input v-model="form.username" autocomplete="username" placeholder="admin"
                @focus="focusU = true" @blur="focusU = false" />
            </div>
          </label>

          <label class="fld">
            <span>密码</span>
            <div class="ipt" :class="{ focus: focusP }">
              <AppIcon name="star" :size="16" />
              <input v-model="form.password" :type="showPw ? 'text' : 'password'"
                autocomplete="current-password" placeholder="请输入密码"
                @focus="focusP = true" @blur="focusP = false" @keyup.enter="onLogin" />
              <button type="button" class="eye" @click="showPw = !showPw">
                <AppIcon :name="showPw ? 'eyeOff' : 'eye'" :size="15" />
              </button>
            </div>
          </label>

          <div v-if="errMsg" class="err">
            <AppIcon name="info" :size="14" /> {{ errMsg }}
          </div>

          <button class="submit" type="submit" :disabled="loading">
            <span v-if="loading" class="spin" />
            {{ loading ? '正在登录…' : '登录' }}
          </button>
        </form>

        <div class="hint">
          <AppIcon name="info" :size="14" />
          <span>首次启动默认账号 <b>admin</b> / <b>naspic123</b>，登录后请立即修改密码。</span>
        </div>
      </div>
    </section>
  </div>
</template>

<script setup>
import { reactive, ref } from 'vue'
import { useRouter } from 'vue-router'
import AppIcon from '../components/AppIcon.vue'
import { APP_NAME, APP_VERSION } from '../version'
import { login } from '../api'

const router = useRouter()
const loading = ref(false)
const showPw = ref(false)
const focusU = ref(false)
const focusP = ref(false)
const errMsg = ref('')
const form = reactive({ username: 'admin', password: '' })

async function onLogin() {
  if (!form.username || !form.password) {
    errMsg.value = '请输入用户名和密码'
    return
  }
  loading.value = true
  errMsg.value = ''
  try {
    const data = await login(form.username, form.password)
    localStorage.setItem('naspic.token', data.token)
    localStorage.setItem('naspic.user', form.username)
    localStorage.setItem('naspic.role', String(data.role || 1))
    if (data.user_id) localStorage.setItem('naspic.uid', String(data.user_id))
    const redirect = router.currentRoute.value.query.redirect
    router.push(typeof redirect === 'string' && redirect ? redirect : '/photos')
  } catch (e) {
    errMsg.value = e.message || '登录失败，请检查账号密码'
  } finally {
    loading.value = false
  }
}
</script>

<style scoped>
.lg {
  display: flex;
  height: 100%;
  background: var(--bg-app);
}

/* ---------- 品牌侧 ---------- */
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
.lg-feats {
  list-style: none;
  margin: 0;
  padding: 0;
  display: flex;
  flex-direction: column;
  gap: 13px;
}
.lg-feats li {
  display: flex;
  align-items: center;
  gap: 11px;
  font-size: 13.5px;
  color: rgba(255, 255, 255, 0.92);
}
.lg-feats li :deep(.np-icon) {
  opacity: 0.85;
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
  max-width: 372px;
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

.submit {
  width: 100%;
  height: 44px;
  margin-top: 6px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  gap: 8px;
  border: none;
  border-radius: var(--r-md);
  background: var(--brand);
  color: #fff;
  font-size: 14.5px;
  font-weight: 650;
  font-family: inherit;
  cursor: pointer;
  box-shadow: 0 6px 18px rgba(43, 92, 255, 0.3);
  transition: all 0.16s;
}
.submit:hover:not(:disabled) {
  background: var(--brand-hover);
  transform: translateY(-1px);
  box-shadow: 0 8px 22px rgba(43, 92, 255, 0.38);
}
.submit:disabled { opacity: 0.7; cursor: not-allowed; }
.submit .spin {
  width: 14px;
  height: 14px;
  border-color: rgba(255, 255, 255, 0.4);
  border-top-color: #fff;
}

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
