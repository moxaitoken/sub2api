<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useSupplierI18n } from '../i18n/useSupplierI18n'
import PortalIcon from './PortalIcon.vue'
defineProps<{ title: string; footer?: boolean }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useSupplierI18n()
const element = ref<HTMLElement | null>(null)
let previous: HTMLElement | null = null
function keydown(event: KeyboardEvent) {
  if (event.key === 'Escape') { event.preventDefault(); emit('close') }
  if (event.key !== 'Tab') return
  const controls = [...(element.value?.querySelectorAll<HTMLElement>('button:not(:disabled), input:not(:disabled), select:not(:disabled), a[href], [tabindex="0"]') || [])]
  const first = controls[0], last = controls.at(-1)
  if (!first || !last) { event.preventDefault(); element.value?.focus(); return }
  if (event.shiftKey && (document.activeElement === first || document.activeElement === element.value)) { event.preventDefault(); last.focus() }
  else if (!event.shiftKey && (document.activeElement === last || document.activeElement === element.value)) { event.preventDefault(); first.focus() }
}
onMounted(() => { previous = document.activeElement as HTMLElement; element.value?.focus() })
onUnmounted(() => previous?.focus())
</script>
<template>
  <div class="workspace-overlay" @click.self="emit('close')">
    <section ref="element" class="workspace-dialog" role="dialog" aria-modal="true" :aria-label="title" tabindex="-1" @keydown="keydown">
      <button class="dialog-close icon-button" :aria-label="t('workspace.close')" @click="emit('close')"><PortalIcon name="close" /></button>
      <h2>{{ title }}</h2><slot />
      <div v-if="footer !== false" class="dialog-footer"><button @click="emit('close')">{{ t('workspace.close') }}</button></div>
    </section>
  </div>
</template>
