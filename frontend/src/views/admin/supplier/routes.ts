import type { RouteRecordRaw } from 'vue-router'

export const supplierAdminRoutes: RouteRecordRaw[] = [
  { path: '/admin/suppliers/:id/:tab(accounts|proxies|usage)', name: 'AdminSupplierResources', component: () => import('./SupplierResourceView.vue'), meta: { requiresAuth:true, requiresAdmin:true, titleKey:'supplierAdmin.resourceTitle' } },
  {
    path: '/admin/suppliers',
    name: 'AdminSuppliers',
    component: () => import('./SuppliersView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, titleKey: 'supplierAdmin.list', descriptionKey: 'supplierAdmin.description' }
  },
  {
    path: '/admin/supplier-finance',
    name: 'AdminSupplierFinance',
    component: () => import('./SupplierFinanceView.vue'),
    meta: { requiresAuth: true, requiresAdmin: true, titleKey: 'supplierAdmin.finance', descriptionKey: 'supplierAdmin.financePending' }
  }
]
