<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { session } from '../composables/useSession'
import type { SupplierAccount, Authorization, AccountOperation } from '../api/types'
import RequestError from '../components/RequestError.vue'

const props = defineProps<{ review?: boolean }>()
const { t } = useI18n()
const items = ref<SupplierAccount[]>([])
const total = ref(0)
const page = ref(1)
const pages = ref(0)
const loading = ref(true)
const busy = ref(false)
const error = ref<unknown>(null)
const name = ref('')
const code = ref('')
const reason = ref('')
const authorization = ref<Authorization | null>(null)
const operation = ref<AccountOperation | null>(null)
const completeKey = ref('')
const actionKeys = new Map<string, string>()
let alive = true
let generation = 0
let timer: ReturnType<typeof setTimeout> | undefined

async function load(next = page.value) {
  const current = ++generation
  loading.value = true; error.value = null
  try {
    const data = await session.client.accounts(next, props.review)
    if (!alive || current !== generation) return
    items.value = data.items; total.value = data.total; page.value = data.page; pages.value = data.pages
  } catch (cause) {
    if (alive && current === generation) { items.value = []; total.value = 0; error.value = cause }
  } finally { if (alive && current === generation) loading.value = false }
}

async function begin() {
  if (busy.value) return
  busy.value = true; error.value = null; operation.value = null
  try {
    const result = await session.client.beginAuthorization(name.value.trim())
    if (alive) { authorization.value = result; completeKey.value = crypto.randomUUID() }
  } catch (cause) { if (alive) error.value = cause }
  finally { if (alive) busy.value = false }
}

async function poll(id: string, attempts = 0) {
  if (!alive) return
  try {
    const result = await session.client.accountOperation(id)
    if (!alive) return
    operation.value = result
    if (['pending', 'running'].includes(result.state) && attempts < 30) {
      timer = setTimeout(() => poll(id, attempts + 1), 2000)
    } else await load()
  } catch (cause) { if (alive) error.value = cause }
}

async function complete() {
  if (busy.value || !authorization.value) return
  busy.value = true; error.value = null
  try {
    const result = await session.client.completeAuthorization(authorization.value.id, code.value.trim(), completeKey.value)
    if (!alive) return
    code.value = ''; authorization.value = null; name.value = ''; operation.value = result
    if (['pending', 'running'].includes(result.state)) timer = setTimeout(() => poll(result.id), 2000)
    else await load()
  } catch (cause) { if (alive) error.value = cause /* Retain the same in-memory code/key for an ambiguous transport retry. */ }
  finally { if (alive) busy.value = false }
}

async function change(account: SupplierAccount) {
  if (busy.value) return
  const request = `${account.id}:${account.version}:${props.review ? 'approve' : 'pause'}`
  const key = actionKeys.get(request) || crypto.randomUUID()
  actionKeys.set(request, key)
  busy.value = true; error.value = null
  try {
    const result = await session.client.changeAccount(account, !!props.review, key, reason.value.trim())
    if (!alive) return
    operation.value = result
    await load()
  } catch (cause) { if (alive) error.value = cause }
  finally { if (alive) busy.value = false }
}

function cancel() { authorization.value = null; code.value = ''; completeKey.value = '' }
onMounted(() => load())
onUnmounted(() => { alive = false; generation++; code.value = ''; authorization.value = null; if (timer) clearTimeout(timer) })
</script>

<template>
  <section class="card">
    <div class="section-heading"><div><h1>{{ t(review ? 'accounts.reviewTitle' : 'accounts.title') }}</h1><p class="muted">{{ t(review ? 'accounts.reviewHint' : 'accounts.description') }}</p></div><button :disabled="loading || busy" @click="load()">{{ t('app.refresh') }}</button></div>
    <RequestError :error="error" />
    <p v-if="operation" role="status" :class="['notice', { warning: ['failed', 'needs_attention'].includes(operation.state) }]">{{ t(`accounts.operation.${operation.state}`) }} <span class="mono">{{ operation.id }}</span></p>
    <form v-if="!review && !authorization" class="account-add" @submit.prevent="begin">
      <label for="account-name">{{ t('accounts.name') }}</label><input id="account-name" v-model="name" required maxlength="100" :disabled="busy" />
      <button class="primary" type="submit" :disabled="busy">{{ t('accounts.add') }}</button>
    </form>
    <form v-if="authorization" class="oauth-flow" @submit.prevent="complete">
      <h2>{{ t('accounts.authorizeTitle') }}</h2><p>{{ t('accounts.authorizeHint') }}</p>
      <a :href="authorization.authorization_url" target="_blank" rel="noopener noreferrer">{{ t('accounts.openOfficial') }}</a>
      <p class="muted">{{ t('accounts.expires') }} {{ new Date(authorization.expires_at).toLocaleString() }}</p>
      <label for="authorization-code">{{ t('accounts.code') }}</label><input id="authorization-code" v-model="code" type="password" autocomplete="off" required maxlength="4096" :disabled="busy" />
      <div class="toolbar"><button type="submit" class="primary" :disabled="busy">{{ t('accounts.submit') }}</button><button type="button" :disabled="busy" @click="cancel">{{ t('accounts.cancel') }}</button></div>
    </form>
    <div v-if="review" class="account-add"><label for="review-reason">{{ t('accounts.reason') }}</label><input id="review-reason" v-model="reason" maxlength="500" :disabled="busy" /></div>
    <p v-if="loading" role="status">{{ t('app.loading') }}</p>
    <template v-else><p class="muted">{{ t('accounts.total', { count: total }) }}</p>
      <p v-if="!items.length" class="empty-state">{{ t(review ? 'accounts.reviewEmpty' : 'accounts.empty') }}</p>
      <div v-else class="table-scroll"><table><thead><tr><th>{{ t('accounts.name') }}</th><th>{{ t('accounts.platform') }}</th><th>{{ t('accounts.state') }}</th><th>{{ t('accounts.supply') }}</th><th>{{ t('accounts.actions') }}</th></tr></thead><tbody>
        <tr v-for="account in items" :key="account.id"><td>{{ account.name }}</td><td>Claude OAuth</td><td>{{ t(`accounts.states.${account.admission_state}`) }}</td><td>{{ t(`accounts.supplyStates.${account.desired_state}`) }}</td><td>
          <button v-if="review && account.admission_state === 'pending'" :disabled="busy || !reason.trim()" @click="change(account)">{{ t('accounts.approve') }}</button>
          <button v-else-if="!review && account.admission_state === 'approved' && account.desired_state === 'running'" :disabled="busy" @click="change(account)">{{ t('accounts.pause') }}</button>
          <span v-else class="muted">—</span>
        </td></tr>
      </tbody></table></div>
      <div v-if="pages > 1" class="pagination"><button :disabled="page <= 1 || busy" @click="load(page - 1)">{{ t('accounts.previous') }}</button><span>{{ page }} / {{ pages }}</span><button :disabled="page >= pages || busy" @click="load(page + 1)">{{ t('accounts.next') }}</button></div>
    </template>
  </section>
</template>
