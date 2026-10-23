<script setup lang="ts">
import { useI18n } from 'vue-i18n'
import { ApiError } from '../api/client'
import { errorKey } from '../api/errorText'
defineProps<{ error: unknown }>()
const { t } = useI18n()
</script>

<template>
  <div v-if="error" class="notice error" role="alert">
    <p>{{ t(errorKey(error)) }}</p>
    <small v-if="error instanceof ApiError && error.requestId">{{ t('app.requestId') }}: {{ error.requestId }}</small>
  </div>
</template>
