<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { IconShieldLock, IconUser } from '@tabler/icons-vue'
import { api } from '../api'
import BaseButton from './ui/BaseButton.vue'

const props = defineProps<{ defaultRole: 'admin' | 'user' }>()
const emit = defineEmits<{ authenticated: [identity: { username: string; role: 'admin' | 'user' }] }>()
const role = ref(props.defaultRole)
const register = ref(false)
const initialized = ref(true)
const username = ref('')
const password = ref('')
const confirmation = ref('')
const error = ref('')
const busy = ref(false)
const setup = computed(() => role.value === 'admin' && !initialized.value)
const creating = computed(() => setup.value || register.value)
const title = computed(() => setup.value ? '创建管理员' : register.value ? '创建你的账号' : '欢迎回来')
onMounted(async () => {
  try { initialized.value = (await api<{ initialized: boolean }>('/admin/auth/initialized')).initialized } catch { error.value = '无法连接服务，请稍后重试' }
})
function selectRole(value: 'admin' | 'user') { role.value = value; register.value = false; error.value = ''; password.value = ''; confirmation.value = '' }
async function submit() {
  error.value = ''
  if (creating.value && password.value !== confirmation.value) { error.value = '两次输入的密码不一致'; return }
  busy.value = true
  try {
    const path = role.value === 'admin' ? '/admin/auth/' + (setup.value ? 'setup' : 'login') : '/auth/' + (register.value ? 'register' : 'login')
    const identity = await api<{ username: string; role: 'admin' | 'user' }>(path, { body: { username: role.value === 'user' ? username.value.trim() : username.value, password: password.value } })
    password.value = ''; confirmation.value = ''
    emit('authenticated', { ...identity, role: role.value })
  } catch (e: any) { error.value = e.message || '操作失败，请重试' }
  finally { busy.value = false }
}
</script>

<template>
  <main class="auth-page">
    <section class="auth-card" aria-label="账号登录注册">
      <a href="/portal" class="brand">AI Gateway<span>统一模型服务平台</span></a>
      <div class="role-tabs" aria-label="账号类型">
        <button type="button" :class="{ active: role === 'user' }" :aria-pressed="role === 'user'" :disabled="busy" @click="selectRole('user')"><IconUser :size="17" />普通用户</button>
        <button type="button" :class="{ active: role === 'admin' }" :aria-pressed="role === 'admin'" :disabled="busy" @click="selectRole('admin')"><IconShieldLock :size="17" />管理员</button>
      </div>
      <h1>{{ title }}</h1>
      <p class="intro">{{ role === 'admin' ? (setup ? '首次使用，设置管理账号。' : '登录管理后台，管理模型与运营数据。') : register ? '无需邀请码，注册后即可进入用户控制台。' : '登录后管理你的模型、订阅和用量。' }}</p>
      <form @submit.prevent="submit">
        <label for="account-username">用户名</label>
        <input id="account-username" v-model="username" required :maxlength="role === 'user' ? 32 : 64" autocomplete="username" :pattern="role === 'user' ? '[a-zA-Z0-9_-]{3,32}' : undefined" :placeholder="role === 'user' ? '3–32 位字母、数字、下划线或短横线' : '管理员用户名'" :disabled="busy" />
        <label for="account-password">密码</label>
        <input id="account-password" v-model="password" type="password" required :minlength="role === 'user' || creating ? 8 : 6" maxlength="72" :autocomplete="creating ? 'new-password' : 'current-password'" :placeholder="creating ? '至少 8 位密码' : '输入密码'" :disabled="busy" />
        <template v-if="creating"><label for="account-confirmation">确认密码</label><input id="account-confirmation" v-model="confirmation" type="password" required maxlength="72" autocomplete="new-password" placeholder="再次输入密码" :disabled="busy" /></template>
        <p v-if="error" class="error" role="alert">{{ error }}</p>
        <BaseButton type="submit" variant="primary" block size="lg" :loading="busy">{{ setup ? '创建管理员并进入' : register ? '注册并进入' : '登录' }}</BaseButton>
      </form>
      <p v-if="role === 'user'" class="switch">{{ register ? '已有账号？' : '还没有账号？' }}<button type="button" :disabled="busy" @click="register = !register; error = ''; confirmation = ''">{{ register ? '立即登录' : '直接注册' }}</button></p>
      <p v-else class="admin-note">管理员账号由系统初始化创建，公开注册仅开放普通用户。</p>
    </section>
    <p class="foot">一个账号，连接模型与应用。</p>
  </main>
</template>

<style scoped>
.auth-page{min-height:100dvh;display:flex;flex-direction:column;align-items:center;justify-content:center;padding:32px 20px;background:var(--bg)}
.auth-card{width:100%;max-width:420px;padding:32px;background:var(--surface,#fff);border:1px solid var(--line-2);border-radius:12px}
.brand{display:flex;flex-direction:column;font-size:21px;font-weight:700;color:var(--ink-900);text-decoration:none;margin-bottom:28px}.brand span{font-size:12px;font-weight:400;color:var(--ink-500);margin-top:4px}
.role-tabs{display:flex;padding:4px;background:var(--bg);border-radius:8px;gap:4px;margin-bottom:28px}.role-tabs button{display:flex;align-items:center;justify-content:center;gap:7px;flex:1;padding:9px;border:0;background:transparent;color:var(--ink-500);cursor:pointer;border-radius:5px}.role-tabs button.active{background:var(--surface,#fff);color:var(--ink-900);box-shadow:0 1px 3px #0000000d}
h1{font-size:23px;font-weight:650;letter-spacing:-.6px;margin:0 0 8px}.intro{color:var(--ink-500);font-size:13px;line-height:1.7;margin:0 0 24px}
form{display:flex;flex-direction:column;gap:8px}label{font-size:13px;font-weight:500}input{width:100%;padding:11px 12px;border:1px solid var(--line-2);border-radius:6px;background:var(--surface,#fff);color:var(--ink-900);font:inherit;font-size:13px;margin-bottom:10px}input:focus{outline:2px solid var(--accent-600);outline-offset:1px}.error{font-size:13px;color:#b42318;line-height:1.6;margin:0 0 8px}.switch,.admin-note{font-size:12px;color:var(--ink-500);text-align:center;margin:22px 0 0;line-height:1.7}.switch button{background:none;border:0;color:var(--accent-600);cursor:pointer;margin-left:6px}.foot{font-size:12px;color:var(--ink-500);margin-top:24px}@media(max-width:480px){.auth-card{padding:24px}.auth-page{padding:20px 16px}}
</style>
