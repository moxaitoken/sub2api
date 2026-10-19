<template>
  <AppLayout>
    <div class="space-y-6">
      <header>
        <h1 class="text-2xl font-semibold tracking-tight text-gray-950 dark:text-white">{{ t('supplier.console.title') }}</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">
          {{ supplier ? t('supplier.console.welcome', { name: supplier.name }) : t('supplier.console.description') }}
        </p>
      </header>

      <div v-if="state === 'loading'" class="card p-8 text-center text-gray-400">{{ t('supplier.common.loading') }}</div>
      <div v-else-if="state === 'denied'" class="card p-8 text-center">
        <p class="font-medium text-gray-900 dark:text-white">{{ t('supplier.console.notSupplier') }}</p>
        <p class="mt-1 text-sm text-gray-500">{{ t('supplier.console.notSupplierHint') }}</p>
      </div>
      <template v-else>
        <div class="flex gap-2 border-b border-gray-200 dark:border-dark-600" role="tablist">
          <button
            v-for="tab in tabs"
            :key="tab"
            role="tab"
            :aria-selected="active === tab"
            class="-mb-px border-b-2 px-4 py-2 text-sm font-medium"
            :class="active === tab ? 'border-primary-600 text-primary-600' : 'border-transparent text-gray-500 hover:text-gray-700'"
            @click="active = tab"
          >
            {{ t(`supplier.console.tabs.${tab}`) }}
          </button>
        </div>
        <UsagePanel v-if="active === 'usage'" />
        <AccountsPanel v-else-if="active === 'accounts'" />
        <ProxiesPanel v-else-if="active === 'proxies'" />
        <SettlementsPanel v-else />
      </template>
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import AccountsPanel from './components/AccountsPanel.vue'
import ProxiesPanel from './components/ProxiesPanel.vue'
import SettlementsPanel from './components/SettlementsPanel.vue'
import UsagePanel from './components/UsagePanel.vue'
import { getMe } from './api'
import type { Supplier } from './types'
import { useSupplierStatus } from './useSupplierStatus'

const { t } = useI18n()

const tabs = ['usage', 'accounts', 'proxies', 'settlements'] as const
const active = ref<(typeof tabs)[number]>('usage')
const supplier = ref<Supplier | null>(null)
const state = ref<'loading' | 'ready' | 'denied'>('loading')
const { refresh: refreshSidebarStatus } = useSupplierStatus()

onMounted(async () => {
  try {
    supplier.value = await getMe()
    state.value = 'ready'
    // Clears a stale "not a supplier" cache so the sidebar entry appears.
    void refreshSidebarStatus()
  } catch {
    state.value = 'denied'
  }
})
</script>
