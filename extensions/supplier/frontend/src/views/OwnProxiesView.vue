<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import type { SupplierClient } from '../api/client'
import type { OwnProxy } from '../api/types'
import PortalIcon from '../components/PortalIcon.vue'
import PortalDialog from '../components/PortalDialog.vue'
import ProxyCreateForm from '../components/ProxyCreateForm.vue'
import RequestError from '../components/RequestError.vue'
const props = defineProps<{ client: SupplierClient }>()
const { t } = useI18n()
const rows = ref<OwnProxy[]>([]), loading = ref(false), error = ref<unknown>(null), add = ref(false), busy = ref(false)
const removing = ref<OwnProxy | null>(null), deleteError = ref<unknown>(null)
function requestDelete(proxy: OwnProxy) { removing.value = proxy; deleteError.value = null }
async function remove() {
  if (!removing.value || busy.value) return
  busy.value = true; deleteError.value = null
  try { await props.client.deleteOwnProxy(removing.value.id); removing.value = null }
  catch (cause) { if (alive) deleteError.value = cause }
  finally { if (alive) { busy.value = false; await load() } }
}
let generation = 0, alive = true
async function load() {
  const expected = ++generation; loading.value = true; error.value = null
  try { const data = await props.client.ownProxies(); if (alive && expected === generation) rows.value = data.items }
  catch (cause) { if (alive && expected === generation) { rows.value = []; error.value = cause } }
  finally { if (alive && expected === generation) loading.value = false }
}
async function test(proxy: OwnProxy) {
  if (busy.value) return
  busy.value = true; error.value = null
  try { await props.client.testOwnProxy(proxy.id); await load() }
  catch (cause) { if (alive) error.value = cause }
  finally { if (alive) busy.value = false }
}
async function created() { add.value = false; await load() }
onMounted(load)
onUnmounted(() => { alive = false; generation++ })
</script>
<template>
  <div class="supplier-workspace workspace-proxies">
    <div class="workspace-heading"><div><h1>{{ t('portal.proxies') }}</h1><p>{{ t('workspace.proxiesDescription') }}</p></div><div class="heading-actions"><button :disabled="loading || busy" @click="load"><PortalIcon name="refresh" :size="15" />{{ t('app.refresh') }}</button><button class="primary" :disabled="busy" @click="add = true"><PortalIcon name="plus" :size="15" />{{ t('workspace.addProxy') }}</button></div></div>
    <RequestError :error="error" />
    <section><div class="proxy-toolbar"><div v-for="(count,index) in [rows.filter(p => p.status === 'active').length,rows.filter(p => p.status === 'disabled').length,rows.length]" :key="index" class="proxy-metric"><small>{{ t(`workspace.${['validProxies','invalidProxies','totalProxies'][index]}`) }}</small><b>{{ loading || error ? '—' : count }}</b></div></div>
      <div class="workspace-table-scroll"><table><thead><tr><th v-for="key in ['proxyName','proxyAddress','region','proxyStatus','boundAccounts','actions']" :key="key">{{ t(`workspace.columnsMap.${key}`) }}</th></tr></thead><tbody><tr v-for="proxy in rows" :key="proxy.id"><td>{{ proxy.name }}</td><td>{{ proxy.address }}</td><td>{{ proxy.region }}</td><td><span class="status-badge" :class="proxy.status === 'active' ? 'green' : 'gray'">{{ t(`onboarding.proxyStates.${proxy.status}`) }}</span></td><td>{{ proxy.accounts }} + {{ proxy.held }} / {{ proxy.capacity }}</td><td><button :disabled="busy || !['active','disabled'].includes(proxy.status)" @click="test(proxy)">{{ t('onboarding.testProxy') }}</button> <button :disabled="busy || proxy.accounts > 0 || proxy.held > 0 || proxy.status === 'provisioning'" :title="t('onboarding.deleteProxyHint')" @click="requestDelete(proxy)">{{ t('onboarding.deleteProxy') }}</button></td></tr><tr v-if="loading || error || !rows.length"><td colspan="6" class="table-empty"><PortalIcon name="proxies" :size="32" /><b>{{ t(loading ? 'onboarding.loading' : error ? 'onboarding.proxyLoadFailed' : 'onboarding.emptyProxies') }}</b></td></tr></tbody></table></div><div class="workspace-pagination"><span>{{ t('onboarding.capacityHint') }}</span></div>
    </section>
    <PortalDialog v-if="add" :title="t('workspace.addProxy')" :footer="false" @close="!busy && (add = false)"><ProxyCreateForm :client="client" @saved="load" @created="created" @busy="value => busy = value" /><div class="dialog-footer"><button :disabled="busy" @click="add = false">{{ t('workspace.close') }}</button></div></PortalDialog>
    <PortalDialog v-if="removing" :title="t('onboarding.deleteProxy')" :footer="false" @close="!busy && (removing = null)"><p>{{ t('onboarding.deleteProxyConfirm', {name:removing.name}) }}</p><RequestError :error="deleteError" /><div class="dialog-footer"><button :disabled="busy" @click="removing = null">{{ t('accounts.cancel') }}</button><button class="primary" :disabled="busy" @click="remove">{{ t(busy ? 'onboarding.deletingProxy' : 'onboarding.confirmDeleteProxy') }}</button></div></PortalDialog>
  </div>
</template>
