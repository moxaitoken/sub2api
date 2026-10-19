<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center gap-3">
      <select v-model.number="filterSupplier" class="input w-48" @change="reload">
        <option :value="0">{{ t('supplier.admin.allSuppliers') }}</option>
        <option v-for="s in suppliers" :key="s.id" :value="s.id">{{ s.name }}</option>
      </select>
      <select v-model="filterStatus" class="input w-36" @change="reload">
        <option value="">{{ t('supplier.admin.allStatuses') }}</option>
        <option v-for="st in statuses" :key="st" :value="st">{{ t(`supplier.settlementStatus.${st}`) }}</option>
      </select>
      <div class="flex flex-1 justify-end gap-2">
        <button class="btn btn-secondary" :disabled="loading" @click="load">
          <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
        </button>
        <button class="btn btn-primary" @click="openCreate">
          <Icon name="plus" size="md" class="mr-1" />{{ t('supplier.admin.createSettlement') }}
        </button>
      </div>
    </div>

    <SettlementTable :items="items" :loading="loading" show-supplier @view="view">
      <template #actions="{ settlement }">
        <button v-if="settlement.status === 'draft'" class="btn btn-ghost btn-sm" @click="act(settlement, 'confirm')">{{ t('supplier.admin.confirm') }}</button>
        <button v-if="settlement.status === 'confirmed'" class="btn btn-ghost btn-sm text-green-600" @click="act(settlement, 'mark-paid')">{{ t('supplier.admin.markPaid') }}</button>
        <button v-if="settlement.status === 'draft' || settlement.status === 'confirmed'" class="btn btn-ghost btn-sm text-red-600" @click="act(settlement, 'void')">{{ t('supplier.admin.void') }}</button>
      </template>
    </SettlementTable>
    <Pagination v-if="total > pageSize" :page="page" :total="total" :page-size="pageSize" @update:page="(p: number) => { page = p; load() }" />

    <BaseDialog :show="createOpen" :title="t('supplier.admin.createSettlement')" width="extra-wide" @close="createOpen = false">
      <div class="space-y-4">
        <div class="grid gap-3 sm:grid-cols-3">
          <div>
            <label class="input-label">{{ t('supplier.settlements.supplier') }}</label>
            <select v-model.number="createForm.supplierId" class="input">
              <option :value="0" disabled>-</option>
              <option v-for="s in suppliers" :key="s.id" :value="s.id">{{ s.name }}</option>
            </select>
          </div>
          <div>
            <label class="input-label">{{ t('supplier.usage.start') }}</label>
            <input v-model="createForm.start" type="date" class="input" />
          </div>
          <div>
            <label class="input-label">{{ t('supplier.usage.end') }}</label>
            <input v-model="createForm.end" type="date" class="input" />
          </div>
        </div>
        <p class="text-xs text-gray-500">{{ t('supplier.admin.periodHint') }}</p>
        <div>
          <label class="input-label">{{ t('supplier.admin.notes') }}</label>
          <input v-model="createForm.notes" type="text" class="input" />
        </div>
        <div v-if="preview" class="rounded-lg bg-gray-50 p-3 text-sm dark:bg-dark-700">
          {{ t('supplier.admin.previewSummary', { requests: formatNumber(preview.request_count), lines: preview.items?.length ?? 0 }) }}
          <b class="ml-2">{{ formatAmount(preview.amount) }}</b>
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end gap-2">
          <button class="btn btn-secondary" @click="createOpen = false">{{ t('common.cancel') }}</button>
          <button class="btn btn-secondary" :disabled="saving || !createValid" @click="runPreview">{{ t('supplier.admin.preview') }}</button>
          <button class="btn btn-primary" :disabled="saving || !createValid" @click="create">{{ t('supplier.admin.createDraft') }}</button>
        </div>
      </template>
    </BaseDialog>

    <SettlementDetailDialog :settlement="detail" @close="detail = null" />

    <ConfirmDialog
      :show="!!pending"
      :title="pending ? t(`supplier.admin.actionTitle.${pending.action}`) : ''"
      :message="pending ? t(`supplier.admin.actionMessage.${pending.action}`, { amount: formatAmount(pending.settlement.amount) }) : ''"
      :danger="pending?.action === 'void'"
      @confirm="confirmAction"
      @cancel="pending = null"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { formatNumber } from '@/utils/format'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import SettlementDetailDialog from '../SettlementDetailDialog.vue'
import SettlementTable from '../SettlementTable.vue'
import { adminSupplierAPI } from '../../api'
import { formatAmount, previousMonthRange } from '../../format'
import type { Settlement, SettlementStatus, Supplier } from '../../types'

type Action = 'confirm' | 'mark-paid' | 'void'

const { t } = useI18n()
const appStore = useAppStore()

const statuses: SettlementStatus[] = ['draft', 'confirmed', 'paid', 'void']
const items = ref<Settlement[]>([])
const suppliers = ref<Supplier[]>([])
const filterSupplier = ref(0)
const filterStatus = ref<SettlementStatus | ''>('')
const loading = ref(false)
const saving = ref(false)
const page = ref(1)
const pageSize = 20
const total = ref(0)

const createOpen = ref(false)
const createForm = reactive({ supplierId: 0, start: '', end: '', notes: '' })
const preview = ref<Settlement | null>(null)
const detail = ref<Settlement | null>(null)
const pending = ref<{ settlement: Settlement; action: Action } | null>(null)

const createValid = computed(() => !!createForm.supplierId && !!createForm.start && !!createForm.end)

async function load() {
  loading.value = true
  try {
    const result = await adminSupplierAPI.listSettlements(page.value, pageSize, filterSupplier.value, filterStatus.value)
    items.value = result.items
    total.value = result.total
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.loadFailed'))
  } finally {
    loading.value = false
  }
}

function reload() {
  page.value = 1
  load()
}

function openCreate() {
  const range = previousMonthRange()
  Object.assign(createForm, { supplierId: filterSupplier.value, start: range.start, end: range.end, notes: '' })
  preview.value = null
  createOpen.value = true
}

function periodPayload() {
  return { supplier_id: createForm.supplierId, period_start: createForm.start, period_end: createForm.end }
}

async function runPreview() {
  saving.value = true
  try {
    preview.value = await adminSupplierAPI.previewSettlement(periodPayload())
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.loadFailed'))
  } finally {
    saving.value = false
  }
}

async function create() {
  saving.value = true
  try {
    detail.value = await adminSupplierAPI.createSettlement({ ...periodPayload(), notes: createForm.notes })
    createOpen.value = false
    await load()
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function view(s: Settlement) {
  try {
    detail.value = await adminSupplierAPI.getSettlement(s.id)
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.loadFailed'))
  }
}

function act(settlement: Settlement, action: Action) {
  pending.value = { settlement, action }
}

async function confirmAction() {
  if (!pending.value) return
  const { settlement, action } = pending.value
  pending.value = null
  try {
    await adminSupplierAPI.transitionSettlement(settlement.id, action)
    await load()
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.saveFailed'))
  }
}

onMounted(async () => {
  suppliers.value = (await adminSupplierAPI.list(1, 200).catch(() => null))?.items ?? []
  await load()
})
</script>
