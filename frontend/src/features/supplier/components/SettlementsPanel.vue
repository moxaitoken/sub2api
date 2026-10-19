<template>
  <div class="space-y-4">
    <p class="text-sm text-gray-500 dark:text-dark-300">{{ t('supplier.settlements.hint') }}</p>
    <SettlementTable :items="items" :loading="loading" @view="view" />
    <Pagination
      v-if="total > pageSize"
      :page="page"
      :total="total"
      :page-size="pageSize"
      @update:page="(p: number) => { page = p; load() }"
    />
    <SettlementDetailDialog :settlement="detail" @close="detail = null" />
  </div>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import Pagination from '@/components/common/Pagination.vue'
import SettlementDetailDialog from './SettlementDetailDialog.vue'
import SettlementTable from './SettlementTable.vue'
import { getSettlement, listSettlements } from '../api'
import type { Settlement } from '../types'

const { t } = useI18n()
const appStore = useAppStore()

const items = ref<Settlement[]>([])
const detail = ref<Settlement | null>(null)
const loading = ref(false)
const page = ref(1)
const pageSize = 20
const total = ref(0)

async function load() {
  loading.value = true
  try {
    const result = await listSettlements(page.value, pageSize)
    items.value = result.items
    total.value = result.total
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function view(s: Settlement) {
  try {
    detail.value = await getSettlement(s.id)
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.loadFailed'))
  }
}

onMounted(load)
</script>
