<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center gap-3">
      <input v-model="search" type="text" class="input sm:max-w-64" :placeholder="t('supplier.admin.searchPlaceholder')" @keyup.enter="reload" />
      <div class="flex flex-1 justify-end gap-2">
        <button class="btn btn-secondary" :disabled="loading" @click="load">
          <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
        </button>
        <button class="btn btn-primary" @click="openForm(null)">
          <Icon name="plus" size="md" class="mr-1" />{{ t('supplier.admin.create') }}
        </button>
      </div>
    </div>

    <div class="card table-container">
      <table class="table">
        <thead>
          <tr>
            <th>{{ t('supplier.admin.columns.name') }}</th>
            <th>{{ t('supplier.admin.columns.user') }}</th>
            <th>{{ t('supplier.admin.columns.status') }}</th>
            <th class="!text-right">{{ t('supplier.admin.columns.accounts') }}</th>
            <th class="!text-right">{{ t('supplier.admin.columns.proxies') }}</th>
            <th class="!text-right">{{ t('supplier.admin.columns.defaultConcurrency') }}</th>
            <th class="!text-right">{{ t('supplier.admin.columns.maxAccounts') }}</th>
            <th class="!text-right">{{ t('supplier.common.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!items.length">
            <td colspan="8" class="text-center text-gray-400">{{ loading ? t('supplier.common.loading') : t('supplier.common.empty') }}</td>
          </tr>
          <tr v-for="s in items" :key="s.id">
            <td class="font-medium">{{ s.name }}</td>
            <td class="text-xs">{{ s.user_email }} <span class="text-gray-400">#{{ s.user_id }}</span></td>
            <td>
              <span :class="s.status === 'active' ? 'badge badge-success' : 'badge badge-gray'">{{ t(`supplier.status.${s.status}`) }}</span>
            </td>
            <td class="text-right">{{ s.account_count }}</td>
            <td class="text-right">{{ s.proxy_count }}</td>
            <td class="text-right">{{ s.default_concurrency }}</td>
            <td class="text-right">{{ s.max_accounts || t('supplier.admin.unlimited') }}</td>
            <td class="text-right whitespace-nowrap">
              <button class="btn btn-ghost btn-sm" @click="openUsage(s)">{{ t('supplier.admin.usage') }}</button>
              <button class="btn btn-ghost btn-sm" @click="openAttach(s)">{{ t('supplier.admin.attach') }}</button>
              <button class="btn btn-ghost btn-sm" @click="openForm(s)">{{ t('supplier.common.edit') }}</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
    <Pagination v-if="total > pageSize" :page="page" :total="total" :page-size="pageSize" @update:page="(p: number) => { page = p; load() }" />

    <BaseDialog :show="formOpen" :title="editing ? t('supplier.admin.editTitle') : t('supplier.admin.createTitle')" @close="formOpen = false">
      <div class="space-y-4">
        <div v-if="!editing">
          <label class="input-label">{{ t('supplier.admin.userEmail') }}</label>
          <input v-model="form.email" type="email" class="input" placeholder="user@example.com" />
          <p class="mt-1 text-xs text-gray-500">{{ t('supplier.admin.userEmailHint') }}</p>
        </div>
        <div>
          <label class="input-label">{{ t('supplier.admin.columns.name') }}</label>
          <input v-model="form.name" type="text" class="input" />
        </div>
        <div class="grid grid-cols-2 gap-3">
          <div>
            <label class="input-label">{{ t('supplier.admin.columns.defaultConcurrency') }}</label>
            <input v-model.number="form.default_concurrency" type="number" min="1" class="input" />
          </div>
          <div>
            <label class="input-label">{{ t('supplier.admin.columns.maxAccounts') }}</label>
            <input v-model.number="form.max_accounts" type="number" min="0" class="input" />
            <p class="mt-1 text-xs text-gray-500">{{ t('supplier.admin.maxAccountsHint') }}</p>
          </div>
        </div>
        <div v-if="editing">
          <label class="input-label">{{ t('supplier.admin.columns.status') }}</label>
          <select v-model="form.status" class="input">
            <option value="active">{{ t('supplier.status.active') }}</option>
            <option value="disabled">{{ t('supplier.status.disabled') }}</option>
          </select>
        </div>
        <div>
          <label class="input-label">{{ t('supplier.admin.notes') }}</label>
          <textarea v-model="form.notes" rows="2" class="input" />
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end gap-2">
          <button class="btn btn-secondary" @click="formOpen = false">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" :disabled="saving" @click="save">{{ t('common.save') }}</button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog :show="!!attachTarget" :title="t('supplier.admin.attachTitle', { name: attachTarget?.name ?? '' })" @close="attachTarget = null">
      <label class="input-label">{{ t('supplier.admin.attachIds') }}</label>
      <input v-model="attachIds" type="text" class="input font-mono" placeholder="12, 15, 18" />
      <p class="mt-1 text-xs text-gray-500">{{ t('supplier.admin.attachHint') }}</p>
      <template #footer>
        <div class="flex justify-end gap-2">
          <button class="btn btn-secondary" @click="attachTarget = null">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" :disabled="saving || !parsedAttachIds.length" @click="attach">{{ t('supplier.admin.attach') }}</button>
        </div>
      </template>
    </BaseDialog>

    <BaseDialog :show="!!usageTarget" :title="t('supplier.admin.usageTitle', { name: usageTarget?.name ?? '' })" width="extra-wide" @close="usageTarget = null">
      <div class="mb-3 flex flex-wrap items-end gap-3">
        <input v-model="usageRange.start" type="date" class="input w-auto" />
        <input v-model="usageRange.end" type="date" class="input w-auto" />
        <button class="btn btn-secondary btn-sm" @click="loadUsage">{{ t('common.refresh') }}</button>
        <span v-if="usage" class="ml-auto text-sm">{{ t('supplier.usage.amount') }}: <b>{{ formatAmount(usage.totals.amount) }}</b></span>
      </div>
      <UsageTable :rows="usage?.by_family ?? []" label-key="family" :label-formatter="(v: string) => t(`supplier.family.${v}`)" />
      <div class="mt-4">
        <UsageTable :rows="usage?.by_model ?? []" label-key="model" />
      </div>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Pagination from '@/components/common/Pagination.vue'
import Icon from '@/components/icons/Icon.vue'
import UsageTable from '../UsageTable.vue'
import { adminSupplierAPI } from '../../api'
import { defaultRange, formatAmount } from '../../format'
import type { Supplier, UsageSummary } from '../../types'

const { t } = useI18n()
const appStore = useAppStore()

const items = ref<Supplier[]>([])
const loading = ref(false)
const saving = ref(false)
const search = ref('')
const page = ref(1)
const pageSize = 20
const total = ref(0)

const formOpen = ref(false)
const editing = ref<Supplier | null>(null)
const form = reactive({ email: '', name: '', status: 'active', default_concurrency: 1, max_accounts: 0, notes: '' })

const attachTarget = ref<Supplier | null>(null)
const attachIds = ref('')
const parsedAttachIds = computed(() =>
  attachIds.value
    .split(/[\s,，]+/)
    .map((v) => Number(v))
    .filter((v) => Number.isInteger(v) && v > 0),
)

const usageTarget = ref<Supplier | null>(null)
const usageRange = reactive(defaultRange(30))
const usage = ref<UsageSummary | null>(null)

async function load() {
  loading.value = true
  try {
    const result = await adminSupplierAPI.list(page.value, pageSize, search.value.trim())
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

function openForm(s: Supplier | null) {
  editing.value = s
  Object.assign(form, {
    email: '',
    name: s?.name ?? '',
    status: s?.status ?? 'active',
    default_concurrency: s?.default_concurrency ?? 1,
    max_accounts: s?.max_accounts ?? 0,
    notes: s?.notes ?? '',
  })
  formOpen.value = true
}

async function save() {
  saving.value = true
  try {
    const payload = {
      name: form.name.trim(),
      status: form.status,
      default_concurrency: form.default_concurrency,
      max_accounts: form.max_accounts,
      notes: form.notes,
    }
    if (editing.value) await adminSupplierAPI.update(editing.value.id, payload)
    else await adminSupplierAPI.create({ ...payload, email: form.email.trim() })
    formOpen.value = false
    await load()
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.saveFailed'))
  } finally {
    saving.value = false
  }
}

function openAttach(s: Supplier) {
  attachTarget.value = s
  attachIds.value = ''
}

async function attach() {
  if (!attachTarget.value) return
  saving.value = true
  try {
    await adminSupplierAPI.attachAccounts(attachTarget.value.id, parsedAttachIds.value)
    attachTarget.value = null
    await load()
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function openUsage(s: Supplier) {
  usageTarget.value = s
  usage.value = null
  await loadUsage()
}

async function loadUsage() {
  if (!usageTarget.value) return
  try {
    usage.value = await adminSupplierAPI.usageSummary(usageTarget.value.id, { ...usageRange })
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.loadFailed'))
  }
}

onMounted(load)
</script>
