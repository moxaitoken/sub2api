<template>
  <div class="table-container">
    <table class="table">
      <thead>
        <tr>
          <th>{{ t(`supplier.usage.columns.${labelKey}`) }}</th>
          <th v-if="secondaryKey">{{ t(`supplier.usage.columns.${secondaryKey}`) }}</th>
          <th class="!text-right">{{ t('supplier.usage.requests') }}</th>
          <th class="!text-right">{{ t('supplier.usage.inputTokens') }}</th>
          <th class="!text-right">{{ t('supplier.usage.outputTokens') }}</th>
          <th class="!text-right">{{ t('supplier.usage.cacheWrite') }}</th>
          <th class="!text-right">{{ t('supplier.usage.cacheRead') }}</th>
          <th class="!text-right">{{ t('supplier.usage.amount') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="!rows.length">
          <td :colspan="secondaryKey ? 8 : 7" class="text-center text-gray-400">{{ t('supplier.common.empty') }}</td>
        </tr>
        <tr v-for="(row, index) in rows" :key="index">
          <td class="font-medium">{{ label(row) }}</td>
          <td v-if="secondaryKey">{{ row[secondaryKey] }}</td>
          <td class="text-right tabular-nums">{{ formatNumberLocaleString(row.request_count) }}</td>
          <td class="text-right tabular-nums">{{ formatNumberLocaleString(row.input_tokens) }}</td>
          <td class="text-right tabular-nums">{{ formatNumberLocaleString(row.output_tokens) }}</td>
          <td class="text-right tabular-nums">{{ formatNumberLocaleString(row.cache_creation_tokens) }}</td>
          <td class="text-right tabular-nums">{{ formatNumberLocaleString(row.cache_read_tokens) }}</td>
          <td class="text-right tabular-nums">{{ formatAmount(row.amount) }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { formatNumberLocaleString } from '@/utils/format'
import { formatAmount } from '../format'
import type { UsageRow } from '../types'

type RowKey = 'family' | 'model' | 'account_name' | 'date'

const props = defineProps<{
  rows: UsageRow[]
  labelKey: RowKey
  secondaryKey?: RowKey
  labelFormatter?: (value: string) => string
}>()

const { t } = useI18n()

function label(row: UsageRow): string {
  const value = String(row[props.labelKey] ?? '')
  return props.labelFormatter ? props.labelFormatter(value) : value
}
</script>
