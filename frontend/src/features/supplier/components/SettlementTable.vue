<template>
  <div class="card table-container">
    <table class="table">
      <thead>
        <tr>
          <th v-if="showSupplier">{{ t('supplier.settlements.supplier') }}</th>
          <th>{{ t('supplier.settlements.period') }}</th>
          <th>{{ t('supplier.settlements.status') }}</th>
          <th class="!text-right">{{ t('supplier.usage.requests') }}</th>
          <th class="!text-right">{{ t('supplier.settlements.tokens') }}</th>
          <th class="!text-right">{{ t('supplier.usage.amount') }}</th>
          <th>{{ t('supplier.settlements.createdAt') }}</th>
          <th class="!text-right">{{ t('supplier.common.actions') }}</th>
        </tr>
      </thead>
      <tbody>
        <tr v-if="!items.length">
          <td :colspan="showSupplier ? 8 : 7" class="text-center text-gray-400">
            {{ loading ? t('supplier.common.loading') : t('supplier.common.empty') }}
          </td>
        </tr>
        <tr v-for="s in items" :key="s.id">
          <td v-if="showSupplier">{{ s.supplier_name }}</td>
          <td class="whitespace-nowrap">{{ formatDateOnly(s.period_start) }} ~ {{ formatDateOnly(new Date(new Date(s.period_end).getTime() - 1)) }}</td>
          <td><span :class="settlementBadgeClass(s.status)">{{ t(`supplier.settlementStatus.${s.status}`) }}</span></td>
          <td class="text-right tabular-nums">{{ formatNumberLocaleString(s.request_count) }}</td>
          <td class="text-right tabular-nums">{{ formatNumberLocaleString(totalTokens(s)) }}</td>
          <td class="text-right font-medium tabular-nums">{{ formatAmount(s.amount) }}</td>
          <td class="text-xs">{{ formatDateTime(s.created_at) }}</td>
          <td class="text-right whitespace-nowrap">
            <button class="btn btn-ghost btn-sm" @click="emit('view', s)">{{ t('supplier.common.view') }}</button>
            <slot name="actions" :settlement="s" />
          </td>
        </tr>
      </tbody>
    </table>
  </div>
</template>

<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { formatDateOnly, formatDateTime, formatNumberLocaleString } from '@/utils/format'
import { formatAmount, settlementBadgeClass, totalTokens } from '../format'
import type { Settlement } from '../types'

defineProps<{ items: Settlement[]; loading?: boolean; showSupplier?: boolean }>()
const emit = defineEmits<{ (e: 'view', settlement: Settlement): void }>()
const { t } = useI18n()
</script>
