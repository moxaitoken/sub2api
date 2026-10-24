<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useSupplierI18n } from '../i18n/useSupplierI18n'
import { ApiError, type SupplierClient } from '../api/client'
import type { AccountGroup, AccountOperation, AccountSettings, Authorization, OnboardingOptions, OwnProxy } from '../api/types'
import PortalDialog from '../components/PortalDialog.vue'
import ProxyCreateForm from '../components/ProxyCreateForm.vue'
import RequestError from '../components/RequestError.vue'
const props = defineProps<{ client: SupplierClient }>()
const emit = defineEmits<{ close: []; created: [] }>()
const { t } = useSupplierI18n()
const options = ref<OnboardingOptions>({ groups: [], proxies: [] }), loading = ref(true), busy = ref(false), error = ref<unknown>(null), groupOpen = ref(false)
const name = ref(''), selectedGroupKeys = ref<number[]>([]), source = ref('existing'), proxyID = ref(''), code = ref(''), advanced = ref(false)
const settings = ref({ concurrency: 100, windowCostEnabled: false, windowCostLimit: null as number | null, windowCostStickyReserve: 10, sessionLimitEnabled: true, maxSessions: 100, sessionIdleTimeout: 5, rpmLimitEnabled: true, baseRpm: 15, rpmStrategy: 'tiered' as 'tiered' | 'sticky_exempt', rpmStickyBuffer: null as number | null })
const authorization = ref<Authorization | null>(null), operation = ref<AccountOperation | null>(null), submitted = ref(false)
const authorizationUrlInput = ref<HTMLTextAreaElement | null>(null)
const groupPicker = ref<HTMLElement | null>(null)
const copying = ref(false), copyState = ref<'idle' | 'copied' | 'failed'>('idle')
const beginKey = ref(crypto.randomUUID()), completeKey = ref(crypto.randomUUID())
const eligible = computed(() => options.value.proxies.filter(p => p.status === 'active' && p.accounts + p.held < p.capacity))
const groupOptions = computed(() => {
  const merged = new Map<string, AccountGroup>()
  for (const item of options.value.groups) {
    const name = item.name.trim()
    const ids = item.group_ids?.length ? item.group_ids : [item.id]
    const current = merged.get(name)
    if (current) current.group_ids = [...new Set([...(current.group_ids || [current.id]), ...ids])]
    else merged.set(name, { ...item, name, group_ids: [...new Set(ids)] })
  }
  return [...merged.values()]
})
const selectedGroupIds = computed(() => [...new Set(groupOptions.value.filter(item => selectedGroupKeys.value.includes(item.id)).flatMap(item => item.group_ids?.length ? item.group_ids : [item.id]))])
const selectedGroupNames = computed(() => groupOptions.value.filter(item => selectedGroupKeys.value.includes(item.id)).map(item => item.name))
const withDefault = (value: unknown, fallback: number) => value === '' || value == null ? fallback : Number(value)
const accountSettings = computed<AccountSettings>(() => {
  const current = settings.value
  const output: AccountSettings = { concurrency: withDefault(current.concurrency, 100), max_sessions: 0, base_rpm: 0 }
  if (current.windowCostEnabled) {
    output.window_cost_limit = withDefault(current.windowCostLimit, 0)
    output.window_cost_sticky_reserve = withDefault(current.windowCostStickyReserve, 10)
  }
  if (current.sessionLimitEnabled) {
    output.max_sessions = withDefault(current.maxSessions, 100)
    output.session_idle_timeout_minutes = withDefault(current.sessionIdleTimeout, 5)
  }
  if (current.rpmLimitEnabled) {
    output.base_rpm = withDefault(current.baseRpm, 15)
    output.rpm_strategy = current.rpmStrategy
    if (current.rpmStrategy === 'tiered' && current.rpmStickyBuffer != null && String(current.rpmStickyBuffer) !== '') output.rpm_sticky_buffer = Number(current.rpmStickyBuffer)
  }
  return output
})
const settingsValid = computed(() => {
  const value = accountSettings.value
  const positiveInteger = (n: number, max = 2147483647) => Number.isInteger(n) && n >= 1 && n <= max
  return positiveInteger(value.concurrency) &&
    (!settings.value.sessionLimitEnabled || (positiveInteger(value.max_sessions!) && positiveInteger(value.session_idle_timeout_minutes!))) &&
    (!settings.value.rpmLimitEnabled || (positiveInteger(value.base_rpm!, 10000) && (value.rpm_sticky_buffer === undefined || positiveInteger(value.rpm_sticky_buffer)))) &&
    (!settings.value.windowCostEnabled || (Number.isFinite(value.window_cost_limit) && value.window_cost_limit! > 0 && Number.isFinite(value.window_cost_sticky_reserve) && value.window_cost_sticky_reserve! >= 0))
})
const canBegin = computed(() => !loading.value && !busy.value && settingsValid.value && selectedGroupIds.value.length > 0 && (source.value === 'auto' ? eligible.value.length > 0 : source.value === 'existing' && eligible.value.some(p => p.id === proxyID.value)))
let alive = true, poll: ReturnType<typeof setTimeout> | undefined
function toggleGroup(id: number) {
  if (busy.value) return
  selectedGroupKeys.value = selectedGroupKeys.value.includes(id)
    ? selectedGroupKeys.value.filter(value => value !== id)
    : [...selectedGroupKeys.value, id]
}
function outsideGroup(event: PointerEvent) {
  if (event.target instanceof Node && !groupPicker.value?.contains(event.target)) groupOpen.value = false
}
function closeGroup(event: KeyboardEvent) {
  if (!groupOpen.value) return
  event.stopPropagation(); event.preventDefault(); groupOpen.value = false
}
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
  try { const data = await props.client.beginOnboarding(name.value.trim(), source.value === 'auto' ? '' : proxyID.value, selectedGroupIds.value, beginKey.value, accountSettings.value); if (alive) authorization.value = data }
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
onMounted(() => { void load(); document.addEventListener('pointerdown', outsideGroup) })
onUnmounted(() => { alive = false; clearTimeout(poll); code.value = ''; document.removeEventListener('pointerdown', outsideGroup) })
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
        <div class="onboarding-form">
          <label for="supplier-group-picker">{{ t('onboarding.group') }}</label>
          <div ref="groupPicker" class="group-picker" @keydown.esc="closeGroup">
            <button id="supplier-group-picker" class="group-picker-trigger" type="button" :disabled="busy || !groupOptions.length" :aria-expanded="groupOpen" aria-haspopup="listbox" @click="groupOpen = !groupOpen">
              <span>{{ selectedGroupNames.length ? selectedGroupNames.join(', ') : t(groupOptions.length ? 'onboarding.chooseGroups' : 'onboarding.noGroup') }}</span><span aria-hidden="true">⌄</span>
            </button>
            <div v-if="groupOpen" class="group-picker-menu" role="listbox" :aria-label="t('onboarding.group')" aria-multiselectable="true">
              <button v-for="item in groupOptions" :key="item.id" class="group-picker-option" type="button" role="option" :aria-selected="selectedGroupKeys.includes(item.id)" :disabled="busy" @click="toggleGroup(item.id)">
                <span class="group-picker-check" aria-hidden="true">{{ selectedGroupKeys.includes(item.id) ? '✓' : '' }}</span><span>{{ item.name }}</span>
              </button>
            </div>
          </div>
          <p v-if="groupOptions.length > 1" class="onboarding-hint">{{ t('onboarding.groupMultiHint') }}</p>
        </div>
        <button class="text-button" type="button" :aria-expanded="advanced" @click="advanced = !advanced">{{ t('onboarding.advanced') }} ⌄</button>
        <p class="onboarding-hint">{{ t('onboarding.defaultsHint') }}</p>
        <div v-if="advanced" class="advanced-settings">
          <label>{{ t('accounts.name') }}<input v-model="name" maxlength="100" :placeholder="t('onboarding.nameHint')" :disabled="busy" /></label>
          <label>{{ t('onboarding.concurrency') }}<input v-model.number="settings.concurrency" data-testid="account-concurrency" type="number" min="1" step="1" :disabled="busy" @blur="settings.concurrency = withDefault(settings.concurrency, 100)" /></label>
          <div class="setting-card">
            <label class="setting-heading"><span>{{ t('onboarding.sessionLimit') }}</span><input v-model="settings.sessionLimitEnabled" data-testid="session-limit-enabled" type="checkbox" :disabled="busy" /></label>
            <div v-if="settings.sessionLimitEnabled" class="setting-grid">
              <label>{{ t('onboarding.maxSessions') }}<input v-model.number="settings.maxSessions" data-testid="max-sessions" type="number" min="1" step="1" :disabled="busy" @blur="settings.maxSessions = withDefault(settings.maxSessions, 100)" /></label>
              <label>{{ t('onboarding.sessionIdleTimeout') }}<input v-model.number="settings.sessionIdleTimeout" data-testid="session-idle" type="number" min="1" step="1" :disabled="busy" @blur="settings.sessionIdleTimeout = withDefault(settings.sessionIdleTimeout, 5)" /></label>
            </div>
          </div>
          <div class="setting-card">
            <label class="setting-heading"><span>{{ t('onboarding.rpmLimit') }}</span><input v-model="settings.rpmLimitEnabled" data-testid="rpm-limit-enabled" type="checkbox" :disabled="busy" /></label>
            <div v-if="settings.rpmLimitEnabled" class="setting-grid">
              <label>{{ t('onboarding.baseRpm') }}<input v-model.number="settings.baseRpm" data-testid="base-rpm" type="number" min="1" max="10000" step="1" :disabled="busy" @blur="settings.baseRpm = withDefault(settings.baseRpm, 15)" /></label>
              <label>{{ t('onboarding.rpmStrategy') }}<select v-model="settings.rpmStrategy" data-testid="rpm-strategy" :disabled="busy"><option value="tiered">{{ t('onboarding.rpmTiered') }}</option><option value="sticky_exempt">{{ t('onboarding.rpmStickyExempt') }}</option></select></label>
              <label v-if="settings.rpmStrategy === 'tiered'">{{ t('onboarding.rpmBuffer') }}<input v-model.number="settings.rpmStickyBuffer" data-testid="rpm-buffer" type="number" min="1" step="1" :placeholder="t('onboarding.rpmBufferAuto')" :disabled="busy" /></label>
            </div>
          </div>
          <div class="setting-card">
            <label class="setting-heading"><span>{{ t('onboarding.windowCost') }}</span><input v-model="settings.windowCostEnabled" data-testid="window-cost-enabled" type="checkbox" :disabled="busy" /></label>
            <div v-if="settings.windowCostEnabled" class="setting-grid">
              <label>{{ t('onboarding.windowCostLimit') }}<input v-model.number="settings.windowCostLimit" data-testid="window-cost-limit" type="number" min="0" step="any" :disabled="busy" /></label>
              <label>{{ t('onboarding.windowCostReserve') }}<input v-model.number="settings.windowCostStickyReserve" data-testid="window-cost-reserve" type="number" min="0" step="any" :disabled="busy" @blur="settings.windowCostStickyReserve = withDefault(settings.windowCostStickyReserve, 10)" /></label>
            </div>
          </div>
          <p v-if="!settingsValid" class="onboarding-warning" role="alert">{{ t('onboarding.invalidSettings') }}</p>
        </div>
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
.group-picker { position: relative; }
.group-picker-trigger { width: 100%; display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 9px 11px; border: 1px solid #d9e2e7; border-radius: 6px; background: #fff; color: #30444d; text-align: left; font-size: 12px; }
.group-picker-trigger:disabled { cursor: not-allowed; opacity: .65; }
.group-picker-menu { position: absolute; z-index: 2; top: calc(100% + 4px); left: 0; right: 0; max-height: 180px; overflow-y: auto; padding: 4px; border: 1px solid #d9e2e7; border-radius: 6px; background: #fff; box-shadow: 0 8px 20px #30444d26; }
.group-picker-option { width: 100%; display: flex; align-items: center; gap: 8px; padding: 8px; border: 0; border-radius: 4px; background: transparent; color: #30444d; text-align: left; font-size: 12px; }
.group-picker-option:hover, .group-picker-option[aria-selected="true"] { background: #e8f3f4; }
.group-picker-check { width: 14px; color: #087f8c; font-weight: 700; }
.advanced-settings { display: grid; gap: 10px; margin-top: 8px; padding: 10px; border: 1px solid #d9e2e7; border-radius: 7px; background: #f7fafb; }
.advanced-settings label:not(.setting-heading), .setting-grid label { display: grid; gap: 4px; color: #60727b; font-size: 12px; }
.advanced-settings input, .advanced-settings select { box-sizing: border-box; width: 100%; padding: 8px 10px; border: 1px solid #d9e2e7; border-radius: 6px; background: #fff; color: #30444d; }
.setting-card { padding: 9px; border: 1px solid #d9e2e7; border-radius: 6px; background: #fff; }
.advanced-settings .setting-heading input { width: 16px; height: 16px; padding: 0; accent-color: #087f8c; }
.setting-heading { display: flex; align-items: center; justify-content: space-between; color: #30444d; font-size: 12px; font-weight: 600; }
.setting-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 8px; margin-top: 8px; }
@media (max-width: 480px) { .setting-grid { grid-template-columns: 1fr; } }
</style>
