<template>
  <div class="space-y-6">
    <div class="flex flex-wrap items-end gap-3">
      <label class="block">
        <span class="input-label">{{ t('supplier.usage.start') }}</span>
        <input v-model="range.start" type="date" class="input" />
      </label>
      <label class="block">
        <span class="input-label">{{ t('supplier.usage.end') }}</span>
        <input v-model="range.end" type="date" class="input" />
      </label>
      <label v-if="accounts.length" class="block">
        <span class="input-label">{{ t('supplier.usage.account') }}</span>
        <select v-model.number="accountId" class="input">
          <option :value="0">{{ t('supplier.usage.allAccounts') }}</option>
          <option v-for="acc in accounts" :key="acc.id" :value="acc.id">{{ acc.name }}</option>
        </select>
      </label>
      <button class="btn btn-secondary" :disabled="loading" @click="load">
        <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
      </button>
    </div>

    <div v-if="summary" class="grid grid-cols-2 gap-4 lg:grid-cols-5">
      <div v-for="card in cards" :key="card.key" class="card p-4">
        <p class="text-xs text-gray-500 dark:text-dark-300">{{ card.label }}</p>
        <p class="mt-1 text-xl font-semibold text-gray-900 dark:text-white">{{ card.value }}</p>
      </div>
    </div>

    <section class="card">
      <div class="card-header font-medium">{{ t('supplier.usage.byFamily') }}</div>
      <UsageTable :rows="summary?.by_family ?? []" label-key="family" :label-formatter="familyLabel" />
    </section>

    <section class="card">
      <div class="card-header font-medium">{{ t('supplier.usage.byModel') }}</div>
      <UsageTable :rows="summary?.by_model ?? []" label-key="model" />
    </section>

    <section v-if="!accountId" class="card">
      <div class="card-header font-medium">{{ t('supplier.usage.byAccount') }}</div>
      <UsageTable :rows="byAccount" label-key="account_name" />
    </section>

    <section class="card">
      <div class="card-header font-medium">{{ t('supplier.usage.daily') }}</div>
      <UsageTable :rows="daily" label-key="date" secondary-key="model" />
    </section>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { formatNumber } from '@/utils/format'
import Icon from '@/components/icons/Icon.vue'
import UsageTable from './UsageTable.vue'
import { getUsageByAccount, getUsageDaily, getUsageSummary, listAccounts } from '../api'
import { defaultRange, formatAmount } from '../format'
import type { SupplierAccount, UsageParams, UsageRow, UsageSummary } from '../types'

const { t } = useI18n()
const appStore = useAppStore()

const range = reactive(defaultRange())
const accountId = ref(0)
const accounts = ref<SupplierAccount[]>([])
const summary = ref<UsageSummary | null>(null)
const daily = ref<UsageRow[]>([])
const byAccount = ref<UsageRow[]>([])
const loading = ref(false)

const familyLabel = (value: string) => t(`supplier.family.${value}`)

const cards = computed(() => {
  const totals = summary.value?.totals
  if (!totals) return []
  return [
    { key: 'requests', label: t('supplier.usage.requests'), value: formatNumber(totals.request_count) },
    { key: 'input', label: t('supplier.usage.inputTokens'), value: formatNumber(totals.input_tokens) },
    { key: 'output', label: t('supplier.usage.outputTokens'), value: formatNumber(totals.output_tokens) },
    {
      key: 'cache',
      label: t('supplier.usage.cacheTokens'),
      value: formatNumber(totals.cache_creation_tokens + totals.cache_read_tokens),
    },
    { key: 'amount', label: t('supplier.usage.amount'), value: formatAmount(totals.amount) },
  ]
})

async function load() {
  const params: UsageParams = { start: range.start, end: range.end }
  if (accountId.value) params.account_id = accountId.value
  loading.value = true
  try {
    const [s, d, a] = await Promise.all([
      getUsageSummary(params),
      getUsageDaily(params),
      accountId.value ? Promise.resolve([]) : getUsageByAccount(params),
    ])
    summary.value = s
    daily.value = d
    byAccount.value = a
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.loadFailed'))
  } finally {
    loading.value = false
  }
}

watch(accountId, load)

onMounted(async () => {
  try {
    accounts.value = await listAccounts()
  } catch {
    accounts.value = []
  }
  await load()
})
</script>
