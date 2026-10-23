<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import TablePageLayout from '@/components/layout/TablePageLayout.vue'
import DataTable from '@/components/common/DataTable.vue'
import Pagination from '@/components/common/Pagination.vue'
import BaseDialog from '@/components/common/BaseDialog.vue'
import SettlementDialog from './SettlementDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { supplierAdminAPI, type ManagedSupplier, type SupplierAccount } from '@/api/admin/suppliers'

const { t } = useI18n()
const rows = ref<ManagedSupplier[]>([]), total = ref(0), page = ref(1), search = ref('')
const loading = ref(false), busy = ref(false), error = ref(''), dialogError = ref('')
const dialog = ref<'create' | 'details' | 'reset' | 'status' | 'accounts' | null>(null)
const selected = ref<ManagedSupplier | null>(null)
const settling = ref<ManagedSupplier | null>(null)
const name = ref(''), login = ref(''), password = ref(''), mutationKey = ref('')
const accounts = ref<SupplierAccount[]>([]), accountTotal = ref(0), accountPage = ref(1)
let generation = 0, accountGeneration = 0
const columns = computed(() => [
  { key: 'name', label: t('supplierAdmin.supplier') },
  { key: 'login_account', label: t('supplierAdmin.login') },
  { key: 'portal_status', label: t('common.status') },
  { key: 'unsettled', label: t('supplierAdmin.unsettled') },
  { key: 'settled', label: t('supplierAdmin.settled') },
  { key: 'total', label: t('supplierAdmin.total') },
  { key: 'business', label: t('supplierAdmin.business') },
  { key: 'actions', label: t('common.actions') }
])
const accountColumns = computed(() => [
  { key: 'name', label: t('common.name') },
  { key: 'admission_state', label: t('supplierAdmin.admission') },
  { key: 'desired_state', label: t('supplierAdmin.supplyState') }
])
const title = computed(() => t(`supplierAdmin.${dialog.value === 'create' ? 'create' : dialog.value === 'reset' ? 'resetPassword' : dialog.value === 'accounts' ? 'accounts' : dialog.value === 'status' ? 'changeStatus' : 'details'}`))
function failure(cause: unknown): string {
  const value = cause as { status?: number; reason?: string }
  if (value.status === 401 || value.status === 403) {
    rows.value = []; total.value = 0; accounts.value = []; selected.value = null
    return t('supplierAdmin.denied')
  }
  if (value.status === 409) return t('supplierAdmin.conflict')
  if (value.status === 423) return t('supplierAdmin.compliance')
  if (value.status === 400) return t('supplierAdmin.invalid')
  return t('supplierAdmin.failed')
}
async function load(next = page.value) {
  const current = ++generation
  loading.value = true; error.value = ''
  try {
    const result = await supplierAdminAPI.list(search.value.trim(), next)
    if (current !== generation) return
    rows.value = result.items; total.value = result.total; page.value = result.page
  } catch (cause) {
    if (current === generation) { rows.value = []; total.value = 0; error.value = failure(cause) }
  } finally { if (current === generation) loading.value = false }
}
function close() {
  if (busy.value) return
  dialog.value = null; selected.value = null; password.value = ''; dialogError.value = ''; accounts.value = []; accountGeneration++
}
async function open(mode: NonNullable<typeof dialog.value>, row?: ManagedSupplier) {
  password.value = ''; dialogError.value = ''; name.value = ''; login.value = ''; mutationKey.value = crypto.randomUUID()
  selected.value = row || null; dialog.value = mode
  if (mode === 'accounts') { await loadAccounts(1); return }
  if (row) {
    busy.value = true
    try { selected.value = await supplierAdminAPI.detail(row.id) }
    catch (cause) { dialogError.value = failure(cause); selected.value = null }
    finally { busy.value = false }
  }
}
async function loadAccounts(next: number) {
  const owner = selected.value?.id, current = ++accountGeneration
  if (!owner) return
  busy.value = true; dialogError.value = ''; accounts.value = []; accountTotal.value = 0
  try {
    const result = await supplierAdminAPI.accounts(owner, next)
    if (current !== accountGeneration || owner !== selected.value?.id) return
    accounts.value = result.items; accountTotal.value = result.total; accountPage.value = result.page
  } catch (cause) { if (current === accountGeneration) dialogError.value = failure(cause) }
  finally { if (current === accountGeneration) busy.value = false }
}
async function submit() {
  if (busy.value) return
  const creating = dialog.value === 'create'
  busy.value = true; dialogError.value = ''
  try {
    if (dialog.value === 'create') {
      await supplierAdminAPI.create({ name: name.value.trim(), login_account: login.value.trim(), initial_password: password.value }, mutationKey.value)
    } else if (selected.value && dialog.value === 'reset') {
      await supplierAdminAPI.resetPassword(selected.value, password.value, mutationKey.value)
    } else if (selected.value && dialog.value === 'status') {
      await supplierAdminAPI.setStatus(selected.value, selected.value.portal_status !== 'active', mutationKey.value)
    } else return
    password.value = ''; dialog.value = null; selected.value = null
    await load(creating ? 1 : page.value)
  } catch (cause) { dialogError.value = failure(cause) }
  finally { busy.value = false }
}
function resetFromDetails() {
  dialog.value = 'reset'; mutationKey.value = crypto.randomUUID(); dialogError.value = ''
}
onMounted(() => load())
onUnmounted(() => { generation++; accountGeneration++; password.value = '' })
</script>

