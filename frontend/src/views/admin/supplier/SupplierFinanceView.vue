<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { ref, onMounted, onUnmounted, computed } from 'vue'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/layout/AppLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { supplierFinanceAPI, supplierAdminAPI, type FinanceReport, type Settlement, type ManagedSupplier } from '@/api/admin/suppliers'
const { t } = useI18n()
const route = useRoute()
const supplier = ref(typeof route.query.supplier_id === 'string' ? route.query.supplier_id : ''), search = ref(''), ownerSearch = ref('')
const owners = ref<ManagedSupplier[]>([]), report = ref<FinanceReport | null>(null), error = ref(''), busy = ref(false), detail = ref<Settlement | null>(null)
let generation = 0, ownerGeneration = 0
const columns = computed(() => ['ledger_id','supplier_name','type','amount_usd','before_usd','after_usd','id','payment_method','created_at','actor_id','actions'].map(key => ({key,label:t(`supplierAdmin.ledgerColumns.${key}`)})))
async function load(page = 1) {
  const current = ++generation; busy.value = true; error.value = ''; detail.value = null
  try { const data = await supplierFinanceAPI.ledger(supplier.value, search.value.trim(), page); if (current === generation) report.value = data }
  catch { if (current === generation) { report.value = null; error.value = t('supplierAdmin.failed') } }
  finally { if (current === generation) busy.value = false }
}
async function findOwners() {
  const current = ++ownerGeneration
  try { const data = await supplierAdminAPI.list(ownerSearch.value.trim()); if (current === ownerGeneration) owners.value = data.items }
  catch { if (current === ownerGeneration) error.value = t('supplierAdmin.failed') }
}
async function showDetail(row: Settlement) {
  const current = generation
  try { const data = await supplierFinanceAPI.detail(row.id); if (current === generation) detail.value = data }
  catch { error.value = t('supplierAdmin.failed') }
}
onMounted(() => { void load(); void findOwners() })
onUnmounted(() => { generation++; ownerGeneration++ })
</script>

<template>
  <AppLayout>
    <p v-if="error" role="alert" class="mb-4 text-red-500">{{ error }}</p>
    <section class="mb-5 grid gap-4 sm:grid-cols-2 xl:grid-cols-4">
      <div class="card p-5"><p>{{ t('supplierAdmin.unsettled') }}</p><strong class="text-2xl">{{ report?.overview.unsettled_usd ?? '—' }}</strong></div>
      <div class="card p-5"><p>{{ t('supplierAdmin.settled') }}</p><strong class="text-2xl">{{ report?.overview.settled_usd ?? '—' }}</strong></div>
      <div class="card p-5"><p>{{ t('supplierAdmin.settlementCount') }}</p><strong class="text-2xl">{{ report?.overview.settlement_count ?? '—' }}</strong></div>
      <div class="card p-5"><p>{{ t('supplierAdmin.accrual') }}</p><strong>{{ t('supplierAdmin.accrualPending') }}</strong></div>
    </section>
    <section class="card p-5">
      <form class="mb-4 flex flex-wrap items-end gap-3" @submit.prevent="load(1)">
        <label class="min-w-[240px]">{{ t('supplierAdmin.findSupplier') }}<div class="flex gap-2"><input v-model="ownerSearch" class="input" maxlength="100" /><button type="button" class="btn btn-secondary shrink-0" @click="findOwners">{{ t('common.search') }}</button></div></label>
        <label>{{ t('supplierAdmin.supplier') }}<select v-model="supplier" class="input" @change="load(1)"><option value="">{{ t('supplierAdmin.allSuppliers') }}</option><option v-if="supplier && !owners.some(row => row.id === supplier)" :value="supplier">{{ supplier }}</option><option v-for="row in owners" :key="row.id" :value="row.id">{{ row.name }} · {{ row.login_account }}</option></select></label>
        <input v-model="search" class="input w-full sm:w-72" :aria-label="t('supplierAdmin.ledgerSearch')" :placeholder="t('supplierAdmin.ledgerSearch')" maxlength="200" />
        <button class="btn btn-primary" :disabled="busy">{{ t('common.search') }}</button><button type="button" class="btn btn-secondary" :disabled="busy" @click="search = ''; supplier = ''; load(1)">{{ t('common.reset') }}</button>
      </form>
      <p class="mb-4 text-sm text-gray-500">{{ t('supplierAdmin.financeNote') }}</p>
      <DataTable :columns="columns" :data="report?.items || []" :loading="busy" :sticky-actions-column="false" row-key="id">
        <template #cell-type>{{ t('supplierAdmin.settlementType') }}</template><template #cell-amount_usd="{row}"><span class="text-red-500">−{{ row.amount_usd }}</span></template>
        <template #cell-payment_method="{row}">{{ t(`supplierAdmin.methods.${row.payment_method}`) }}</template><template #cell-created_at="{row}">{{ new Date(row.created_at).toLocaleString() }}</template>
        <template #cell-actions="{row}"><button class="btn btn-secondary btn-sm" @click="showDetail(row)">{{ t('supplierAdmin.ledgerDetail') }}</button></template>
      </DataTable>
      <Pagination v-if="report && report.total > 0" :page="report.page" :total="report.total" :page-size="20" :show-page-size-selector="false" @update:page="load" />
    </section>
    <BaseDialog :show="!!detail" :title="t('supplierAdmin.ledgerDetail')" @close="detail = null"><dl v-if="detail" class="grid grid-cols-[auto_1fr] gap-3 break-all">
      <template v-for="key in ['ledger_id','supplier_name','id','amount_usd','before_usd','after_usd','actor_id','created_at'] as const" :key="key"><dt>{{ t(`supplierAdmin.ledgerColumns.${key}`) }}</dt><dd>{{ detail[key] }}</dd></template>
      <dt>{{ t('supplierAdmin.paymentMethod') }}</dt><dd>{{ t(`supplierAdmin.methods.${detail.payment_method}`) }}</dd><dt>{{ t('supplierAdmin.reference') }}</dt><dd>{{ detail.reference || '—' }}</dd><dt>{{ t('supplierAdmin.remark') }}</dt><dd class="whitespace-pre-wrap">{{ detail.remark || '—' }}</dd>
    </dl></BaseDialog>
  </AppLayout>
</template>
