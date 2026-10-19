<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between gap-3">
      <p class="text-sm text-gray-500 dark:text-dark-300">{{ t('supplier.accounts.hint') }}</p>
      <div class="flex gap-2">
        <button class="btn btn-secondary" :disabled="loading" @click="load">
          <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
        </button>
        <button class="btn btn-primary" @click="openAuthorize(null)">
          <Icon name="plus" size="md" class="mr-1" />{{ t('supplier.accounts.add') }}
        </button>
      </div>
    </div>

    <div class="card table-container">
      <table class="table">
        <thead>
          <tr>
            <th>{{ t('supplier.accounts.columns.name') }}</th>
            <th>{{ t('supplier.accounts.columns.type') }}</th>
            <th>{{ t('supplier.accounts.columns.review') }}</th>
            <th>{{ t('supplier.accounts.columns.status') }}</th>
            <th>{{ t('supplier.accounts.columns.proxy') }}</th>
            <th>{{ t('supplier.accounts.columns.window') }}</th>
            <th>{{ t('supplier.accounts.columns.lastUsed') }}</th>
            <th class="!text-right">{{ t('supplier.common.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!accounts.length">
            <td colspan="8" class="text-center text-gray-400">{{ loading ? t('supplier.common.loading') : t('supplier.common.empty') }}</td>
          </tr>
          <tr v-for="acc in accounts" :key="acc.id">
            <td>
              <div class="font-medium">{{ acc.name }}</div>
              <div v-if="acc.email" class="text-xs text-gray-500">{{ acc.email }}</div>
            </td>
            <td>{{ acc.type }}</td>
            <td>
              <span :class="reviewBadgeClass(acc.review_status)">{{ t(`supplier.review.${acc.review_status}`) }}</span>
              <div v-if="acc.review_note" class="mt-1 text-xs text-gray-500">{{ acc.review_note }}</div>
            </td>
            <td>
              <div>{{ acc.status }}<span v-if="!acc.schedulable" class="ml-1 text-xs text-gray-400">({{ t('supplier.accounts.notScheduled') }})</span></div>
              <div v-if="acc.error_message" class="max-w-xs truncate text-xs text-red-500" :title="acc.error_message">{{ acc.error_message }}</div>
              <div v-if="acc.rate_limit_reset_at" class="text-xs text-amber-600">
                {{ t('supplier.accounts.rateLimitedUntil', { time: formatDateTime(acc.rate_limit_reset_at) }) }}
              </div>
            </td>
            <td>{{ proxyName(acc.proxy_id) }}</td>
            <td class="text-xs">
              <span v-if="acc.session_window_end">{{ acc.session_window_status || '-' }} · {{ formatDateTime(acc.session_window_end) }}</span>
              <span v-else>-</span>
            </td>
            <td class="text-xs">{{ acc.last_used_at ? formatDateTime(acc.last_used_at) : '-' }}</td>
            <td class="text-right whitespace-nowrap">
              <button class="btn btn-ghost btn-sm" @click="openEdit(acc)">{{ t('supplier.common.edit') }}</button>
              <button class="btn btn-ghost btn-sm" @click="openAuthorize(acc)">{{ t('supplier.accounts.reauthorize') }}</button>
              <button class="btn btn-ghost btn-sm text-red-600" @click="deleting = acc">{{ t('supplier.common.delete') }}</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <AuthorizeDialog :show="authorizeOpen" :proxies="proxies" :account="authorizeTarget" @close="authorizeOpen = false" @saved="load" />

    <BaseDialog :show="!!editing" :title="t('supplier.accounts.editTitle')" @close="editing = null">
      <div class="space-y-4">
        <div>
          <label class="input-label">{{ t('supplier.accounts.columns.name') }}</label>
          <input v-model="editForm.name" type="text" class="input" />
        </div>
        <div>
          <label class="input-label">{{ t('supplier.accounts.columns.proxy') }}</label>
          <select v-model.number="editForm.proxyId" class="input">
            <option :value="0">{{ t('supplier.auth.noProxy') }}</option>
            <option v-for="p in proxies" :key="p.id" :value="p.id">{{ p.name }} ({{ p.host }}:{{ p.port }})</option>
          </select>
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end gap-2">
          <button class="btn btn-secondary" @click="editing = null">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" :disabled="saving" @click="saveEdit">{{ t('common.save') }}</button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="!!deleting"
      :title="t('supplier.accounts.deleteTitle')"
      :message="t('supplier.accounts.deleteMessage', { name: deleting?.name ?? '' })"
      danger
      @confirm="confirmDelete"
      @cancel="deleting = null"
    />
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { formatDateTime } from '@/utils/format'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import AuthorizeDialog from './AuthorizeDialog.vue'
import { deleteAccount, listAccounts, listProxies, updateAccount } from '../api'
import { reviewBadgeClass } from '../format'
import type { SupplierAccount, SupplierProxy } from '../types'

const { t } = useI18n()
const appStore = useAppStore()

const accounts = ref<SupplierAccount[]>([])
const proxies = ref<SupplierProxy[]>([])
const loading = ref(false)
const saving = ref(false)
const authorizeOpen = ref(false)
const authorizeTarget = ref<SupplierAccount | null>(null)
const editing = ref<SupplierAccount | null>(null)
const deleting = ref<SupplierAccount | null>(null)
const editForm = reactive({ name: '', proxyId: 0 })

function proxyName(id?: number | null): string {
  if (!id) return '-'
  return proxies.value.find((p) => p.id === id)?.name ?? `#${id}`
}

async function load() {
  loading.value = true
  try {
    const [accountList, proxyList] = await Promise.all([listAccounts(), listProxies()])
    accounts.value = accountList
    proxies.value = proxyList
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.loadFailed'))
  } finally {
    loading.value = false
  }
}

function openAuthorize(acc: SupplierAccount | null) {
  authorizeTarget.value = acc
  authorizeOpen.value = true
}

function openEdit(acc: SupplierAccount) {
  editing.value = acc
  editForm.name = acc.name
  editForm.proxyId = acc.proxy_id ?? 0
}

async function saveEdit() {
  if (!editing.value) return
  saving.value = true
  try {
    await updateAccount(editing.value.id, {
      name: editForm.name.trim(),
      ...(editForm.proxyId ? { proxy_id: editForm.proxyId } : { clear_proxy: true }),
    })
    editing.value = null
    await load()
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function confirmDelete() {
  if (!deleting.value) return
  try {
    await deleteAccount(deleting.value.id)
    deleting.value = null
    await load()
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.saveFailed'))
  }
}

onMounted(load)
</script>