<template>
  <AppLayout>
    <TablePageLayout class="supplier-admin-list">
      <template #filters>
        <div class="space-y-3">
          <p v-if="error" role="alert" class="rounded-xl bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">{{ error }}</p>
          <form class="flex flex-wrap items-center gap-3" @submit.prevent="load(1)">
            <input v-model="search" class="input w-full sm:w-72" :placeholder="t('supplierAdmin.search')" :aria-label="t('supplierAdmin.search')" maxlength="100" />
            <button class="btn btn-secondary" :disabled="loading">{{ t('common.search') }}</button>
            <button type="button" class="btn btn-secondary" :disabled="loading" @click="search = ''; load(1)">{{ t('common.reset') }}</button>
            <div class="flex flex-1 justify-end gap-2">
              <button type="button" class="btn btn-secondary" :disabled="loading || busy" :aria-label="t('common.refresh')" @click="load()"><Icon name="refresh" size="md" /></button>
              <button type="button" class="btn btn-primary" :disabled="busy" @click="open('create')"><Icon name="plus" size="md" class="mr-2" />{{ t('supplierAdmin.create') }}</button>
            </div>
          </form>
          <p class="text-sm text-gray-500 dark:text-gray-400">{{ t('supplierAdmin.financeNote') }}</p>
        </div>
      </template>
      <template #table>
        <DataTable :columns="columns" :data="rows" :loading="loading" :sticky-actions-column="false" row-key="id">
          <template #cell-portal_status="{ row }"><span :class="row.portal_status === 'active' ? 'badge badge-success' : 'badge badge-gray'">{{ t(row.portal_status === 'active' ? 'supplierAdmin.active' : 'supplierAdmin.disabled') }}</span></template>
          <template #cell-unsettled="{row}">{{ row.unsettled_usd }}</template><template #cell-settled="{row}">{{ row.settled_usd }}</template><template #cell-total="{row}">{{ row.total_consumption_usd }}</template>
          <template #cell-business="{ row }"><div class="flex flex-wrap gap-2"><RouterLink v-for="tab in ['accounts','proxies','usage']" :key="tab" class="btn btn-secondary btn-sm" :to="`/admin/suppliers/${row.id}/${tab}`">{{ t(`supplierAdmin.${tab}`) }}</RouterLink></div></template>
          <template #cell-actions="{ row }"><div class="flex flex-wrap gap-2">
