<template>
  <div class="space-y-4">
    <div class="flex flex-wrap items-center gap-3">
      <select v-model.number="supplierId" class="input w-48" @change="load">
        <option :value="0">{{ t('supplier.admin.allSuppliers') }}</option>
        <option v-for="s in suppliers" :key="s.id" :value="s.id">{{ s.name }}</option>
      </select>
      <select v-model="reviewStatus" class="input w-36" @change="load">
        <option value="">{{ t('supplier.admin.allStatuses') }}</option>
        <option value="pending">{{ t('supplier.review.pending') }}</option>
        <option value="approved">{{ t('supplier.review.approved') }}</option>
        <option value="rejected">{{ t('supplier.review.rejected') }}</option>
      </select>
      <button class="btn btn-secondary ml-auto" :disabled="loading" @click="load">
        <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
      </button>
    </div>

    <div class="card table-container">
      <table class="table">
        <thead>
          <tr>
            <th>{{ t('supplier.settlements.supplier') }}</th>
            <th>{{ t('supplier.accounts.columns.name') }}</th>
            <th>{{ t('supplier.accounts.columns.type') }}</th>
            <th>{{ t('supplier.accounts.columns.review') }}</th>
            <th>{{ t('supplier.accounts.columns.status') }}</th>
            <th>{{ t('supplier.admin.columns.groups') }}</th>
            <th class="!text-right">{{ t('supplier.admin.columns.concurrency') }}</th>
            <th class="!text-right">{{ t('supplier.common.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!accounts.length">
            <td colspan="8" class="text-center text-gray-400">{{ loading ? t('supplier.common.loading') : t('supplier.common.empty') }}</td>
          </tr>
          <tr v-for="acc in accounts" :key="acc.id">
            <td>{{ supplierName(acc.supplier_id) }}</td>
            <td>
              <div class="font-medium">{{ acc.name }} <span class="text-xs text-gray-400">#{{ acc.id }}</span></div>
              <div v-if="acc.email" class="text-xs text-gray-500">{{ acc.email }}</div>
            </td>
            <td>{{ acc.type }}</td>
            <td><span :class="reviewBadgeClass(acc.review_status)">{{ t(`supplier.review.${acc.review_status}`) }}</span></td>
            <td>
              {{ acc.status }}
              <div v-if="acc.error_message" class="max-w-xs truncate text-xs text-red-500" :title="acc.error_message">{{ acc.error_message }}</div>
            </td>
            <td class="text-xs">{{ acc.group_ids.map(groupName).join(', ') || '-' }}</td>
            <td class="text-right">{{ acc.concurrency }}</td>
            <td class="text-right whitespace-nowrap">
              <button class="btn btn-ghost btn-sm text-green-600" @click="openReview(acc, 'approve')">{{ t('supplier.admin.approve') }}</button>
              <button class="btn btn-ghost btn-sm text-red-600" @click="openReview(acc, 'reject')">{{ t('supplier.admin.reject') }}</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <BaseDialog
      :show="!!reviewing"
      :title="reviewAction === 'approve' ? t('supplier.admin.approveTitle', { name: reviewing?.name ?? '' }) : t('supplier.admin.rejectTitle', { name: reviewing?.name ?? '' })"
      @close="reviewing = null"
    >
      <div class="space-y-4">
        <template v-if="reviewAction === 'approve'">
          <div>
            <label class="input-label">{{ t('supplier.admin.columns.groups') }}</label>
            <div class="max-h-48 space-y-1 overflow-y-auto rounded border border-gray-200 p-2 dark:border-dark-600">
              <label v-for="g in groups" :key="g.id" class="flex items-center gap-2 text-sm">
                <input v-model="reviewForm.groupIds" type="checkbox" :value="g.id" />
                {{ g.name }} <span class="text-xs text-gray-400">×{{ g.rate_multiplier }}</span>
              </label>
              <p v-if="!groups.length" class="text-xs text-gray-400">{{ t('supplier.common.empty') }}</p>
            </div>
          </div>
          <div class="grid grid-cols-3 gap-3">
            <div>
              <label class="input-label">{{ t('supplier.admin.columns.concurrency') }}</label>
              <input v-model.number="reviewForm.concurrency" type="number" min="1" class="input" />
            </div>
            <div>
              <label class="input-label">{{ t('supplier.admin.priority') }}</label>
              <input v-model.number="reviewForm.priority" type="number" min="0" class="input" />
            </div>
            <div>
              <label class="input-label">{{ t('supplier.admin.rateMultiplier') }}</label>
              <input v-model.number="reviewForm.rateMultiplier" type="number" min="0" step="0.01" class="input" />
            </div>
          </div>
          <p class="text-xs text-gray-500">{{ t('supplier.admin.rateMultiplierHint') }}</p>
        </template>
        <div>
          <label class="input-label">{{ t('supplier.admin.reviewNote') }}</label>
          <textarea v-model="reviewForm.note" rows="2" class="input" />
        </div>
      </div>
      <template #footer>
        <div class="flex justify-end gap-2">
          <button class="btn btn-secondary" @click="reviewing = null">{{ t('common.cancel') }}</button>
          <button :class="reviewAction === 'approve' ? 'btn btn-primary' : 'btn btn-danger'" :disabled="saving" @click="submitReview">
            {{ reviewAction === 'approve' ? t('supplier.admin.approve') : t('supplier.admin.reject') }}
          </button>
        </div>
      </template>
    </BaseDialog>
  </div>
</template>

<script setup lang="ts">
import { onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import { adminAPI } from '@/api/admin'
import type { AdminGroup } from '@/types'
import BaseDialog from '@/components/common/BaseDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { adminSupplierAPI } from '../../api'
import { reviewBadgeClass } from '../../format'
import type { AdminSupplierAccount, ReviewStatus, Supplier } from '../../types'

const { t } = useI18n()
const appStore = useAppStore()

const accounts = ref<AdminSupplierAccount[]>([])
const suppliers = ref<Supplier[]>([])
const groups = ref<AdminGroup[]>([])
const supplierId = ref(0)
const reviewStatus = ref<ReviewStatus | ''>('pending')
const loading = ref(false)
const saving = ref(false)

const reviewing = ref<AdminSupplierAccount | null>(null)
const reviewAction = ref<'approve' | 'reject'>('approve')
const reviewForm = reactive({ groupIds: [] as number[], concurrency: 1, priority: 1, rateMultiplier: 1, note: '' })

const supplierName = (id: number) => suppliers.value.find((s) => s.id === id)?.name ?? `#${id}`
const groupName = (id: number) => groups.value.find((g) => g.id === id)?.name ?? `#${id}`

async function load() {
  loading.value = true
  try {
    accounts.value = await adminSupplierAPI.listAccounts(supplierId.value, reviewStatus.value)
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.loadFailed'))
  } finally {
    loading.value = false
  }
}

function openReview(acc: AdminSupplierAccount, action: 'approve' | 'reject') {
  reviewing.value = acc
  reviewAction.value = action
  Object.assign(reviewForm, {
    groupIds: [...acc.group_ids],
    concurrency: acc.concurrency || 1,
    priority: acc.priority,
    rateMultiplier: acc.rate_multiplier ?? 1,
    note: '',
  })
}

async function submitReview() {
  if (!reviewing.value) return
  saving.value = true
  try {
    await adminSupplierAPI.review(
      reviewing.value.id,
      reviewAction.value === 'approve'
        ? {
            action: 'approve',
            note: reviewForm.note,
            group_ids: reviewForm.groupIds,
            concurrency: reviewForm.concurrency,
            priority: reviewForm.priority,
            rate_multiplier: reviewForm.rateMultiplier,
          }
        : { action: 'reject', note: reviewForm.note },
    )
    reviewing.value = null
    await load()
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.saveFailed'))
  } finally {
    saving.value = false
  }
}

onMounted(async () => {
  const [supplierPage, allGroups] = await Promise.all([
    adminSupplierAPI.list(1, 200).catch(() => null),
    adminAPI.groups.getAll('anthropic').catch(() => [] as AdminGroup[]),
  ])
  suppliers.value = supplierPage?.items ?? []
  groups.value = allGroups
  await load()
})
</script>
