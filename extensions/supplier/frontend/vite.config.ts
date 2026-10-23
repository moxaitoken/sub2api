import { defineConfig, loadEnv } from 'vite'
import vue from '@vitejs/plugin-vue'

export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), '')
  const target = env.SUPPLIER_DEV_PROXY_TARGET || 'http://127.0.0.1:8091'
  const url = new URL(target)
  // Development must stay local; this portal must never proxy to production by accident.
  if (url.protocol !== 'http:' || !['127.0.0.1', 'localhost', '[::1]'].includes(url.hostname) || url.username || url.password) {
    throw new Error('SUPPLIER_DEV_PROXY_TARGET must be a local HTTP endpoint')
  }
  return {
    plugins: [vue()],
    define: { __INTLIFY_JIT_COMPILATION__: true },
    resolve: { alias: { 'vue-i18n': 'vue-i18n/dist/vue-i18n.runtime.esm-bundler.js' } },
    server: {
      host: '127.0.0.1',
      port: 5174,
      strictPort: true,
      proxy: { '/api/supplier/v1': { target, changeOrigin: false } }
    },
    build: { outDir: 'dist', emptyOutDir: true },
    test: { environment: 'jsdom', clearMocks: true, restoreMocks: true }
  }
})
