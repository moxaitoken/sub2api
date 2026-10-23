import { mount, flushPromises } from '@vue/test-utils'
import { reactive } from 'vue'
import { it, expect, vi } from 'vitest'
import SupplierResourceView from '../SupplierResourceView.vue'
const { detail, resource, accounts } = vi.hoisted(() => ({detail:vi.fn(),resource:vi.fn(),accounts:vi.fn()}))
vi.mock('@/api/admin/suppliers',()=>({supplierAdminAPI:{detail},supplierResourceClient:resource}))
vi.mock('vue-i18n',async original=>({...await original<typeof import('vue-i18n')>(),useI18n:()=>({t:(s:string)=>s,locale:{value:'zh'}})}))
const params = reactive({id:'30000000-0000-0000-0000-000000000001',tab:'accounts'})
vi.mock('vue-router',async original=>({...await original<typeof import('vue-router')>(),useRoute:()=>({params})}))
it('pins resources to the target supplier and removes old content while another target loads',async()=>{
  detail.mockResolvedValue({id:params.id,name:'Supplier A',login_account:'vendor_a'})
  accounts.mockResolvedValue({items:[],total:0,page:1,pages:1});resource.mockImplementation(()=>({accounts}))
  const w=mount(SupplierResourceView,{global:{stubs:{AppLayout:{template:'<main><slot/></main>'},RouterLink:{props:['to'],template:'<a :href="to"><slot/></a>'},SupplierWorkspaceView:true,OwnProxiesView:true,PortalDialog:true}}});await flushPromises()
  expect(resource).toHaveBeenLastCalledWith(params.id);expect(w.text()).toContain('vendor_a')
  expect(w.findAll('a').map(a=>a.attributes('href'))).toContain(`/admin/suppliers/${params.id}/usage`)
  let resolve!: (data:unknown)=>void
  detail.mockReturnValue(new Promise(r=>{resolve=r}));params.id='30000000-0000-0000-0000-000000000002';await flushPromises()
  expect(w.text()).not.toContain('vendor_a');expect(w.find('supplier-workspace-view-stub').exists()).toBe(false)
  resolve({id:params.id,name:'Supplier B',login_account:'vendor_b'});await flushPromises();expect(w.text()).toContain('vendor_b');w.unmount()
})
