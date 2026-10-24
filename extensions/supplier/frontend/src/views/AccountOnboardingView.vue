<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useSupplierI18n } from '../i18n/useSupplierI18n'
import { ApiError, type SupplierClient } from '../api/client'
import type { AccountOperation, Authorization, OnboardingOptions, OwnProxy } from '../api/types'
import PortalDialog from '../components/PortalDialog.vue'
import ProxyCreateForm from '../components/ProxyCreateForm.vue'
import RequestError from '../components/RequestError.vue'
const props = defineProps<{ client: SupplierClient }>()
const emit = defineEmits<{ close: []; created: [] }>()
const { t } = useSupplierI18n()
const options = ref<OnboardingOptions>({ groups: [], proxies: [] }), loading = ref(true), busy = ref(false), error = ref<unknown>(null)
const name = ref(''), selectedGroupKeys = ref<number[]>([]), source = ref('existing'), proxyID = ref(''), code = ref(''), advanced = ref(false)
const authorization = ref<Authorization | null>(null), operation = ref<AccountOperation | null>(null), submitted = ref(false)
const authorizationUrlInput = ref<HTMLTextAreaElement | null>(null)
const copying = ref(false), copyState = ref<'idle' | 'copied' | 'failed'>('idle')
const beginKey = ref(crypto.randomUUID()), completeKey = ref(crypto.randomUUID())
const eligible = computed(() => options.value.proxies.filter(p => p.status === 'active' && p.accounts + p.held < p.capacity))
const selectedGroupIds = computed(() => options.value.groups.filter(item => selectedGroupKeys.value.includes(item.id)).flatMap(item => item.group_ids?.length ? item.group_ids : [item.id]))
const canBegin = computed(() => !loading.value && !busy.value && selectedGroupIds.value.length > 0 && (source.value === 'auto' ? eligible.value.length > 0 : source.value === 'existing' && eligible.value.some(p => p.id === proxyID.value)))
let alive = true, poll: ReturnType<typeof setTimeout> | undefined
async function load() {
  loading.value = true; error.value = null
  try { const data = await props.client.onboardingOptions(); if (!alive) return; options.value = data; if (!selectedGroupKeys.value.length) selectedGroupKeys.value = data.groups[0] ? [data.groups[0].id] : [] }
  catch (cause) { if (alive) error.value = cause }
  finally { if (alive) loading.value = false }
}
async function proxyCreated(proxy: OwnProxy) { source.value = 'existing'; proxyID.value = proxy.id; await load() }
async function begin() {
  if (!canBegin.value) return
  busy.value = true; error.value = null
  try { const data = await props.client.beginOnboarding(name.value.trim(), source.value === 'auto' ? '' : proxyID.value, selectedGroupIds.value, beginKey.value); if (alive) authorization.value = data }
  catch (cause) { if (alive) error.value = cause }
  finally { if (alive) busy.value = false }
}
async function copyAuthorizationLink() {
  const url = authorization.value?.authorization_url
  if (!url || copying.value) return
  copying.value = true; copyState.value = 'idle'
  try {
    if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable')
    await navigator.clipboard.writeText(url)
    if (alive) copyState.value = 'copied'
  } catch {
    if (alive) {
      copyState.value = 'failed'
      authorizationUrlInput.value?.focus()
      authorizationUrlInput.value?.select()
    }
  } finally { if (alive) copying.value = false }
}
function showResult(result: AccountOperation) {
  operation.value = result
  if (result.state === 'succeeded') emit('created')
  else if (['running','pending'].includes(result.state)) poll = setTimeout(() => void check(result.id), 2000)
}
async function check(id: string) {
  try { const result = await props.client.accountOperation(id); if (alive) showResult(result) }
  catch (cause) { if (alive) error.value = cause }
}
async function complete() {
  if (!authorization.value || busy.value || !code.value.trim() || operation.value) return
  busy.value = true; error.value = null; submitted.value = true
  try {
    const request = props.client.completeAuthorization(authorization.value.id, code.value.trim(), completeKey.value)
    code.value = ''
    const result = await request; if (alive) showResult(result)
  } catch (cause) { if (alive) { error.value = cause; if (cause instanceof ApiError && [400,404,409].includes(cause.status)) submitted.value = false } }
  finally { if (alive) busy.value = false }
}
async function close() {
  if (busy.value || loading.value) return
  if (authorization.value && !submitted.value) {
    busy.value = true; error.value = null
    try { await props.client.cancelOnboarding(authorization.value.id) }
    catch (cause) { if (!(cause instanceof ApiError && [404,409].includes(cause.status))) { error.value = cause; busy.value = false; return } }
  }
  emit('close')
}
onMounted(load)
onUnmounted(() => { alive = false; clearTimeout(poll); code.value = '' })
</script>
<template>
  <PortalDialog :title="t('workspace.addAccount')" :footer="false" @close="close">
    <div class="onboarding-content">
      <p v-if="loading" role="status">{{ t('onboarding.loading') }}</p><RequestError :error="error" />
      <template v-if="!authorization && !loading">
        <label class="field-caption">{{ t('onboarding.authMethod') }}</label><div class="onboarding-tabs"><button class="selected" type="button">{{ t('onboarding.manual') }}</button><button type="button" disabled :title="t('onboarding.autoUnavailable')">{{ t('onboarding.automatic') }}</button></div>
        <p class="onboarding-hint">{{ t('onboarding.autoUnavailable') }}</p>
        <div class="onboarding-form"><label>{{ t('onboarding.proxySource') }}<select v-model="source" :disabled="busy"><option value="existing">{{ t('onboarding.existing') }}</option><option value="new">{{ t('onboarding.newProxy') }}</option><option value="auto">{{ t('onboarding.autoProxy') }}</option></select></label></div>
        <ProxyCreateForm v-if="source === 'new'" :client="client" inline @created="proxyCreated" @busy="value => busy = value" />
        <div v-else class="onboarding-form">
          <label v-if="source === 'existing'">{{ t('onboarding.existing') }}<select v-model="proxyID" :disabled="busy"><option value="">{{ t('onboarding.chooseProxy') }}</option><option v-for="proxy in eligible" :key="proxy.id" :value="proxy.id">{{ proxy.name }} · {{ proxy.address }} · {{ proxy.accounts + proxy.held }}/{{ proxy.capacity }}</option></select></label>
          <p v-if="!eligible.length" class="onboarding-warning">{{ t('onboarding.noProxy') }}</p>
          <p v-if="source === 'auto'" class="onboarding-hint">{{ t('onboarding.autoProxyHint') }}</p>
        </div>
        <div class="onboarding-form"><label>{{ t('onboarding.group') }}<select v-model="selectedGroupKeys" multiple size="4" :disabled="busy"><option v-if="!options.groups.length" :value="0">{{ t('onboarding.noGroup') }}</option><option v-for="item in options.groups" :key="item.id" :value="item.id">{{ item.name }}</option></select></label><p v-if="options.groups.length > 1" class="onboarding-hint">{{ t('onboarding.groupMultiHint') }}</p></div>
        <button class="text-button" type="button" :aria-expanded="advanced" @click="advanced = !advanced">{{ t('onboarding.advanced') }} ⌄</button><div v-if="advanced" class="onboarding-form"><label>{{ t('accounts.name') }}<input v-model="name" maxlength="100" :placeholder="t('onboarding.nameHint')" :disabled="busy" /></label></div>
        <p class="onboarding-hint">{{ t('onboarding.schedulingHint') }}</p><button class="primary wide" :disabled="!canBegin" @click="begin">{{ t(busy ? 'auth.submitting' : 'onboarding.generate') }}</button>
      </template>
      <template v-else-if="authorization">
        <div class="authorization-summary"><b>{{ t('onboarding.parametersLocked') }}</b><small>{{ t('onboarding.expires') }} {{ new Date(authorization.expires_at).toLocaleString() }}</small></div>
        <a class="authorization-link" :href="authorization.authorization_url" target="_blank" rel="noopener noreferrer">{{ t('accounts.openOfficial') }} ↗</a>
        <div class="onboarding-form">
          <label>{{ t('onboarding.authorizationUrl') }}<textarea ref="authorizationUrlInput" class="authorization-url" :value="authorization.authorization_url" readonly rows="3" autocomplete="off" spellcheck="false" /></label>
          <button type="button" :disabled="copying" @click="copyAuthorizationLink">{{ t('onboarding.copyLink') }}</button>
          <p v-if="copyState !== 'idle'" role="status" class="onboarding-hint">{{ t(copyState === 'copied' ? 'onboarding.linkCopied' : 'onboarding.copyLinkFailed') }}</p>
          <p class="onboarding-hint">{{ t('onboarding.crossBrowserHint') }}</p>
        </div>
        <p class="onboarding-hint">{{ t('onboarding.codeHint') }}</p>
        <form v-if="!operation" class="onboarding-form" @submit.prevent="complete"><label>{{ t('onboarding.callback') }}<textarea v-model="code" required maxlength="8192" autocomplete="off" spellcheck="false" :disabled="busy" /></label><button class="primary" :disabled="busy || !code.trim()">{{ t(busy ? 'auth.submitting' : 'onboarding.finish') }}</button></form>
        <div v-if="operation" class="onboarding-result" :class="operation.state" role="status"><b>{{ t(`onboarding.results.${operation.state}`) }}</b><p>{{ t(operation.state === 'succeeded' ? 'onboarding.createdRunning' : 'onboarding.noReplay') }}</p><small>{{ t('onboarding.operationID') }} {{ operation.id }}</small></div>
        <p v-else-if="submitted && !busy" class="onboarding-warning">{{ t('onboarding.uncertain') }}</p>
      </template>
      <div class="dialog-footer"><button :disabled="busy || loading" @click="close">{{ t(operation ? 'workspace.close' : 'accounts.cancel') }}</button><button v-if="error && !authorization" :disabled="busy" @click="load">{{ t('app.refresh') }}</button><button v-if="error && operation" @click="check(operation.id)">{{ t('onboarding.checkResult') }}</button></div>
    </div>
  </PortalDialog>
</template>
<style scoped>
.authorization-url { width: 100%; box-sizing: border-box; padding: 9px 11px; color: inherit; background: transparent; border: 1px solid #9fc6cc; border-radius: 7px; overflow-wrap: anywhere; resize: vertical; }
</style>
