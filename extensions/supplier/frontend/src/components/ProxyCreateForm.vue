<script setup lang="ts">
import { ref } from 'vue'
import { useSupplierI18n } from '../i18n/useSupplierI18n'
import type { SupplierClient } from '../api/client'
import type { OwnProxy } from '../api/types'
import RequestError from './RequestError.vue'
const props = defineProps<{ client: SupplierClient; inline?: boolean }>()
const emit = defineEmits<{ created: [proxy: OwnProxy]; saved: [proxy: OwnProxy]; busy: [busy: boolean] }>()
const { t } = useSupplierI18n()
const name = ref(''), address = ref(''), region = ref(''), busy = ref(false), error = ref<unknown>(null)
const key = ref(crypto.randomUUID())
const saved = ref<OwnProxy | null>(null)
const regions = ['US','CA','IT','GB','DE','FR','JP','KR','SG','AE','AU','VN','IN','CH','MY','TH','TW','BR']
async function submit() {
  if (busy.value || (saved.value && saved.value.status !== 'disabled')) return
  busy.value = true; emit('busy', true); error.value = null
  try {
    const request = saved.value
      ? props.client.testOwnProxy(saved.value.id)
      : props.client.addProxy({ name: name.value.trim(), address: address.value.trim(), region: region.value }, key.value)
    address.value = ''
    const proxy = await request
    saved.value = proxy
    if (proxy.status === 'active') { key.value = crypto.randomUUID(); emit('created', proxy) }
    else emit('saved', proxy)
  } catch (cause) { error.value = cause }
  finally { busy.value = false; emit('busy', false) }
}
</script>
<template>
  <form class="onboarding-form proxy-create-form" @submit.prevent="submit">
    <RequestError :error="error" />
    <p v-if="saved && saved.status !== 'active'" class="onboarding-warning" role="alert">{{ t(saved.status === 'disabled' ? 'onboarding.proxySavedInvalid' : 'onboarding.proxySavedUncertain') }}</p>
    <label>{{ t('onboarding.proxyName') }}<input v-model="name" required maxlength="100" :disabled="busy || !!saved" autocomplete="off" /></label>
    <label v-if="!saved">{{ t('onboarding.proxyAddress') }}<input v-model="address" required maxlength="8192" :disabled="busy" placeholder="http://user:password@host:port" autocomplete="off" spellcheck="false" /></label>
    <p v-else class="onboarding-hint">{{ saved.address }}</p>
    <p class="onboarding-hint">{{ t('onboarding.proxyHint') }}</p>
    <label>{{ t('onboarding.region') }}<select v-model="region" :required="!inline" :disabled="busy || !!saved"><option value="">{{ t(inline ? 'onboarding.unknownRegion' : 'onboarding.chooseRegion') }}</option><option v-for="code in regions" :key="code" :value="code">{{ code === 'AE' ? t('onboarding.dubai') : code }}</option></select></label>
    <button class="primary" :disabled="busy || (!!saved && saved.status !== 'disabled')">{{ t(busy ? 'onboarding.testing' : saved ? 'onboarding.retestSavedProxy' : 'onboarding.createProxy') }}</button>
  </form>
</template>
