<template>
  <AppLayout>
    <div class="space-y-6">
      <header>
        <h1 class="text-2xl font-semibold tracking-tight text-gray-950 dark:text-white">{{ t('supplier.admin.title') }}</h1>
        <p class="mt-1 text-sm text-gray-500 dark:text-dark-300">{{ t('supplier.admin.description') }}</p>
      </header>
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
          {{ t(`supplier.admin.tabs.${tab}`) }}
        </button>
      </div>
      <AdminSuppliersPanel v-if="active === 'suppliers'" />
      <AdminReviewPanel v-else-if="active === 'review'" />
      <AdminSettlementsPanel v-else />
    </div>
  </AppLayout>
</template>

<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import AppLayout from '@/components/layout/AppLayout.vue'
import AdminReviewPanel from './components/admin/AdminReviewPanel.vue'
import AdminSettlementsPanel from './components/admin/AdminSettlementsPanel.vue'
import AdminSuppliersPanel from './components/admin/AdminSuppliersPanel.vue'

const { t } = useI18n()
const tabs = ['suppliers', 'review', 'settlements'] as const
const active = ref<(typeof tabs)[number]>('suppliers')
</script>
