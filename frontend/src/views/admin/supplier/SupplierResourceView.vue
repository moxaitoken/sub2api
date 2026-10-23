<script setup lang="ts">
import { computed, ref, watch, onUnmounted } from 'vue'
import { useRoute } from 'vue-router'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import { supplierAdminAPI, supplierResourceClient, type ManagedSupplier } from '@/api/admin/suppliers'
import SupplierWorkspaceView from '../../../../../extensions/supplier/frontend/src/views/SupplierWorkspaceView.vue'
import OwnProxiesView from '../../../../../extensions/supplier/frontend/src/views/OwnProxiesView.vue'
import AccountOnboardingView from '../../../../../extensions/supplier/frontend/src/views/AccountOnboardingView.vue'
import PortalDialog from '../../../../../extensions/supplier/frontend/src/components/PortalDialog.vue'
import type { SupplierAccount } from '../../../../../extensions/supplier/frontend/src/api/types'
const route = useRoute(), { t } = useI18n()
const supplier = ref<ManagedSupplier | null>(null), error = ref(''), loading = ref(false), adding = ref(false)
const accounts = ref<SupplierAccount[]>([]), total = ref<number | null>(null), page = ref(1), pages = ref(1), updatedAt = ref('')
const id = computed(() => String(route.params.id || ''))
const tab = computed(() => String(route.params.tab || 'accounts'))
const client = computed(() => supplierResourceClient(id.value))
let generation = 0
async function load(next = 1) {
  const current = ++generation, target = id.value
  loading.value = true; error.value = ''; accounts.value = []; total.value = null
  try {
    const [owner, data] = await Promise.all([supplierAdminAPI.detail(target), client.value.accounts(next)])
    if (current !== generation) return
    supplier.value = owner; accounts.value = data.items; total.value = data.total; page.value = data.page; pages.value = data.pages; updatedAt.value = new Date().toISOString()
  } catch { if (current === generation) { supplier.value = null; error.value = t('supplierAdmin.failed') } }
  finally { if (current === generation) loading.value = false }
}
watch(id, () => { supplier.value = null; adding.value = false; void load() }, { immediate: true })
onUnmounted(() => generation++)
</script>

<template>
  <AppLayout>
    <div class="mb-5 flex flex-wrap items-center justify-between gap-3">
      <div><h2 class="text-xl font-semibold">{{ supplier?.name || t('supplierAdmin.details') }}</h2><p class="text-sm text-gray-500">{{ supplier?.login_account }}</p></div>
      <RouterLink class="btn btn-secondary" to="/admin/suppliers">{{ t('supplierAdmin.back') }}</RouterLink>
    </div>
    <p v-if="error" role="alert" class="mb-4 text-red-500">{{ error }}</p>
    <template v-if="supplier">
      <nav class="mb-5 flex gap-2" :aria-label="t('supplierAdmin.business')">
        <RouterLink v-for="key in ['accounts','proxies','usage']" :key="key" :to="`/admin/suppliers/${id}/${key}`" class="btn" :class="tab === key ? 'btn-primary' : 'btn-secondary'">{{ t(`supplierAdmin.${key}`) }}</RouterLink>
      </nav>
      <div :key="id" class="supplier-business">
        <OwnProxiesView v-if="tab === 'proxies'" :client="client" />
        <SupplierWorkspaceView v-else :key="tab" :client="client" :tab="tab" :accounts="accounts" :total="total" :page="page" :pages="pages" :loading="loading" :updated-at="updatedAt" :failed="!!error" @refresh="load" @add="adding = true" />
        <PortalDialog v-if="adding" :title="t('supplierPortal.workspace.addAccount')" @close="adding = false"><AccountOnboardingView :client="client" @close="adding = false" @created="adding = false; load()" /></PortalDialog>
      </div>
    </template>
  </AppLayout>
</template>

