<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { session } from '../composables/useSession'
import RequestError from '../components/RequestError.vue'
const { t } = useI18n()
const email = ref('')
const password = ref('')
const code = ref('')
const turnstile = ref('')
const ticket = ref('')
const randstr = ref('')
const busy = ref(false)
const error = ref<unknown>(null)

async function submit() {
  if (busy.value) return
  busy.value = true
  error.value = null
  try {
    const request = session.challenge.value
      ? session.completeTwoFactor(code.value.trim())
      : session.login({ email: email.value.trim(), password: password.value,
          ...(turnstile.value ? { turnstile_token: turnstile.value } : {}),
          ...(ticket.value ? { tencent_captcha_ticket: ticket.value } : {}),
          ...(randstr.value ? { tencent_captcha_randstr: randstr.value } : {}) })
    password.value = ''
    code.value = ''
    turnstile.value = ''
    ticket.value = ''
    randstr.value = ''
    await request
  } catch (cause) { error.value = cause }
  finally { busy.value = false }
}

function cancel() { session.clear(); error.value = null; code.value = '' }
</script>

<template>
  <section class="card login">
    <h1>{{ t(session.challenge.value ? 'auth.twoFactor' : 'auth.title') }}</h1>
    <p class="muted">{{ t('auth.description') }}</p>
    <RequestError :error="error" />
    <form @submit.prevent="submit">
      <template v-if="!session.challenge.value">
        <label for="email">{{ t('auth.email') }}</label>
        <input id="email" v-model="email" name="email" type="email" autocomplete="username" required maxlength="320" :disabled="busy" />
        <label for="password">{{ t('auth.password') }}</label>
        <input id="password" v-model="password" name="password" type="password" autocomplete="current-password" required :disabled="busy" />
        <details>
          <summary>{{ t('auth.captcha') }}</summary>
          <p class="muted">{{ t('auth.captchaHint') }}</p>
          <label for="turnstile">{{ t('auth.turnstile') }}</label>
          <input id="turnstile" v-model="turnstile" autocomplete="off" :disabled="busy" />
          <label for="ticket">{{ t('auth.ticket') }}</label>
          <input id="ticket" v-model="ticket" autocomplete="off" :disabled="busy" />
          <label for="randstr">{{ t('auth.randstr') }}</label>
          <input id="randstr" v-model="randstr" autocomplete="off" :disabled="busy" />
        </details>
      </template>
      <template v-else>
        <p class="muted">{{ t('auth.expires') }}: {{ new Date(session.challenge.value.expires_at).toLocaleString() }}</p>
        <label for="totp">{{ t('auth.code') }}</label>
        <input id="totp" v-model="code" inputmode="numeric" autocomplete="one-time-code" pattern="[0-9]{6}" minlength="6" maxlength="6" required :disabled="busy" />
      </template>
      <button class="primary wide" type="submit" :disabled="busy">{{ t(busy ? 'auth.submitting' : session.challenge.value ? 'auth.verify' : 'auth.login') }}</button>
      <button v-if="session.challenge.value" class="wide" type="button" :disabled="busy" @click="cancel">{{ t('auth.cancel') }}</button>
    </form>
  </section>
</template>
