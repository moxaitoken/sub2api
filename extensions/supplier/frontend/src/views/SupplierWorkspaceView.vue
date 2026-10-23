<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SupplierAccount } from '../api/types'
import PortalIcon from '../components/PortalIcon.vue'
import PortalDialog from '../components/PortalDialog.vue'
import UsageStatsView from './UsageStatsView.vue'
import type { SupplierClient } from '../api/client'
const props = defineProps<{ client?: SupplierClient; tab: string; accounts: SupplierAccount[]; total: number | null; page: number; pages: number; loading: boolean; updatedAt: string; failed: boolean }>()
const emit = defineEmits<{ refresh: [page?: number]; add: [] }>()
const { t, locale } = useI18n()
const search = ref(''), state = ref(''), supply = ref(''), overview = ref(true), columnsOpen = ref(false)
const details = ref<SupplierAccount | null>(null), scopeOpen = ref(false)
const columns = ['name', 'state', 'supply', 'quota', 'concurrency', 'weight', 'today', 'cumulative', 'observed', 'actions'] as const
const visible = ref<string[]>([...columns])
const filtered = computed(() => props.accounts.filter(account =>
  (!search.value.trim() || `${account.id} ${account.name}`.toLowerCase().includes(search.value.trim().toLowerCase())) &&
  (!state.value || account.admission_state === state.value) && (!supply.value || account.desired_state === supply.value)))