<style>
/* Shared resource components inherit the main console theme only inside this surface. */
.supplier-business { --accent:#0d9488; font-size:14px; }
.supplier-business button,.supplier-business input,.supplier-business select,.supplier-business textarea { border:1px solid #cbd5e1; border-radius:7px; padding:8px 12px; background:#fff; color:#334155; font:inherit; }
.supplier-business button { cursor:pointer; display:inline-flex; align-items:center; justify-content:center; gap:6px; }
.supplier-business button:disabled { opacity:.5; cursor:not-allowed; }
.supplier-business .primary { background:#0d9488; color:white; border-color:#0d9488; }
.supplier-business h1 { font-size:22px; font-weight:600; }.supplier-business p,.supplier-business small { color:#64748b; }
.supplier-business .workspace-heading,.supplier-business .section-heading,.supplier-business .heading-actions,.supplier-business .overview-heading,.supplier-business .account-filters,.supplier-business .pagination,.supplier-business .table-context,.supplier-business .proxy-stats { display:flex; gap:12px; align-items:center; flex-wrap:wrap; }
.supplier-business .workspace-heading,.supplier-business .section-heading { justify-content:space-between; margin-bottom:20px; }
.supplier-business .account-overview,.supplier-business .account-list,.supplier-business .proxy-table,.supplier-business .card,.supplier-business .usage-card { background:#fff; border:1px solid #e2e8f0; border-radius:12px; padding:18px; margin-bottom:18px; }
.supplier-business .overview-metrics,.supplier-business .quota-windows,.supplier-business .usage-metrics { display:flex; flex-wrap:wrap; gap:24px; padding:16px 0; }
.supplier-business .overview-metrics>div { flex:1; min-width:95px; }.supplier-business .overview-metrics strong { display:block; font-size:24px; }
.supplier-business .overview-metrics small { display:block; }.supplier-business .quota-windows>div { flex:1; min-width:150px; }.supplier-business .quota-track { height:4px; background:#e2e8f0; margin:10px 0; }
.supplier-business .push,.supplier-business .overview-updated { margin-left:auto; }.supplier-business .table-scroll { overflow-x:auto; }.supplier-business table { width:100%; border-collapse:collapse; text-align:left; }.supplier-business th,.supplier-business td { padding:12px; border-bottom:1px solid #e2e8f0; white-space:nowrap; }.supplier-business th { font-size:12px; color:#64748b; }.supplier-business .empty-state { padding:40px; text-align:center; }.supplier-business .empty-state svg { margin:auto; }
.supplier-business .account-filters { padding:10px 0; }.supplier-business .filter-select,.supplier-business .search-field { display:flex; align-items:center; gap:6px; }.supplier-business .filter-select select { max-width:150px; }.supplier-business .column-options { display:flex; flex-wrap:wrap; gap:14px; padding:12px; }.supplier-business .column-options input { width:auto; }.supplier-business .table-context,.supplier-business .workspace-footnote { font-size:12px; color:#64748b; padding:12px 0; }
.supplier-business .notice,.supplier-business .scope-notice { padding:14px; background:#eff6ff; border-radius:8px; margin:12px 0; }.supplier-business .error { color:#b91c1c; background:#fef2f2; }.supplier-business .muted { color:#64748b; }.supplier-business .badge { font-size:12px; padding:3px 7px; border-radius:5px; background:#e2e8f0; }
.supplier-business form label { display:block; margin:12px 0; }.supplier-business form input,.supplier-business form select,.supplier-business textarea { display:block; width:100%; }.supplier-business .pagination { justify-content:flex-end; margin-top:14px; }.supplier-business dl { display:grid; grid-template-columns:160px 1fr; gap:10px; }.supplier-business dd { overflow-wrap:anywhere; }
.supplier-business .workspace-overlay { position:fixed; inset:0; z-index:60; background:#0f172a66; display:grid; place-items:center; padding:24px; }.supplier-business .workspace-dialog { background:white; color:#334155; border-radius:12px; padding:24px; width:min(720px,100%); max-height:90vh; overflow:auto; }.supplier-business .workspace-dialog-header,.supplier-business .dialog-footer { display:flex; align-items:center; justify-content:space-between; gap:12px; }
.dark .supplier-business button,.dark .supplier-business input,.dark .supplier-business select,.dark .supplier-business textarea,.dark .supplier-business .account-overview,.dark .supplier-business .account-list,.dark .supplier-business .proxy-table,.dark .supplier-business .card,.dark .supplier-business .usage-card,.dark .supplier-business .workspace-dialog { background:#1e293b; color:#e2e8f0; border-color:#475569; }.dark .supplier-business .primary { background:#0d9488; }.dark .supplier-business .notice { background:#164e63; }.dark .supplier-business td,.dark .supplier-business th { border-color:#334155; }
.supplier-business .dialog-close { float:right; }.supplier-business .workspace-dialog h2 { font-size:20px; font-weight:600; margin-bottom:20px; }.supplier-business .dialog-footer { justify-content:flex-end; margin-top:20px; }
.supplier-business .usage-filters { display:flex; align-items:center; gap:18px; flex-wrap:wrap; margin:22px 0; }
.supplier-business .usage-filters label { display:flex; align-items:center; gap:10px; font-size:12px; }
.supplier-business .usage-filters>span { color:#64748b; font-size:12px; }
.supplier-business .usage-cards { display:grid; grid-template-columns:repeat(4,minmax(0,1fr)); gap:16px; margin-bottom:22px; }
.supplier-business .usage-card { display:flex; flex-direction:column; gap:12px; margin-bottom:0; min-width:0; }
.supplier-business .usage-card>span { color:#64748b; font-size:12px; }
.supplier-business .usage-card strong { font-size:28px; font-weight:600; overflow-wrap:anywhere; }
.supplier-business .usage-card small { font-size:11px; }
.supplier-business .usage-panel { margin-bottom:20px; }
.supplier-business .panel-heading { display:flex; justify-content:space-between; align-items:center; flex-wrap:wrap; gap:10px; padding:18px 0; }
.supplier-business .panel-heading h2 { font-size:15px; font-weight:600; }
.supplier-business .panel-heading small { font-size:11px; }
.supplier-business .workspace-table-scroll { overflow-x:auto; }
.supplier-business .workspace-pagination { display:flex; justify-content:flex-end; align-items:center; flex-wrap:wrap; gap:12px; margin-top:16px; }
.supplier-business .table-empty { padding:36px; text-align:center; color:#64748b; }
.dark .supplier-business .usage-card>span,.dark .supplier-business .usage-filters>span,.dark .supplier-business p,.dark .supplier-business small { color:#94a3b8; }
@media(max-width:760px) { .supplier-business .usage-cards { grid-template-columns:repeat(2,minmax(0,1fr)); } }
</style>
