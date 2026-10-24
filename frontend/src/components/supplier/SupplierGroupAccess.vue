<script setup lang="ts">
import { ref, watch } from 'vue'
import { supplierGroupAPI, type SupplierGroupAccess } from '@/api/admin/suppliers'
import { useI18n } from 'vue-i18n'
const props = defineProps<{ groupId: number; platform: string; status: string }>()
const { t } = useI18n()
const state = ref<SupplierGroupAccess | null>(null)
const enabled = ref(false), supplierGroupName = ref(''), busy = ref(false), error = ref(''), saved = ref(false)
let generation = 0
async function load() {
  const token = ++generation
  state.value = null; error.value = ''; saved.value = false
  try {
    const data = await supplierGroupAPI.get(props.groupId)
    if (token !== generation) return
    state.value = data; enabled.value = data.enabled; supplierGroupName.value = data.supplier_group_name || ''
  } catch { if (token === generation) error.value = t('supplierPortal.groupAccess.loadFailed') }
}
watch(() => props.groupId, load, { immediate: true })
async function save() {
  if (!state.value || busy.value) return
  const token = generation
  busy.value = true; error.value = ''; saved.value = false
  try {
    const nameChanged = supplierGroupName.value.trim() !== (state.value.supplier_group_name || '')
    const data = nameChanged
      ? await supplierGroupAPI.set(props.groupId, enabled.value, state.value.version, supplierGroupName.value.trim())
      : await supplierGroupAPI.set(props.groupId, enabled.value, state.value.version)
    if (token === generation) { state.value = data; enabled.value = data.enabled; supplierGroupName.value = data.supplier_group_name || ''; saved.value = true }
  } catch { if (token === generation) error.value = t('supplierPortal.groupAccess.saveFailed') }
  finally { busy.value = false }
}
</script>
<template>
  <section class="rounded-lg border border-gray-200 p-4 dark:border-dark-600">
    <label class="flex items-center gap-3">
      <input v-model="enabled" type="checkbox" role="switch" :aria-checked="enabled" :disabled="!state || busy || (!enabled && (platform !== 'anthropic' || status !== 'active'))" @change="saved = false" />
      <span>{{ t('supplierPortal.groupAccess.label') }}</span>
    </label>
    <p class="input-hint">{{ t('supplierPortal.groupAccess.hint') }}</p>
    <div v-if="platform === 'anthropic'" class="mt-3">
      <label class="input-label">{{ t('supplierPortal.groupAccess.supplierGroupName') }}</label>
      <input v-model="supplierGroupName" maxlength="100" type="text" class="input" :disabled="!state || busy" />
      <p class="input-hint">{{ t('supplierPortal.groupAccess.supplierGroupNameHint') }}</p>
    </div>
    <p v-if="platform !== 'anthropic' || status !== 'active'" class="input-hint">{{ t('supplierPortal.groupAccess.eligibleOnly') }}</p>
    <p v-if="error" role="alert" class="text-sm text-red-500">{{ error }}</p>
    <p v-if="saved" role="status" class="text-sm text-green-600">{{ t('supplierPortal.groupAccess.saved') }}</p>
    <button v-if="state" type="button" class="btn btn-secondary mt-2" :disabled="busy || (enabled === state.enabled && supplierGroupName.trim() === (state.supplier_group_name || ''))" @click="save">{{ t('supplierPortal.groupAccess.save') }}</button>
    <button type="button" class="btn btn-secondary mt-2" :disabled="busy" @click="load">{{ t('supplierPortal.groupAccess.retry') }}</button>
  </section>
</template>