<button class="btn btn-secondary btn-sm" :disabled="busy || !row.unsettled_usd || /^0(?:\.0+)?$/.test(row.unsettled_usd)" @click="settling = row">{{ t('supplierAdmin.settle') }}</button>
            <button class="btn btn-secondary btn-sm" :disabled="busy" @click="open('details', row)">{{ t('supplierAdmin.details') }}</button>
            <button class="btn btn-secondary btn-sm" :disabled="busy" @click="open('status', row)">{{ t(row.portal_status === 'active' ? 'supplierAdmin.disable' : 'supplierAdmin.enable') }}</button>
          </div></template>
        </DataTable>
      </template>
      <template #pagination><Pagination v-if="total > 0" :page="page" :page-size="20" :total="total" :show-page-size-selector="false" @update:page="load" /></template>
    </TablePageLayout>

    <SettlementDialog :supplier="settling" @close="settling = null" @completed="load()" />
    <BaseDialog :show="dialog !== null" :title="title" :width="dialog === 'accounts' ? 'wide' : 'normal'" :show-close-button="!busy" :close-on-escape="!busy" @close="close">
      <p v-if="dialogError" role="alert" class="mb-4 rounded-xl bg-red-50 p-3 text-sm text-red-700 dark:bg-red-950 dark:text-red-300">{{ dialogError }}</p>
      <template v-if="dialog === 'accounts'">
        <p class="mb-4 text-sm text-gray-500">{{ selected?.name }} · {{ selected?.login_account }}</p>
        <DataTable :columns="accountColumns" :data="accounts" :loading="busy" row-key="id">
          <template #cell-admission_state="{ row }">{{ t(`supplierAdmin.states.${row.admission_state}`) }}</template>
          <template #cell-desired_state="{ row }">{{ t(row.desired_state === 'running' ? 'supplierAdmin.running' : 'supplierAdmin.paused') }}</template>
        </DataTable>
        <Pagination v-if="accountTotal > 0" :page="accountPage" :page-size="20" :total="accountTotal" :show-page-size-selector="false" @update:page="loadAccounts" />
      </template>
      <template v-else-if="dialog === 'details'">
        <dl v-if="selected" class="space-y-4 text-sm"><div><dt class="text-gray-500">{{ t('supplierAdmin.supplier') }}</dt><dd>{{ selected.name }}</dd></div><div><dt class="text-gray-500">{{ t('supplierAdmin.login') }}</dt><dd>{{ selected.login_account }}</dd></div><div><dt class="text-gray-500">{{ t('common.status') }}</dt><dd>{{ t(selected.portal_status === 'active' ? 'supplierAdmin.active' : 'supplierAdmin.disabled') }}</dd></div></dl>
        <button class="btn btn-secondary mt-6" :disabled="busy || !selected" @click="resetFromDetails">{{ t('supplierAdmin.resetPassword') }}</button>
      </template>
      <form v-else class="space-y-4" @submit.prevent="submit">
        <template v-if="dialog === 'create'">
          <label class="block"><span class="mb-2 block text-sm">{{ t('supplierAdmin.supplier') }}</span><input v-model="name" class="input w-full" required maxlength="100" :disabled="busy" /></label>
          <label class="block"><span class="mb-2 block text-sm">{{ t('supplierAdmin.login') }}</span><input v-model="login" class="input w-full" required minlength="4" maxlength="64" pattern="[A-Za-z0-9_.@-]+" autocomplete="off" :disabled="busy" /></label>
        </template>
        <template v-if="dialog === 'create' || dialog === 'reset'">
          <label class="block"><span class="mb-2 block text-sm">{{ t('supplierAdmin.password') }}</span><input v-model="password" class="input w-full" type="password" required minlength="10" maxlength="64" autocomplete="new-password" :disabled="busy" /></label>
          <p class="text-sm text-gray-500">{{ t('supplierAdmin.passwordHint') }}</p>
        </template>
        <p v-if="dialog === 'status'">{{ t(selected?.portal_status === 'active' ? 'supplierAdmin.disableConfirm' : 'supplierAdmin.enableConfirm', { name: selected?.name }) }}</p>
        <div class="flex justify-end gap-3"><button type="button" class="btn btn-secondary" :disabled="busy" @click="close">{{ t('common.cancel') }}</button><button type="submit" class="btn btn-primary" :disabled="busy || (dialog !== 'create' && !selected)">{{ t(busy ? 'common.submitting' : 'common.confirm') }}</button></div>
      </form>
    </BaseDialog>
  </AppLayout>
</template>

<style scoped>
.supplier-admin-list :deep(th:first-child),.supplier-admin-list :deep(td:first-child) { position:static !important; }
.supplier-admin-list :deep(table) { min-width:1400px; }
.supplier-admin-list :deep(td) { vertical-align:top; }
</style>
