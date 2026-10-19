<template>
  <BaseDialog :show="show" :title="account ? t('supplier.auth.reauthTitle', { name: account.name }) : t('supplier.auth.addTitle')" width="wide" @close="close">
    <div class="space-y-4">
      <p class="rounded-lg bg-amber-50 p-3 text-sm text-amber-800 dark:bg-amber-900/20 dark:text-amber-200">
        {{ t('supplier.auth.reviewNotice') }}
      </p>

      <div v-if="!account">
        <label class="input-label">{{ t('supplier.auth.name') }}</label>
        <input v-model="form.name" type="text" class="input" :placeholder="t('supplier.auth.namePlaceholder')" />
      </div>

      <div class="grid gap-4 sm:grid-cols-2">
        <div>
          <label class="input-label">{{ t('supplier.auth.mode') }}</label>
          <select v-model="form.mode" class="input" :disabled="!!authUrl">
            <option value="oauth">{{ t('supplier.auth.modes.oauth') }}</option>
            <option value="setup-token">{{ t('supplier.auth.modes.setupToken') }}</option>
          </select>
        </div>
        <div>
          <label class="input-label">{{ t('supplier.auth.proxy') }}</label>
          <select v-model.number="form.proxyId" class="input" :disabled="!!authUrl">
            <option :value="0">{{ t('supplier.auth.noProxy') }}</option>
            <option v-for="p in proxies" :key="p.id" :value="p.id">{{ p.name }} ({{ p.host }}:{{ p.port }})</option>
          </select>
        </div>
      </div>

      <div class="flex gap-2">
        <button class="btn btn-sm" :class="method === 'browser' ? 'btn-primary' : 'btn-secondary'" @click="method = 'browser'">
          {{ t('supplier.auth.methods.browser') }}
        </button>
        <button class="btn btn-sm" :class="method === 'sessionKey' ? 'btn-primary' : 'btn-secondary'" @click="method = 'sessionKey'">
          {{ t('supplier.auth.methods.sessionKey') }}
        </button>
      </div>

      <div v-if="method === 'browser'" class="space-y-3">
        <button v-if="!authUrl" class="btn btn-secondary" :disabled="busy" @click="start">
          {{ t('supplier.auth.generateUrl') }}
        </button>
        <template v-else>
          <div>
            <label class="input-label">{{ t('supplier.auth.step1') }}</label>
            <div class="flex gap-2">
              <input :value="authUrl" readonly class="input font-mono text-xs" />
              <a :href="authUrl" target="_blank" rel="noopener noreferrer" class="btn btn-secondary">{{ t('supplier.auth.open') }}</a>
            </div>
          </div>
          <div>
            <label class="input-label">{{ t('supplier.auth.step2') }}</label>
            <input v-model="form.code" type="text" class="input font-mono" :placeholder="t('supplier.auth.codePlaceholder')" />
          </div>
        </template>
      </div>

      <div v-else>
        <label class="input-label">{{ t('supplier.auth.sessionKey') }}</label>
        <input v-model="form.sessionKey" type="password" autocomplete="off" class="input font-mono" placeholder="sk-ant-sid01-..." />
        <p class="mt-1 text-xs text-gray-500">{{ t('supplier.auth.sessionKeyHint') }}</p>
      </div>
    </div>

    <template #footer>
      <div class="flex justify-end gap-2">
        <button class="btn btn-secondary" @click="close">{{ t('common.cancel') }}</button>
        <button class="btn btn-primary" :disabled="!canSubmit || busy" @click="submit">
          {{ account ? t('supplier.auth.reauthorize') : t('supplier.auth.create') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import { useAppStore } from '@/stores/app'
import BaseDialog from '@/components/common/BaseDialog.vue'
import { cookieAuth, exchangeCode, generateAuthUrl } from '../api'
import { extractAuthCode } from '../format'
import type { AuthMode, SupplierAccount, SupplierProxy } from '../types'

const props = defineProps<{
  show: boolean
  proxies: SupplierProxy[]
  /** When set, re-authorizes this account instead of creating a new one. */
  account?: SupplierAccount | null
}>()
const emit = defineEmits<{ (e: 'close'): void; (e: 'saved', account: SupplierAccount): void }>()

const { t } = useI18n()
const appStore = useAppStore()

const method = ref<'browser' | 'sessionKey'>('browser')
const authUrl = ref('')
const sessionId = ref('')
const busy = ref(false)
const form = reactive({
  name: '',
  mode: 'oauth' as AuthMode,
  proxyId: 0,
  code: '',
  sessionKey: '',
})

watch(
  () => props.show,
  (visible) => {
    if (!visible) return
    method.value = 'browser'
    authUrl.value = ''
    sessionId.value = ''
    Object.assign(form, {
      name: '',
      mode: props.account?.type === 'setup-token' ? 'setup-token' : 'oauth',
      proxyId: props.account?.proxy_id ?? 0,
      code: '',
      sessionKey: '',
    })
  },
)

const canSubmit = computed(() => {
  if (!props.account && !form.name.trim()) return false
  return method.value === 'browser' ? !!authUrl.value && !!form.code.trim() : !!form.sessionKey.trim()
})

async function start() {
  busy.value = true
  try {
    const result = await generateAuthUrl(form.mode, form.proxyId || null)
    authUrl.value = result.auth_url
    sessionId.value = result.session_id
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.auth.failed'))
  } finally {
    busy.value = false
  }
}

async function submit() {
  busy.value = true
  try {
    const common = {
      name: form.name.trim() || undefined,
      proxy_id: form.proxyId || null,
      account_id: props.account?.id,
    }
    const saved =
      method.value === 'browser'
        ? await exchangeCode({ ...common, session_id: sessionId.value, code: extractAuthCode(form.code) })
        : await cookieAuth({ ...common, mode: form.mode, session_key: form.sessionKey.trim() })
    appStore.showSuccess(props.account ? t('supplier.auth.reauthorized') : t('supplier.auth.created'))
    emit('saved', saved)
    close()
  } catch (err: any) {
    appStore.showError(err?.message || t('supplier.auth.failed'))
  } finally {
    busy.value = false
  }
}

function close() {
  emit('close')
}
</script>
