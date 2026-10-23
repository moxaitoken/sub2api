<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { ApiError, SupplierClient } from './api/client'
import type { PortalIdentity, ManagedSupplier, SupplierAccount } from './api/types'
import PortalLoginView from './views/PortalLoginView.vue'
import RequestError from './components/RequestError.vue'
import PortalIcon from './components/PortalIcon.vue'
import SupplierWorkspaceView from './views/SupplierWorkspaceView.vue'
import AccountOnboardingView from './views/AccountOnboardingView.vue'
import OwnProxiesView from './views/OwnProxiesView.vue'
const { t, locale } = useI18n()
const administrator = new URLSearchParams(window.location.search).get('portal') === 'admin'
const identity = ref<PortalIdentity | null>(null)
const addAccountOpen = ref(false)
const loading = ref(true), busy = ref(false), error = ref<unknown>(null)
const tab = ref('accounts'), suppliers = ref<ManagedSupplier[]>([]), total = ref(0), page = ref(1), search = ref('')
const selected = ref<ManagedSupplier | null>(null), accounts = ref<SupplierAccount[]>([])
const dialog = ref<'create' | 'reset' | 'details' | null>(null), dialogError = ref<unknown>(null)
const name = ref(''), login = ref(''), password = ref(''), dialogKey = ref('')
const accountTotal = ref<number | null>(null), accountPage = ref(1), accountPages = ref(0), accountLoading = ref(false), accountsFailed = ref(false), updatedAt = ref('')
let accountGeneration = 0
let generation = 0
const actionKeys = new Map<string, string>()
const client = new SupplierClient(() => { identity.value = null; addAccountOpen.value = false; selected.value = null; suppliers.value = []; accounts.value = []; accountTotal.value = null; updatedAt.value = ''; accountGeneration++ }, (...args) => fetch(...args), administrator ? 'admin' : 'supplier')
async function loadSuppliers(next = page.value) {
  const expected = ++generation
  const data = await client.suppliers(search.value.trim(), next)
  if (expected !== generation) return
  suppliers.value = data.items; total.value = data.total; page.value = data.page
}
async function loadAccounts(next = 1) {
  const expected = ++accountGeneration, owner = identity.value, supplierID = selected.value?.id
  accountLoading.value = true; accountsFailed.value = false; accounts.value = []
  try {
    const data = await client.scopedAccounts(administrator ? supplierID : undefined, next)
    if (expected !== accountGeneration || identity.value !== owner || selected.value?.id !== supplierID) return
    accounts.value = data.items; accountTotal.value = data.total; accountPage.value = data.page; accountPages.value = data.pages; updatedAt.value = new Date().toISOString()
  } catch (cause) {
    if (expected === accountGeneration) { accountsFailed.value = true; accountTotal.value = null; updatedAt.value = '' }
    throw cause
  } finally { if (expected === accountGeneration) accountLoading.value = false }
}
async function refresh(initial = false) {
  loading.value = initial; error.value = null
  try {
    identity.value = await client.portalMe()
    if (administrator && !selected.value && tab.value !== 'finance') await loadSuppliers()
    else if (tab.value === 'accounts') await loadAccounts()
  } catch (cause) { if (!(initial && cause instanceof ApiError && cause.status === 401)) error.value = cause }
  finally { loading.value = false }
}
async function run(action: () => Promise<void>) {
  if (busy.value) return
  busy.value = true; error.value = null
  try { await action() } catch (cause) { error.value = cause }
  finally { busy.value = false }
}
async function logout() { await run(async () => { await client.logout(); client.clearSession(); identity.value = null; addAccountOpen.value = false; selected.value = null; accounts.value = []; suppliers.value = [] }) }
async function chooseSupplier(row: ManagedSupplier) { await run(async () => { selected.value = row; tab.value = 'accounts'; await loadAccounts() }) }
function openCreate() { name.value = ''; login.value = ''; password.value = ''; dialogKey.value = crypto.randomUUID(); dialogError.value = null; dialog.value = 'create' }
function openDetails(row: ManagedSupplier) { selected.value = row; password.value = ''; dialogError.value = null; dialog.value = 'details' }
function openReset() { password.value = ''; dialogKey.value = crypto.randomUUID(); dialogError.value = null; dialog.value = 'reset' }
async function submit() {
  if (busy.value) return
  busy.value = true; dialogError.value = null
  try {
    const promise = dialog.value === 'create' ? client.createSupplier(name.value.trim(), login.value.trim(), password.value, dialogKey.value)
      : client.resetSupplier(selected.value!, password.value, dialogKey.value)
    password.value = ''
    await promise; dialog.value = null; selected.value = null; await loadSuppliers(1)
  } catch (cause) { dialogError.value = cause }
  finally { busy.value = false }
}
async function toggle(row: ManagedSupplier) {
  await run(async () => {
    const id = `${row.id}:${row.version}:status`; const key = actionKeys.get(id) || crypto.randomUUID(); actionKeys.set(id, key)
    await client.setSupplierStatus(row, row.portal_status !== 'active', key); await loadSuppliers()
  })
}
async function switchTab(value: string) {
  tab.value = value
  if (value === 'accounts' && (!administrator || selected.value)) await run(loadAccounts)
}
onMounted(() => refresh(true))
</script>
<template>
  <div :class="{ 'supplier-shell': identity && !administrator, 'supplier-entry': !identity && !administrator }">
  <aside v-if="identity && !administrator" class="supplier-sidebar">
    <div class="brand"><PortalIcon name="brand" :size="27" /><b>Goccmax</b><span>{{ t('workspace.supplyPlatform') }}</span></div>
    <nav :aria-label="t('app.title')"><div class="nav-group">{{ t('workspace.resources') }}</div><button v-for="item in ['accounts','proxies']" :key="item" :class="{ active: tab === item }" :aria-label="t(item === 'accounts' ? 'accounts.title' : 'portal.proxies')" :aria-current="tab === item ? 'page' : undefined" @click="switchTab(item)"><PortalIcon :name="item" /><span>{{ t(item === 'accounts' ? 'accounts.title' : 'portal.proxies') }}</span><PortalIcon v-if="tab === item" class="push" name="chevron" :size="14" /></button><div class="nav-group statistics-label">{{ t('workspace.statistics') }}</div><button :class="{ active: tab === 'usage' }" :aria-label="t('portal.usage')" :aria-current="tab === 'usage' ? 'page' : undefined" @click="switchTab('usage')"><PortalIcon name="usage" /><span>{{ t('portal.usage') }}</span><PortalIcon v-if="tab === 'usage'" class="push" name="chevron" :size="14" /></button></nav>
    <div class="supplier-sidebar-bottom"><PortalIcon name="user" /><div><b>{{ identity.supplier?.name }}</b><small>{{ identity.principal.login_account }}</small></div></div>
  </aside>
  <div :class="{ 'supplier-body': identity && !administrator }">
  <header class="topbar" :class="{ 'supplier-header': identity && !administrator }"><template v-if="identity && !administrator"><div class="breadcrumb">Goccmax <PortalIcon name="chevron" :size="12" />{{ t('workspace.supplierWorkspace') }}<PortalIcon name="chevron" :size="12" /><b>{{ t(tab === 'accounts' ? 'accounts.title' : `portal.${tab}`) }}</b></div></template><template v-else><div><strong>{{ administrator ? t('app.title') : 'Goccmax' }}</strong><small>{{ t(administrator ? 'portal.stage' : 'workspace.supplyPlatform') }}</small></div></template>
    <div class="toolbar"><select v-model="locale" :aria-label="t('app.language')"><option value="zh">简体中文</option><option value="en">English</option></select><button v-if="identity" :disabled="busy" @click="logout">{{ t('app.logout') }}</button></div>
  </header>
  <main :class="{ 'supplier-main': identity && !administrator }">
    <p v-if="loading" role="status">{{ t('app.loading') }}</p>
    <template v-else-if="!identity"><RequestError :error="error" /><PortalLoginView :client="client" :administrator="administrator" @authenticated="refresh()" /></template>
    <template v-else>
      <RequestError :error="error" />
      <nav v-if="administrator" :aria-label="t('portal.management')"><button @click="selected = null; dialog = null; tab = 'accounts'; run(() => loadSuppliers())">{{ t('portal.suppliers') }}</button><button @click="selected = null; tab = 'finance'">{{ t('portal.finance') }}</button></nav>
      <OwnProxiesView v-if="!administrator && tab === 'proxies'" :client="client" />
      <SupplierWorkspaceView v-else-if="!administrator" :client="client" :tab="tab" :accounts="accounts" :total="accountTotal" :page="accountPage" :pages="accountPages" :loading="accountLoading" :updated-at="updatedAt" :failed="accountsFailed" @refresh="next => run(() => loadAccounts(next))" @add="addAccountOpen = true" />
      <section v-else-if="administrator && !selected && tab !== 'finance'" class="card">
        <div class="actions"><h1>{{ t('portal.suppliers') }}</h1><button class="primary" :disabled="busy" @click="openCreate">{{ t('portal.create') }}</button></div>
        <form class="actions" @submit.prevent="run(() => loadSuppliers(1))"><input v-model="search" :placeholder="t('portal.search')" maxlength="100" /><button :disabled="busy">{{ t('portal.searchButton') }}</button><button type="button" @click="search = ''; run(() => loadSuppliers(1))">{{ t('portal.reset') }}</button></form>
        <div class="table-wrap"><table><thead><tr><th>{{ t('identity.supplier') }}</th><th>{{ t('portal.loginAccount') }}</th><th>{{ t('portal.status') }}</th><th>{{ t('portal.unsettled') }}</th><th>{{ t('portal.settled') }}</th><th>{{ t('portal.total') }}</th><th>{{ t('portal.business') }}</th><th>{{ t('accounts.actions') }}</th></tr></thead>
          <tbody><tr v-for="row in suppliers" :key="row.id"><td>{{ row.name }}</td><td>{{ row.login_account || '—' }}</td><td>{{ t(row.portal_status === 'active' ? 'identity.active' : 'identity.disabled') }}</td><td>{{ row.unsettled_usd }}</td><td>{{ row.settled_usd }}</td><td>{{ row.total_consumption_usd }}</td><td><button :disabled="busy" @click="chooseSupplier(row)">{{ t('accounts.title') }}</button></td><td><button :disabled="busy" @click="openDetails(row)">{{ t('portal.details') }}</button><button :disabled="busy" @click="toggle(row)">{{ t(row.portal_status === 'active' ? 'portal.disable' : 'portal.enable') }}</button></td></tr></tbody></table></div>
        <p v-if="!suppliers.length">{{ t('portal.empty') }}</p><div class="actions"><span>{{ total }}</span><button :disabled="busy || page <= 1" @click="run(() => loadSuppliers(page - 1))">{{ t('accounts.previous') }}</button><button :disabled="busy || page * 20 >= total" @click="run(() => loadSuppliers(page + 1))">{{ t('accounts.next') }}</button></div>
      </section>
      <section v-else-if="tab === 'finance'" class="card"><h1>{{ t('portal.finance') }}</h1><p>{{ t('portal.pending') }}</p></section>
      <section v-else class="card">
        <template v-if="administrator && selected"><button @click="selected = null; run(() => loadSuppliers())">{{ t('portal.back') }}</button><h1>{{ selected.name }} · {{ selected.login_account }}</h1><div class="actions"><button @click="switchTab('accounts')">{{ t('accounts.title') }}</button><button @click="switchTab('proxies')">{{ t('portal.proxies') }}</button><button @click="switchTab('usage')">{{ t('portal.usage') }}</button></div></template>
        <h1 v-else>{{ identity.supplier?.name }}</h1>
        <template v-if="tab === 'accounts'"><h2>{{ t('accounts.title') }}</h2><p class="muted">{{ t('portal.accountStage') }}</p><button :disabled="busy" @click="run(loadAccounts)">{{ t('app.refresh') }}</button><table><thead><tr><th>{{ t('accounts.name') }}</th><th>{{ t('accounts.state') }}</th></tr></thead><tbody><tr v-for="account in accounts" :key="account.id"><td>{{ account.name }}</td><td>{{ t(`accounts.states.${account.admission_state}`) }}</td></tr></tbody></table><p v-if="!accounts.length">{{ t('portal.noAccounts') }}</p></template>
        <p v-else>{{ t('portal.pending') }}</p>
      </section>
      <AccountOnboardingView v-if="!administrator && addAccountOpen" :client="client" @close="addAccountOpen = false" @created="run(() => loadAccounts(1))" />
      <div v-if="dialog" class="portal-overlay"><section class="card portal-dialog" role="dialog" aria-modal="true" :aria-label="t(dialog === 'create' ? 'portal.create' : dialog === 'reset' ? 'portal.passwordReset' : 'portal.details')">
        <h2>{{ t(dialog === 'create' ? 'portal.create' : dialog === 'reset' ? 'portal.passwordReset' : 'portal.details') }}</h2><RequestError :error="dialogError" />
        <template v-if="dialog === 'details' && selected"><p>{{ selected.name }} · {{ selected.login_account }}</p><button :disabled="busy || !selected.login_account" @click="openReset">{{ t('portal.passwordReset') }}</button></template>
        <form v-else @submit.prevent="submit"><template v-if="dialog === 'create'"><label for="supplier-name">{{ t('identity.supplier') }}</label><input id="supplier-name" v-model="name" required maxlength="100" /><label for="supplier-login">{{ t('portal.loginAccount') }}</label><input id="supplier-login" v-model="login" required minlength="4" maxlength="64" pattern="[A-Za-z0-9_.@-]+" autocomplete="off" /></template><label for="supplier-password">{{ t('portal.newPassword') }}</label><input id="supplier-password" v-model="password" type="password" required minlength="10" maxlength="64" autocomplete="new-password" /><p class="muted">{{ t('portal.passwordHint') }}</p><button class="primary" :disabled="busy">{{ t('portal.save') }}</button></form>
        <button :disabled="busy" @click="dialog = null; password = ''; selected = null">{{ t('accounts.cancel') }}</button>
      </section></div>
    </template>
    <footer v-if="identity && !administrator" class="supplier-footer"><span>Goccmax / {{ t('workspace.supplierWorkspace') }}</span><span>{{ t('workspace.ownScope') }}</span></footer>
  </main>
  </div></div>
</template>
<style scoped>
.portal-overlay{position:fixed;inset:0;background:#0005;display:grid;place-items:center;padding:20px;z-index:5}.portal-dialog{width:min(460px,100%)}.portal-dialog label{display:block;margin:12px 0 6px}.portal-dialog input{width:100%}.table-wrap{overflow:auto}th,td{white-space:nowrap}.actions h1{margin-right:auto}nav{margin-bottom:20px}
</style>
