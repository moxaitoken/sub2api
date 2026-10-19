<template>
  <BaseDialog :show="!!settlement" :title="title" width="extra-wide" @close="emit('close')">
    <div v-if="settlement" class="space-y-4">
      <div class="grid grid-cols-2 gap-3 text-sm sm:grid-cols-4">
        <div>
          <p class="text-gray-500">{{ t('supplier.settlements.status') }}</p>
          <span :class="settlementBadgeClass(settlement.status)">{{ t(`supplier.settlementStatus.${settlement.status}`) }}</span>
        </div>
        <div>
          <p class="text-gray-500">{{ t('supplier.usage.requests') }}</p>
          <p class="font-medium">{{ formatNumberLocaleString(settlement.request_count) }}</p>
        </div>
        <div>
          <p class="text-gray-500">{{ t('supplier.settlements.tokens') }}</p>
          <p class="font-medium">{{ formatNumberLocaleString(totalTokens(settlement)) }}</p>
        </div>
        <div>
          <p class="text-gray-500">{{ t('supplier.usage.amount') }}</p>
          <p class="text-lg font-semibold">{{ formatAmount(settlement.amount) }}</p>
        </div>
      </div>
      <p v-if="settlement.notes" class="text-sm text-gray-600 dark:text-dark-300">{{ settlement.notes }}</p>

      <div class="table-container">
        <table class="table">
          <thead>
            <tr>
              <th>{{ t('supplier.usage.columns.account_name') }}</th>
              <th>{{ t('supplier.usage.columns.model') }}</th>
              <th class="!text-right">{{ t('supplier.usage.requests') }}</th>
              <th class="!text-right">{{ t('supplier.usage.inputTokens') }}</th>
              <th class="!text-right">{{ t('supplier.usage.outputTokens') }}</th>
              <th class="!text-right">{{ t('supplier.usage.cacheWrite') }}</th>
              <th class="!text-right">{{ t('supplier.usage.cacheRead') }}</th>
              <th class="!text-right">{{ t('supplier.usage.amount') }}</th>
            </tr>
          </thead>
          <tbody>
            <tr v-if="!settlement.items?.length">
              <td colspan="8" class="text-center text-gray-400">{{ t('supplier.common.empty') }}</td>
            </tr>
            <tr v-for="item in settlement.items ?? []" :key="`${item.account_id}-${item.model}`">
              <td>{{ item.account_name || `#${item.account_id}` }}</td>
              <td>{{ item.model }} <span class="text-xs text-gray-400">{{ t(`supplier.family.${item.family}`) }}</span></td>
              <td class="text-right tabular-nums">{{ formatNumberLocaleString(item.request_count) }}</td>
              <td class="text-right tabular-nums">{{ formatNumberLocaleString(item.input_tokens) }}</td>
              <td class="text-right tabular-nums">{{ formatNumberLocaleString(item.output_tokens) }}</td>
              <td class="text-right tabular-nums">{{ formatNumberLocaleString(item.cache_creation_tokens) }}</td>
              <td class="text-right tabular-nums">{{ formatNumberLocaleString(item.cache_read_tokens) }}</td>
              <td class="text-right tabular-nums">{{ formatAmount(item.amount) }}</td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
    <template #footer>
      <slot name="actions" />
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import { formatDateOnly, formatNumberLocaleString } from '@/utils/format'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { formatAmount, settlementBadgeClass, totalTokens } from '../format'
import type { Settlement } from '../types'

const props = defineProps<{ settlement: Settlement | null }>()
const emit = defineEmits<{ (e: 'close'): void }>()
const { t } = useI18n()

const title = computed(() => {
  const s = props.settlement
  if (!s) return ''
  return t('supplier.settlements.detailTitle', {
    supplier: s.supplier_name ?? '',
    start: formatDateOnly(s.period_start),
    end: formatDateOnly(new Date(new Date(s.period_end).getTime() - 1)),
  })
})
</script>
