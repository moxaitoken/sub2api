<template>
  <div class="space-y-4">
    <div class="flex items-center justify-between gap-3">
      <p class="text-sm text-gray-500 dark:text-dark-300">{{ t('supplier.proxies.hint') }}</p>
      <div class="flex gap-2">
        <button class="btn btn-secondary" :disabled="loading" @click="load">
          <Icon name="refresh" size="md" :class="loading ? 'animate-spin' : ''" />
        </button>
        <button class="btn btn-primary" @click="openForm(null)">
          <Icon name="plus" size="md" class="mr-1" />{{ t('supplier.proxies.add') }}
        </button>
      </div>
    </div>

    <div class="card table-container">
      <table class="table">
        <thead>
          <tr>
            <th>{{ t('supplier.proxies.columns.name') }}</th>
            <th>{{ t('supplier.proxies.columns.address') }}</th>
            <th>{{ t('supplier.proxies.columns.status') }}</th>
            <th class="!text-right">{{ t('supplier.proxies.columns.accounts') }}</th>
            <th>{{ t('supplier.proxies.columns.lastTest') }}</th>
            <th class="!text-right">{{ t('supplier.common.actions') }}</th>
          </tr>
        </thead>
        <tbody>
          <tr v-if="!proxies.length">
            <td colspan="6" class="text-center text-gray-400">{{ loading ? t('supplier.common.loading') : t('supplier.common.empty') }}</td>
          </tr>
          <tr v-for="p in proxies" :key="p.id">
            <td class="font-medium">{{ p.name }}</td>
            <td class="font-mono text-xs">{{ p.protocol }}://{{ p.username ? p.username + '@' : '' }}{{ p.host }}:{{ p.port }}</td>
            <td>{{ p.status }}</td>
            <td class="text-right">{{ p.account_count }}</td>
            <td class="text-xs">
              <template v-if="tests[p.id]">
                <span :class="tests[p.id].success ? 'text-green-600' : 'text-red-600'">
                  {{ tests[p.id].success ? `${tests[p.id].latency_ms ?? '-'} ms · ${tests[p.id].ip_address ?? ''} ${tests[p.id].country ?? ''}` : tests[p.id].message }}
                </span>
              </template>
              <span v-else>-</span>
            </td>
            <td class="text-right whitespace-nowrap">
              <button class="btn btn-ghost btn-sm" :disabled="testing === p.id" @click="runTest(p)">{{ t('supplier.proxies.test') }}</button>
              <button class="btn btn-ghost btn-sm" @click="openForm(p)">{{ t('supplier.common.edit') }}</button>
              <button class="btn btn-ghost btn-sm text-red-600" @click="deleting = p">{{ t('supplier.common.delete') }}</button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <BaseDialog :show="formOpen" :title="editing ? t('supplier.proxies.editTitle') : t('supplier.proxies.addTitle')" @close="formOpen = false">
      <div class="space-y-4">
        <div>
          <label class="input-label">{{ t('supplier.proxies.columns.name') }}</label>
          <input v-model="form.name" type="text" class="input" />
        </div>
        <div class="grid grid-cols-3 gap-3">
          <div>
            <label class="input-label">{{ t('supplier.proxies.protocol') }}</label>
            <select v-model="form.protocol" class="input">
              <option v-for="p in protocols" :key="p" :value="p">{{ p }}</option>
            </select>
          </div>
          <div>
            <label class="input-label">{{ t('supplier.proxies.host') }}</label>
            <input v-model="form.host" type="text" class="input" placeholder="203.0.113.10" />
          </div>
          <div>
            <label class="input-label">{{ t('supplier.proxies.port') }}</label>
            <input v-model.number="form.port" type="number" min="1" max="65535" class="input" />
          </div>
        </div>
        <div class="grid grid-cols-2 gap-3">
          <div>
            <label class="input-label">{{ t('supplier.proxies.username') }}</label>
            <input v-model="form.username" type="text" autocomplete="off" class="input" />
          </div>
          <div>
            <label class="input-label">{{ t('supplier.proxies.password') }}</label>
            <input
              v-model="form.password"
              type="password"
              autocomplete="new-password"
              class="input"
              :placeholder="editing?.has_password ? t('supplier.proxies.passwordKeep') : ''"
            />
          </div>
        </div>
        <p class="text-xs text-gray-500">{{ t('supplier.proxies.publicOnly') }}</p>
      </div>
      <template #footer>
        <div class="flex justify-end gap-2">
          <button class="btn btn-secondary" @click="formOpen = false">{{ t('common.cancel') }}</button>
          <button class="btn btn-primary" :disabled="saving || !formValid" @click="save">{{ t('common.save') }}</button>
        </div>
      </template>
    </BaseDialog>

    <ConfirmDialog
      :show="!!deleting"
      :title="t('supplier.proxies.deleteTitle')"
      :message="t('supplier.proxies.deleteMessage', { name: deleting?.name ?? '' })"
      danger
      @confirm="confirmDelete"
      @cancel="deleting = null"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'
import ConfirmDialog from '@/components/common/ConfirmDialog.vue'
import Icon from '@/components/icons/Icon.vue'
import { createProxy, deleteProxy, listProxies, testProxy, updateProxy } from '../api'
import type { ProxyPayload, ProxyTestResult, SupplierProxy } from '../types'

const { t } = useI18n()
const appStore = useAppStore()

const protocols = ['socks5', 'socks5h', 'http', 'https']
const proxies = ref<SupplierProxy[]>([])
const tests = ref<Record<number, ProxyTestResult>>({})
const loading = ref(false)
const saving = ref(false)
const testing = ref<number | null>(null)
const formOpen = ref(false)
const editing = ref<SupplierProxy | null>(null)
const deleting = ref<SupplierProxy | null>(null)
const form = reactive({ name: '', protocol: 'socks5', host: '', port: 1080, username: '', password: '' })

const formValid = computed(() => !!form.name.trim() && !!form.host.trim() && form.port > 0 && form.port <= 65535)

async function load() {
  loading.value = true
  try {
    proxies.value = await listProxies()
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.loadFailed'))
  } finally {
    loading.value = false
  }
}

function openForm(p: SupplierProxy | null) {
  editing.value = p
  Object.assign(form, {
    name: p?.name ?? '',
    protocol: p?.protocol ?? 'socks5',
    host: p?.host ?? '',
    port: p?.port ?? 1080,
    username: p?.username ?? '',
    password: '',
  })
  formOpen.value = true
}

async function save() {
  saving.value = true
  const payload: ProxyPayload = {
    name: form.name.trim(),
    protocol: form.protocol,
    host: form.host.trim(),
    port: form.port,
    username: form.username.trim(),
  }
  // An empty password on edit keeps the stored one.
  if (form.password || !editing.value) payload.password = form.password
  try {
    if (editing.value) await updateProxy(editing.value.id, payload)
    else await createProxy(payload)
    formOpen.value = false
    await load()
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.saveFailed'))
  } finally {
    saving.value = false
  }
}

async function runTest(p: SupplierProxy) {
  testing.value = p.id
  try {
    tests.value = { ...tests.value, [p.id]: await testProxy(p.id) }
  } catch (err: any) {
    tests.value = { ...tests.value, [p.id]: { success: false, message: err?.message || 'error' } }
  } finally {
    testing.value = null
  }
}

async function confirmDelete() {
  if (!deleting.value) return
  try {
    await deleteProxy(deleting.value.id)
    deleting.value = null
    await load()
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.common.saveFailed'))
  }
}

onMounted(load)
</script>
