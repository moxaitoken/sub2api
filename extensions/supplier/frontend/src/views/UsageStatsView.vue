<script setup lang="ts">
import { onMounted, onBeforeUnmount, ref } from 'vue'
import { useSupplierI18n } from '../i18n/useSupplierI18n'
import type { SupplierClient } from '../api/client'
import type { UsageAccount, UsagePeriod, UsageReport } from '../api/types'
import RequestError from '../components/RequestError.vue'
import PortalIcon from '../components/PortalIcon.vue'

const props = defineProps<{ client: SupplierClient }>()
const { t, locale } = useSupplierI18n()
const period = ref<UsagePeriod>('today'), account = ref(''), page = ref(1)
const report = ref<UsageReport | null>(null), options = ref<UsageAccount[]>([])
const busy = ref(false), error = ref<unknown>(null)
let generation = 0
async function load(next = 1) {
  const token = ++generation
  busy.value = true; error.value = null; report.value = null; page.value = next
  try {
    const result = await props.client.usage(period.value, account.value, next)
    if (token !== generation) return
    report.value = result; options.value = result.account_options
  } catch (e) { if (token === generation) error.value = e }
  finally { if (token === generation) busy.value = false }
}
const count = (s: string) => BigInt(s).toLocaleString(locale.value)
const cost = (s: string) => s.includes('.') ? s.replace(/0+$/, '').replace(/\.$/, '') : s
function hit(read: string, input: string, write: string): string {
  const total = BigInt(read) + BigInt(input) + BigInt(write)
  if (total === 0n) return '—'
  const p = (BigInt(read) * 10000n / total).toString().padStart(3, '0')
  return p.slice(0, -2) + '.' + p.slice(-2) + '%'
}
const observed = (s: string) => new Intl.DateTimeFormat(locale.value, { timeZone: 'Asia/Shanghai', dateStyle: 'short', timeStyle: 'medium' }).format(new Date(s))
function bar(value: string) {
  const max = Math.max(...(report.value?.daily.map(d => Number(d.total_cost_usd)) || []))
  return max > 0 ? (Number(value) / max * 100) + '%' : '0%'
}
onMounted(() => load())
onBeforeUnmount(() => { generation++ })
</script>

