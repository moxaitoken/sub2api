<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { supplierAdminAPI, supplierFinanceAPI, type ManagedSupplier, type SettlementInput, type Settlement } from '@/api/admin/suppliers'
const props = defineProps<{ supplier: ManagedSupplier | null }>()
const emit = defineEmits<{ close: []; completed: [] }>()
const { t } = useI18n()
const owner = ref<ManagedSupplier | null>(null), busy = ref(false), error = ref(''), frozen = ref(false), result = ref<Settlement | null>(null)
const form = ref<SettlementInput>({amount_usd:'',payment_method:'usd_transfer',reference:'',remark:''})
let key = '', generation = 0
function cents(value: string): bigint | null {
  if (!/^(0|[1-9]\d{0,16})(\.\d{1,2})?$/.test(value)) return null
  const [whole, part = ''] = value.split('.'); return BigInt(whole + (part + '00').slice(0,2))
}
const valid = computed(() => {
  const n = cents(form.value.amount_usd), available = cents(owner.value?.unsettled_usd || '')
  return n !== null && available !== null && n > 0n && n <= available && n <= 9223372036854775807n
})
watch(() => props.supplier?.id, async id => {
  const current = ++generation; owner.value = null; error.value = ''; result.value = null; frozen.value = false
  key = crypto.randomUUID(); form.value = {amount_usd:'',payment_method:'usd_transfer',reference:'',remark:''}
  if (!id) return
  busy.value = true
  try { const row = await supplierAdminAPI.detail(id); if (current === generation) owner.value = row }
  catch { if (current === generation) error.value = t('supplierAdmin.failed') }
  finally { if (current === generation) busy.value = false }
}, {immediate:true})
async function submit() {
  if (busy.value || !owner.value || !valid.value) return
  busy.value = true; frozen.value = true; error.value = ''
  try { result.value = await supplierFinanceAPI.settle(owner.value.id, {...form.value}, key); emit('completed') }
  catch (cause) {
    const e = cause as {status?:number}
    if (e.status === 409 || e.status === 400) { frozen.value = false; key = crypto.randomUUID(); error.value = t('supplierAdmin.settlementConflict'); try { owner.value = await supplierAdminAPI.detail(owner.value.id) } catch { owner.value = null } }
    else if ([401,403,423,404].includes(e.status || 0)) { frozen.value = false; owner.value = null; error.value = t('supplierAdmin.failed') }
    else error.value = t('supplierAdmin.uncertain')
  } finally { busy.value = false }
}
function close() { if (!busy.value && (!frozen.value || result.value)) emit('close') }
</script>
<template>
  <BaseDialog :show="!!supplier" :title="t('supplierAdmin.settle')" :show-close-button="!busy && (!frozen || !!result)" :close-on-escape="!busy && (!frozen || !!result)" @close="close">
    <p class="mb-4 font-medium">{{ supplier?.name }} · {{ supplier?.login_account }}</p>
    <p v-if="error" role="alert" class="mb-4 text-red-500">{{ error }}</p>
    <template v-if="result">
      <p role="status">{{ t('supplierAdmin.settlementSuccess') }}</p>
      <dl class="mt-4 grid grid-cols-[auto_1fr] gap-3 break-all"><dt>{{ t('supplierAdmin.settlementNumber') }}</dt><dd>{{ result.id }}</dd><dt>{{ t('supplierAdmin.amount') }}</dt><dd>USD {{ result.amount_usd }}</dd><dt>{{ t('supplierAdmin.balanceChange') }}</dt><dd>{{ result.before_usd }} → {{ result.after_usd }}</dd></dl>
      <button class="btn btn-primary mt-5" @click="close">{{ t('common.close') }}</button>
    </template>
    <form v-else @submit.prevent="submit">
      <p class="mb-4 text-sm">{{ t('supplierAdmin.unsettled') }}：{{ owner?.unsettled_usd || '—' }}</p>
      <p class="mb-4 text-sm text-gray-500">{{ t('supplierAdmin.offlineNote') }}</p>
      <fieldset :disabled="busy || frozen || !owner" class="space-y-4">
        <label class="block">{{ t('supplierAdmin.amount') }}<input v-model="form.amount_usd" class="input mt-2" inputmode="decimal" required maxlength="20" /></label>
        <label class="block">{{ t('supplierAdmin.paymentMethod') }}<select v-model="form.payment_method" class="input mt-2"><option v-for="method in ['usd_transfer','cny_transfer','usdt_transfer','other']" :key="method" :value="method">{{ t(`supplierAdmin.methods.${method}`) }}</option></select></label>
        <label class="block">{{ t('supplierAdmin.reference') }}<input v-model="form.reference" class="input mt-2" maxlength="200" /></label>
        <label class="block">{{ t('supplierAdmin.remark') }}<textarea v-model="form.remark" class="input mt-2" maxlength="1000" rows="3" /></label>
      </fieldset>
      <div class="mt-6 flex justify-end gap-3"><button v-if="!frozen" type="button" class="btn btn-secondary" :disabled="busy" @click="close">{{ t('common.cancel') }}</button><button class="btn btn-primary" :disabled="busy || !valid">{{ t(frozen ? 'supplierAdmin.retrySettlement' : 'supplierAdmin.confirmSettlement') }}</button></div>
    </form>
  </BaseDialog>
</template>