const filtersActive = computed(() => Boolean(search.value || state.value || supply.value))
const displayTime = computed(() => props.updatedAt ? new Date(props.updatedAt).toLocaleTimeString(locale.value === 'zh' ? 'zh-CN' : 'en-US', { hour: '2-digit', minute: '2-digit', second: '2-digit' }) : '—')
function reset() { search.value = ''; state.value = ''; supply.value = '' }
function observed(value: string | null) { return value && Number.isFinite(Date.parse(value)) ? new Date(value).toLocaleString(locale.value === 'zh' ? 'zh-CN' : 'en-US') : '—' }
watch(() => props.tab, () => { details.value = null; columnsOpen.value = false; scopeOpen.value = false })
</script>
<template>
  <UsageStatsView v-if="tab === 'usage' && client" :client="client" />
  <div v-else class="supplier-workspace" :class="`workspace-${tab}`">
    <div class="workspace-heading">
      <div><div v-if="tab === 'usage'" class="eyebrow">SUPPLIER WORKSPACE</div><h1>{{ t(tab === 'accounts' ? 'accounts.title' : `portal.${tab}`) }}</h1><p>{{ t(`workspace.${tab}Description`) }}</p></div>
      <div class="heading-actions">
        <button v-if="tab === 'accounts'" :disabled="loading" @click="emit('refresh', page)"><PortalIcon name="refresh" :size="15" />{{ t('app.refresh') }}</button>
        <button v-if="tab === 'accounts'" class="primary" :disabled="loading" @click="emit('add')"><PortalIcon name="plus" :size="15" />{{ t(tab === 'accounts' ? 'workspace.addAccount' : 'workspace.addProxy') }}</button>
      </div>
    </div>

    <template v-if="tab === 'accounts'">
      <section class="account-overview" :aria-label="t('workspace.overview')">
        <div class="overview-heading"><b>{{ t('workspace.overview') }}</b><button class="text-button icon-button" :aria-label="t('workspace.scope')" @click="scopeOpen = true"><PortalIcon name="info" :size="14" /></button><span class="overview-updated">{{ t('workspace.updated') }} {{ displayTime }}</span><button class="text-button" :aria-expanded="overview" @click="overview = !overview">{{ t(overview ? 'workspace.collapse' : 'workspace.expand') }} <span aria-hidden="true">⌄</span></button></div>
        <template v-if="overview">
          <div class="overview-metrics"><div class="account-count"><small>{{ t('workspace.totalAccounts') }}</small><strong>{{ total === null || failed ? '—' : total }}</strong><small>{{ t('workspace.ownScope') }}</small></div>
            <div v-for="metric in ['rpm','tpm','uncachedTpm','concurrency','highQuota']" :key="metric" class="overview-metric"><small>{{ t(`workspace.${metric}`) }}</small><strong>—</strong><small>{{ t('workspace.awaitingMetrics') }}</small></div>
          </div>
          <div class="quota-windows"><div v-for="window in ['fiveHour','sevenDay','fable']" :key="window"><b>{{ t(`workspace.${window}`) }}</b><span class="muted"> · {{ t('workspace.quotaWindow') }}</span><div class="quota-track"></div><small>{{ t('workspace.awaitingQuota') }}</small></div></div>
        </template>
      </section>
      <section class="account-list" :aria-label="t('accounts.title')" :aria-busy="loading">
        <div class="account-filters">
          <label class="search-field"><PortalIcon name="search" :size="15" /><input v-model="search" :placeholder="t('workspace.searchAccounts')" :aria-label="t('workspace.searchAccounts')" maxlength="100" /></label>
          <label class="filter-select">{{ t('accounts.state') }}<select v-model="state" :aria-label="t('accounts.state')"><option value="">{{ t('workspace.all') }}</option><option v-for="value in ['provisioning','identity_pending','pending','approved','rejected','blocked','deleting','deleted','needs_attention']" :key="value" :value="value">{{ t(`accounts.states.${value}`) }}</option></select></label>
          <label class="filter-select">{{ t('accounts.supply') }}<select v-model="supply" :aria-label="t('accounts.supply')"><option value="">{{ t('workspace.all') }}</option><option value="running">{{ t('accounts.supplyStates.running') }}</option><option value="paused">{{ t('accounts.supplyStates.paused') }}</option></select></label>
          <button :aria-expanded="columnsOpen" @click="columnsOpen = !columnsOpen"><PortalIcon name="columns" :size="14" />{{ t('workspace.columns') }}</button><button @click="reset">{{ t('portal.reset') }}</button>
        </div>
        <div v-if="columnsOpen" class="column-options"><label v-for="column in columns" :key="column"><input v-model="visible" type="checkbox" :value="column" :disabled="['name','actions'].includes(column)" />{{ t(`workspace.columnsMap.${column}`) }}</label></div>
        <div class="table-context"><PortalIcon name="filter" :size="13" /><span>{{ t('workspace.pageFilters') }}</span><span class="push">Claude · OAuth</span></div>
        <div class="workspace-table-scroll"><table class="account-table"><thead><tr><th v-for="column in columns.filter(c => visible.includes(c))" :key="column" scope="col">{{ t(`workspace.columnsMap.${column}`) }}</th></tr></thead><tbody>
          <tr v-for="account in filtered" :key="account.id">
            <td v-if="visible.includes('name')"><button class="account-name text-button" @click="details = account">{{ account.name }}</button><small class="account-id">#{{ account.id }}</small></td>
            <td v-if="visible.includes('state')"><span class="status-badge" :class="account.admission_state === 'approved' ? 'green' : ['blocked','rejected','needs_attention'].includes(account.admission_state) ? 'red' : 'gray'">{{ t(`accounts.states.${account.admission_state}`) }}</span></td>
            <td v-if="visible.includes('supply')"><span class="supply-dot" :class="{ running: account.desired_state === 'running' }"></span>{{ t(`accounts.supplyStates.${account.desired_state}`) }}</td>
            <td v-if="visible.includes('quota')"><div class="row-quota"><span>5h <i></i> —</span><span>7d <i></i> —</span><span>Fable <i></i> —</span></div></td>
            <td v-for="column in ['concurrency','weight','today','cumulative'].filter(c => visible.includes(c))" :key="column"><span :title="t('workspace.notReported')">—</span></td>
            <td v-if="visible.includes('observed')">{{ observed(account.observed_at) }}</td>
            <td v-if="visible.includes('actions')"><button class="icon-button text-button" :aria-label="`${t('workspace.accountDetails')} ${account.name}`" @click="details = account"><PortalIcon name="info" :size="16" /></button></td>
          </tr>
          <tr v-if="loading || failed || !filtered.length"><td :colspan="visible.length" class="table-empty"><PortalIcon name="empty" :size="32" /><b>{{ t(loading ? 'workspace.loadingAccounts' : failed ? 'workspace.accountsFailed' : filtersActive ? 'workspace.noMatches' : 'portal.noAccounts') }}</b><span>{{ t(failed ? 'workspace.retryAccounts' : 'workspace.accountEmptyHint') }}</span></td></tr>
        </tbody></table></div>
        <div class="workspace-pagination"><span>{{ t('workspace.records', { count: total === null || failed ? '—' : total }) }}</span><span v-if="filtersActive">{{ t('workspace.pageMatches', { count: filtered.length }) }}</span><span class="push">{{ t('workspace.pageOf', { page, pages: Math.max(1,pages) }) }}</span><button :disabled="loading || failed || page <= 1" :aria-label="t('accounts.previous')" @click="emit('refresh', page - 1)">‹</button><button :disabled="loading || failed || page >= pages" :aria-label="t('accounts.next')" @click="emit('refresh', page + 1)">›</button></div>
      </section>
      <p class="workspace-caption"><PortalIcon name="shield" :size="13" />{{ t('workspace.accountReadBoundary') }}</p>
    </template>

    <template v-else-if="tab === 'proxies'">
      <section class="proxy-panel"><div class="proxy-toolbar"><div v-for="key in ['validProxies','invalidProxies','totalProxies']" :key="key" class="proxy-metric"><small>{{ t(`workspace.${key}`) }}</small><b>—</b></div><div class="push heading-actions"><button disabled :title="t('workspace.proxyUnavailable')">{{ t('workspace.testProxies') }}</button><button disabled :title="t('workspace.proxyUnavailable')">{{ t('workspace.import') }}</button></div></div><div class="workspace-table-scroll"><table><thead><tr><th v-for="key in ['proxyName','proxyAddress','region','proxyStatus','latency','boundAccounts','actions']" :key="key">{{ t(`workspace.columnsMap.${key}`) }}</th></tr></thead><tbody><tr><td colspan="7" class="table-empty"><PortalIcon name="proxies" :size="32" /><b>{{ t('workspace.proxyUnavailable') }}</b><span>{{ t('workspace.proxyBoundary') }}</span></td></tr></tbody></table></div><div class="workspace-pagination"><span>{{ t('workspace.dataUnavailable') }}</span></div></section>
    </template>

    <template v-else-if="tab === 'usage'">
      <div class="usage-filters"><label>{{ t('workspace.account') }}<select disabled><option>{{ t('workspace.allAccounts') }}</option></select></label><label>{{ t('workspace.timeRange') }}<select disabled><option>{{ t('workspace.today') }}</option></select></label><span>{{ t('workspace.usageUnavailable') }}</span></div>
      <div class="usage-cards"><div v-for="key in ['cost','requests','tokens','cache']" :key="key" class="usage-card"><span>{{ t(`workspace.${key}`) }}</span><strong>—</strong><small>{{ t(key === 'cost' ? 'workspace.costScope' : 'workspace.dataUnavailable') }}</small></div></div>
      <section class="usage-panel"><div class="panel-heading"><h2>{{ t('workspace.dailyTrend') }}</h2><small>{{ t('workspace.usageUnavailable') }}</small></div><div class="workspace-table-scroll"><table><thead><tr><th v-for="key in ['date','requests','tokens','cache','cost']" :key="key">{{ t(`workspace.${key}`) }}</th></tr></thead><tbody><tr><td colspan="5" class="table-empty"><PortalIcon name="usage" :size="32" /><b>{{ t('workspace.usageUnavailable') }}</b><span>{{ t('workspace.usageBoundary') }}</span></td></tr></tbody></table></div></section>
      <section class="usage-panel"><div class="panel-heading"><h2>{{ t('workspace.byAccount') }}</h2><small>{{ t('workspace.ownScope') }}</small></div><div class="workspace-table-scroll"><table><thead><tr><th v-for="key in ['account','requests','inputOutput','cacheReadWrite','cacheHit','cost']" :key="key">{{ t(`workspace.${key}`) }}</th></tr></thead><tbody><tr><td colspan="6" class="table-empty compact">{{ t('workspace.dataUnavailable') }}</td></tr></tbody></table></div></section>
    </template>

    <PortalDialog v-if="scopeOpen" :title="t('workspace.scope')" @close="scopeOpen = false"><p>{{ t('workspace.scopeExplanation') }}</p></PortalDialog>
    <PortalDialog v-if="details" :title="t('workspace.accountDetails')" @close="details = null"><dl class="account-details"><dt>{{ t('accounts.name') }}</dt><dd>{{ details.name }}</dd><dt>ID</dt><dd>{{ details.id }}</dd><dt>{{ t('accounts.state') }}</dt><dd>{{ t(`accounts.states.${details.admission_state}`) }}</dd><dt>{{ t('accounts.supply') }}</dt><dd>{{ t(`accounts.supplyStates.${details.desired_state}`) }}</dd><dt>{{ t('workspace.columnsMap.observed') }}</dt><dd>{{ observed(details.observed_at) }}</dd></dl><p class="muted">{{ t('workspace.notReported') }}</p></PortalDialog>
  </div>
</template>
