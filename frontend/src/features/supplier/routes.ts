import type { RouteRecordRaw } from 'vue-router'

/** Routes of the fork-local supplier module, spread into the main router. */
export const supplierRoutes: RouteRecordRaw[] = [
  {
    path: '/supplier',
    name: 'SupplierConsole',
    component: () => import('./SupplierConsoleView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: false,
      title: 'Supplier Console',
      titleKey: 'supplier.console.title',
      descriptionKey: 'supplier.console.description',
    },
  },
  {
    path: '/admin/suppliers',
    name: 'AdminSuppliers',
    component: () => import('./AdminSuppliersView.vue'),
    meta: {
      requiresAuth: true,
      requiresAdmin: true,
      title: 'Supplier Management',
      titleKey: 'supplier.admin.title',
      descriptionKey: 'supplier.admin.description',
    },
  },
]
