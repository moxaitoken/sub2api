<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { session } from '../composables/useSession'
const { t } = useI18n()
</script>

<template>
  <section v-if="session.identity.value" class="card">
    <h1>{{ t('identity.title') }}</h1>
    <dl>
      <dt>{{ t('identity.email') }}</dt><dd>{{ session.identity.value.user.email }}</dd>
      <dt>{{ t('identity.supplier') }}</dt><dd>{{ session.identity.value.supplier.name }}</dd>
      <dt>{{ t('identity.portalStatus') }}</dt><dd>{{ t(`identity.${session.identity.value.supplier.portal_status}`) }}</dd>
    </dl>
  </section>
</template>