<template>
  <div class="usage-workspace" :aria-busy="busy">
    <div class="workspace-heading"><div><h1>{{ t('portal.usage') }}</h1><p>{{ t('workspace.usageDescription') }}</p></div><button :disabled="busy" @click="load(page)"><PortalIcon name="refresh" />{{ t('app.refresh') }}</button></div>
    <div class="usage-filters">
      <label>{{ t('workspace.account') }}<select v-model="account" aria-label="account" @change="load()"><option value="">{{ t('workspace.allAccounts') }}</option><option v-for="a in options" :key="a.id" :value="a.id">{{ a.name }}{{ a.archived ? ` (${t('usage.archived')})` : '' }}</option></select></label>
      <label>{{ t('workspace.timeRange') }}<select v-model="period" aria-label="period" @change="load()"><option value="all">{{ t('usage.allHistory') }}</option><option value="today">{{ t('workspace.today') }}</option><option value="7d">{{ t('usage.sevenDays') }}</option><option value="30d">{{ t('usage.thirtyDays') }}</option></select></label>
      <span>Asia/Shanghai</span>
    </div>
    <RequestError :error="error" />
    <p v-if="busy" role="status">{{ t('app.loading') }}</p>
    <template v-if="report">
      <p v-if="report.status === 'partial'" class="usage-warning" role="status">{{ t('usage.partial') }}</p>
      <p class="usage-note">{{ t('usage.observed') }} {{ observed(report.observed_at) }} · {{ t('usage.costScope') }}</p>
      <div class="usage-cards">
        <div class="usage-card"><span>{{ t('workspace.cost') }}</span><strong data-testid="usage-cost">${{ cost(report.summary.total_cost_usd) }}</strong><small>USD</small></div>
        <div class="usage-card"><span>{{ t('usage.recordedRequests') }}</span><strong>{{ count(report.summary.record_count) }}</strong><small>{{ t('usage.recordScope') }}</small></div>
        <div class="usage-card"><span>{{ t('workspace.tokens') }}</span><strong>{{ count(report.summary.tokens) }}</strong><small>{{ t('usage.inputOutput') }} {{ count(report.summary.input_tokens) }} / {{ count(report.summary.output_tokens) }}</small></div>
        <div class="usage-card"><span>{{ t('workspace.cache') }}</span><strong>{{ count(report.summary.cache_tokens) }}</strong><small>{{ t('usage.readWrite') }} {{ count(report.summary.cache_read_tokens) }} / {{ count(report.summary.cache_creation_tokens) }}</small></div>
      </div>
      <section class="usage-panel"><div class="panel-heading"><h2>{{ t('workspace.dailyTrend') }}</h2><small>{{ t('usage.recordScope') }}</small></div><div class="workspace-table-scroll"><table><thead><tr><th>{{ t('workspace.date') }}</th><th>{{ t('usage.records') }}</th><th>{{ t('workspace.tokens') }}</th><th>{{ t('workspace.cache') }}</th><th>{{ t('workspace.cost') }} USD</th><th>{{ t('usage.trend') }}</th></tr></thead><tbody><tr v-for="d in report.daily" :key="d.date"><td>{{ d.date }}</td><td>{{ count(d.record_count) }}</td><td>{{ count(d.tokens) }}</td><td>{{ count(d.cache_tokens) }}</td><td>${{ cost(d.total_cost_usd) }}</td><td class="usage-bar-cell"><span class="usage-bar" :style="{ width: bar(d.total_cost_usd) }"></span></td></tr></tbody></table></div></section>
      <section class="usage-panel"><div class="panel-heading"><h2>{{ t('workspace.byAccount') }}</h2><small>{{ t('workspace.ownScope') }}</small></div><div class="workspace-table-scroll"><table><thead><tr><th>{{ t('workspace.account') }}</th><th>{{ t('usage.records') }}</th><th>{{ t('workspace.inputOutput') }}</th><th>{{ t('workspace.cacheReadWrite') }}</th><th>{{ t('workspace.cacheHit') }}</th><th>{{ t('workspace.cost') }} USD</th><th>{{ t('usage.sync') }}</th></tr></thead><tbody><tr v-for="a in report.accounts" :key="a.id"><td>{{ a.name }}<small v-if="a.archived"> ({{ t('usage.archived') }})</small></td><td>{{ count(a.record_count) }}</td><td>{{ count(a.input_tokens) }} / {{ count(a.output_tokens) }}</td><td>{{ count(a.cache_read_tokens) }} / {{ count(a.cache_creation_tokens) }}</td><td>{{ hit(a.cache_read_tokens, a.input_tokens, a.cache_creation_tokens) }}</td><td>${{ cost(a.total_cost_usd) }}</td><td :title="a.synced_at ? observed(a.synced_at) : ''">{{ t(`usage.${a.sync_state}`) }}</td></tr><tr v-if="!report.accounts.length"><td colspan="7" class="table-empty compact">{{ t('usage.empty') }}</td></tr></tbody></table></div>
        <div class="workspace-pagination"><span>{{ t('usage.totalAccounts', { total: report.total }) }}</span><span>{{ page }} / {{ Math.max(1, report.pages) }}</span><button :disabled="busy || page <= 1" @click="load(page - 1)">{{ t('accounts.previous') }}</button><button :disabled="busy || page >= report.pages" @click="load(page + 1)">{{ t('accounts.next') }}</button></div>
      </section>
    </template>
  </div>
</template>

<style scoped>
.usage-warning{background:#fff7df;border:1px solid #ead28b;border-radius:8px;padding:12px 16px;color:#76581e;font-size:13px}.usage-note{font-size:12px;color:#71838b;margin-bottom:18px}.usage-bar-cell{min-width:100px}.usage-bar{display:block;background:#138e9c;height:8px;border-radius:4px}.usage-card strong{overflow-wrap:anywhere;font-size:24px}.usage-workspace table{width:100%}.usage-workspace select{max-width:280px}.usage-workspace td{white-space:nowrap}
</style>
