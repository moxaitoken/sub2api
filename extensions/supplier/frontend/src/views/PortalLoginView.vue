<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SupplierClient } from '../api/client'
import type { LoginResult } from '../api/types'
import RequestError from '../components/RequestError.vue'
import PortalIcon from '../components/PortalIcon.vue'
const props = defineProps<{ client: SupplierClient; administrator: boolean }>()
const emit = defineEmits<{ authenticated: [] }>()
const { t } = useI18n()
const account = ref(''), password = ref(''), code = ref(''), busy = ref(false), error = ref<unknown>(null)
const challenge = ref<Extract<LoginResult, { requires_2fa: true }> | null>(null)
async function submit() {
  if (busy.value) return
  busy.value = true; error.value = null
  try {
    const promise = challenge.value ? props.client.completeTwoFactor(challenge.value.challenge_id, code.value) : props.administrator
      ? props.client.login({email: account.value.trim(), password: password.value}) : props.client.localLogin(account.value.trim(), password.value)
    password.value = ''; code.value = ''
    const result = await promise
    if (result.requires_2fa) challenge.value = result
    else { challenge.value = null; emit('authenticated') }
  } catch (cause) { error.value = cause }
  finally { busy.value = false }
}
</script>
<template>
  <section class="card login" :class="{ 'supplier-login-box': !administrator }">
    <div v-if="!administrator" class="login-brand"><PortalIcon name="brand" :size="30" /><b>MOXAI</b></div>
    <h1>{{ t(administrator ? 'portal.adminLogin' : 'portal.supplierLogin') }}</h1>
    <p class="muted">{{ t(administrator ? 'portal.adminHint' : 'portal.supplierHint') }}</p>
    <RequestError :error="error" />
    <form @submit.prevent="submit">
      <template v-if="!challenge">
        <label for="login-account">{{ t(administrator ? 'auth.email' : 'portal.loginAccount') }}</label>
        <input id="login-account" v-model="account" :type="administrator ? 'email' : 'text'" autocomplete="username" required :maxlength="administrator ? 320 : 64" :disabled="busy" />
        <label for="login-password">{{ t('auth.password') }}</label>
        <input id="login-password" v-model="password" type="password" autocomplete="current-password" required :disabled="busy" />
      </template>
      <template v-else>
        <label for="login-code">{{ t('auth.code') }}</label>
        <input id="login-code" v-model="code" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}" required :disabled="busy" />
      </template>
      <button class="primary wide" :disabled="busy">{{ t(busy ? 'auth.submitting' : challenge ? 'auth.verify' : 'auth.login') }}</button>
    </form>
    <a :href="administrator ? '/?portal=supplier' : '/?portal=admin'">{{ t(administrator ? 'portal.supplierLogin' : 'portal.adminLogin') }}</a>
  </section>
</template>
