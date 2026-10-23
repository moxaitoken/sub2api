<script setup lang="ts">
withDefaults(defineProps<{ name: string; size?: number }>(), { size: 18 })
const paths: Record<string, string> = {
  brand: 'M4 17V7l4 7 4-7 4 7 4-7v10M4 7l8-4 8 4',
  accounts: 'M4 4h16v6H4zM4 14h16v6H4zM7 7h.01M7 17h.01M11 7h6M11 17h6',
  proxies: 'M4 4h6v6H4zM14 14h6v6h-6zM7 10v7h7M10 7h7v7',
  usage: 'M4 3v17h17M8 15v-4M13 15V6M18 15V9',
  chevron: 'm9 5 7 7-7 7',
  refresh: 'M20 7v5h-5M4 17v-5h5M6 7a7 7 0 0 1 12-1l2 6M4 12l2 6a7 7 0 0 0 12-1',
  search: 'M21 21l-5-5M18 10a8 8 0 1 1-16 0 8 8 0 0 1 16 0',
  plus: 'M12 5v14M5 12h14',
  shield: 'M12 3 4 6v6c0 5 8 9 8 9s8-4 8-9V6zM9 12l2 2 4-4',
  user: 'M20 21v-3a5 5 0 0 0-5-5H9a5 5 0 0 0-5 5v3M16 6a4 4 0 1 1-8 0 4 4 0 0 1 8 0',
  info: 'M12 11v6M12 7h.01M22 12a10 10 0 1 1-20 0 10 10 0 0 1 20 0',
  close: 'm6 6 12 12M6 18 18 6',
  columns: 'M3 4h18v16H3zM9 4v16M15 4v16',
  filter: 'M3 5h18M6 12h12M10 19h4',
  logout: 'M9 4H4v16h5M9 12h12m-4-4 4 4-4 4',
  empty: 'M3 8l4-5h10l4 5v12H3zM3 8h5l2 4h4l2-4h5',
}
</script>
<template><svg :width="size" :height="size" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path :d="paths[name] || paths.info" /></svg></template>
