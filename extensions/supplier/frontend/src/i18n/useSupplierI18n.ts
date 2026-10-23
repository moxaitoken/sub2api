import { useI18n } from 'vue-i18n'
// The same views run in the standalone portal and under the main console namespace.
export function useSupplierI18n() {
  const i18n = useI18n()
  const t = (key: string, values?: Record<string, unknown>) => {
    const path = typeof i18n.te === 'function' && i18n.te('supplierPortal.app.title') ? `supplierPortal.${key}` : key
    return values ? i18n.t(path, values) : i18n.t(path)
  }
  return { t, locale: i18n.locale }
}
