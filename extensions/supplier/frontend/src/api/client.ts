import { checkedUsage } from './usage'
import type { UsagePeriod, UsageReport } from './types'
import type { Identity, LoginInput, LoginResult, SupplierAccount, Page, Authorization, AccountOperation, PortalIdentity, ManagedSupplier, OwnProxy, ProxyInput, OnboardingOptions } from './types'

const base = '/api/supplier/v1'
const safeReasons = new Set([
  'PROXY_CHECK_FAILED','PROXY_CLEANUP_PENDING','INVALID_PROXY','PROXY_DNS_REJECTED','PROXY_ADDRESS_REJECTED','PROXY_NOT_AVAILABLE','PROXY_STATE_DRIFT',
  'UNAUTHORIZED', 'INVALID_CREDENTIALS', 'SUPPLIER_DISABLED',
  'UPSTREAM_UNAVAILABLE', 'UPSTREAM_ACTION_REQUIRED', 'FORBIDDEN', 'CSRF_INVALID',
  'INVALID_ORIGIN', 'RATE_LIMITED', 'VALIDATION_ERROR', 'INVALID_REQUEST',
  'CAPTCHA_REQUIRED', 'CAPTCHA_INVALID', 'CHALLENGE_EXPIRED', 'INVALID_2FA_CODE',
  'AUTHENTICATION_REQUIRED', 'ACCESS_DENIED', 'SERVICE_UNAVAILABLE', 'ORIGIN_REJECTED',
  'LOGIN_RATE_LIMIT', 'UPSTREAM_UNAUTHORIZED', 'UPSTREAM_FORBIDDEN', 'UPSTREAM_LOCKED',
  'UPSTREAM_RATE_LIMITED', 'UPSTREAM_UNAVAILABLE', 'UPSTREAM_TRANSPORT_ERROR',
  'UPSTREAM_BAD_RESPONSE', 'UPSTREAM_REJECTED', 'UPSTREAM_INVALID_TOKEN',
  'ACCOUNT_SERVICE_NOT_CONFIGURED', 'INVALID_ACCOUNT_NAME', 'INVALID_SUPPLIER_GROUP_NAME', 'OAUTH_STATE_MISMATCH', 'OAUTH_EXPIRED',
  'INVALID_AUTHORIZATION_CODE', 'INVALID_AUTHORIZATION_RESPONSE', 'ACCOUNT_IDENTITY_UNVERIFIED',
  'ACCOUNT_STATE_DRIFT', 'ACCOUNT_STATE_CONFLICT', 'IDEMPOTENCY_KEY_REQUIRED', 'IF_MATCH_REQUIRED',
  'INVALID_SUPPLIER_NAME', 'INVALID_SUPPLIER_LOGIN', 'INVALID_SUPPLIER_PASSWORD',
  'INVALID_SUPPLIER_INPUT', 'INVALID_VERSION', 'INVALID_PAGE', 'RESOURCE_VERSION_CONFLICT', 'INVALID_REVIEW_REASON'
])

export class ApiError extends Error {
  constructor(readonly status: number, readonly reason: string, readonly requestId?: string) {
    // Never copy raw upstream messages into UI state or logs.
    super('Supplier request failed')
    this.name = 'ApiError'
  }
}

export interface RequestOptions {
  method?: 'GET' | 'POST' | 'PUT' | 'DELETE'
  body?: unknown
  authentication?: boolean
  allowEmptyData?: boolean
  idempotencyKey?: string
  version?: number
}

export class SupplierClient {
  private csrf = ''

  constructor(
    private readonly onUnauthorized: () => void,
    private readonly transport: typeof fetch = (...args) => fetch(...args),
    private readonly portal: 'supplier' | 'admin' = 'supplier',
    private readonly scopedRequest?: <T>(path: string, options: RequestOptions) => Promise<T>
  ) {}

  clearSession(): void { this.csrf = '' }

  private async request<T>(path: string, options: RequestOptions = {}): Promise<T> {
    if (this.scopedRequest) return this.scopedRequest<T>(path, options)
    const method = options.method || 'GET'
    const headers = new Headers({ Accept: 'application/json' })
    if (options.idempotencyKey) headers.set('Idempotency-Key', options.idempotencyKey)
    if (options.version !== undefined) headers.set('If-Match', String(options.version))
    if (options.body !== undefined) headers.set('Content-Type', 'application/json')
    if (method !== 'GET' && options.authentication !== false) {
      if (!this.csrf) throw new ApiError(403, 'CSRF_INVALID')
      headers.set('X-CSRF-Token', this.csrf)
    }
    let response: Response
    try {
      response = await this.transport(`${this.portal === 'admin' ? '/api/supplier/admin/v1' : base}${path}`, {
        method, headers, credentials: 'include', cache: 'no-store', redirect: 'error',
        body: options.body === undefined ? undefined : JSON.stringify(options.body),
        signal: AbortSignal.timeout(20_000)
      })
    } catch {
      throw new ApiError(0, 'NETWORK_ERROR')
    }
    if ([401, 403].includes(response.status)) this.onUnauthorized()
    let decoded: unknown
    try { decoded = await response.json() } catch { throw new ApiError(response.status, 'INVALID_RESPONSE') }
    if (!decoded || typeof decoded !== 'object' || Array.isArray(decoded)) throw new ApiError(response.status, 'INVALID_RESPONSE')
    const envelope = decoded as { code?: number; data?: T; reason?: unknown; request_id?: unknown }
    if (![401, 403].includes(response.status) && [401, 403].includes(envelope.code || 0)) this.onUnauthorized()
    if (!response.ok || envelope.code !== 0) {
      const reason = typeof envelope.reason === 'string' && safeReasons.has(envelope.reason)
        ? envelope.reason : 'REQUEST_FAILED'
      const requestId = typeof envelope.request_id === 'string' && /^[a-f0-9-]{36}$/i.test(envelope.request_id)
        ? envelope.request_id : undefined
      throw new ApiError(response.status, reason, requestId)
    }
    if (envelope.data === undefined && !options.allowEmptyData) throw new ApiError(response.status, 'INVALID_RESPONSE')
    return envelope.data as T
  }

  private validateLogin(result: LoginResult): LoginResult {
    if (!result || typeof result.requires_2fa !== 'boolean' || (result.requires_2fa &&
      (typeof result.challenge_id !== 'string' || !result.challenge_id ||
        typeof result.expires_at !== 'string' || !Number.isFinite(Date.parse(result.expires_at))))) {
      throw new ApiError(200, 'INVALID_RESPONSE')
    }
    return result
  }

  async login(input: LoginInput): Promise<LoginResult> {
    return this.validateLogin(await this.request<LoginResult>('/auth/login', { method: 'POST', body: input, authentication: false }))
  }

  async completeTwoFactor(challengeId: string, totpCode: string): Promise<LoginResult> {
    return this.validateLogin(await this.request<LoginResult>('/auth/login/2fa', {
      method: 'POST', body: { challenge_id: challengeId, totp_code: totpCode }, authentication: false
    }))
  }

  async me(): Promise<Identity> {
    const identity = await this.request<Identity>('/me')
    const nonempty = (value: unknown): value is string => typeof value === 'string' && value.trim().length > 0
    if (!identity || !identity.user || !nonempty(identity.user.id) ||
      !nonempty(identity.user.email) || !nonempty(identity.user.role) || !nonempty(identity.user.status) ||
      !nonempty(identity.csrf_token) || !identity.supplier ||
      !nonempty(identity.supplier.id) || !nonempty(identity.supplier.name) ||
      !['active', 'disabled'].includes(identity.supplier.portal_status) ||
      !Number.isSafeInteger(identity.supplier.version) || identity.supplier.version < 1) {
      this.clearSession()
      this.onUnauthorized()
      throw new ApiError(200, 'INVALID_RESPONSE')
    }
    this.csrf = identity.csrf_token
    return identity
  }

  async logout(): Promise<void> {
    await this.request('/auth/logout', { method: 'POST', allowEmptyData: true })
    this.clearSession()
  }

  async usage(period: UsagePeriod, accountId = '', page = 1): Promise<UsageReport> {
    const query = new URLSearchParams({ period, page: String(page), page_size: '20' })
    if (accountId) query.set('account_id', accountId)
    const value = await this.request<unknown>('/usage?' + query)
    try { return checkedUsage(value) } catch { throw new ApiError(200, 'INVALID_RESPONSE') }
  }

  async accounts(page = 1, review = false): Promise<Page<SupplierAccount>> {
    const data = await this.request<Page<SupplierAccount>>(`${review ? '/review' : ''}/accounts?page=${page}&page_size=20`)
    if (!data || !Array.isArray(data.items) || !Number.isSafeInteger(data.total) || data.total < 0 ||
      data.items.some(item => !item || typeof item.id !== 'string' || typeof item.name !== 'string' ||
        !Number.isSafeInteger(item.version) || item.version < 1 ||
        !['provisioning', 'identity_pending', 'pending', 'approved', 'rejected', 'blocked', 'deleting', 'deleted', 'needs_attention'].includes(item.admission_state) ||
        !['paused', 'running'].includes(item.desired_state))) throw new ApiError(200, 'INVALID_RESPONSE')
    return data
  }

  async beginAuthorization(name: string): Promise<Authorization> {
    const data = await this.request<Authorization>('/oauth/sessions', { method: 'POST', body: { name } })
    try {
      const url = new URL(data.authorization_url)
      if (url.protocol !== 'https:' || url.host !== 'claude.com' || url.pathname !== '/cai/oauth/authorize' ||
        url.username || url.password || !url.searchParams.get('state') || typeof data.id !== 'string' ||
        !Number.isFinite(Date.parse(data.expires_at))) throw new Error('invalid authorization response')
    } catch { throw new ApiError(200, 'INVALID_RESPONSE') }
    return data
  }

  private checkedOperation(data: AccountOperation): AccountOperation {
    if (!data || typeof data.id !== 'string' || !data.id || typeof data.step !== 'string' ||
      !['pending', 'running', 'succeeded', 'failed', 'needs_attention'].includes(data.state)) throw new ApiError(200, 'INVALID_RESPONSE')
    return data
  }

  async completeAuthorization(id: string, code: string, key: string): Promise<AccountOperation> {
    return this.checkedOperation(await this.request<AccountOperation>(`/oauth/sessions/${encodeURIComponent(id)}/complete`, {
      method: 'POST', body: { authorization_code: code }, idempotencyKey: key
    }))
  }

  async changeAccount(account: SupplierAccount, approve: boolean, key: string, reason: string): Promise<AccountOperation> {
    return this.checkedOperation(await this.request<AccountOperation>(`${approve ? '/review' : ''}/accounts/${encodeURIComponent(account.id)}/${approve ? 'approve' : 'pause'}`, {
      method: 'POST', body: { reason }, idempotencyKey: key, version: account.version
    }))
  }

  async accountOperation(id: string): Promise<AccountOperation> {
    return this.checkedOperation(await this.request<AccountOperation>(`/operations/${encodeURIComponent(id)}`))
  }
  async portalMe(): Promise<PortalIdentity> {
    const data = await this.request<PortalIdentity>('/me')
    const validText = (x: unknown) => typeof x === 'string' && x.trim().length > 0
    if (!data || !data.principal || data.principal.kind !== this.portal || !validText(data.principal.id) || !validText(data.csrf_token) ||
      (this.portal === 'supplier' && (!data.supplier || !validText(data.supplier.id) || !validText(data.supplier.name) || !validText(data.principal.login_account) || data.supplier.portal_status !== 'active')) ||
      (this.portal === 'admin' && (!validText(data.principal.email) || data.supplier !== undefined))) {
      this.clearSession(); this.onUnauthorized(); throw new ApiError(200, 'INVALID_RESPONSE')
    }
    this.csrf = data.csrf_token
    return data
  }
  async localLogin(login: string, password: string): Promise<LoginResult> {
    return this.validateLogin(await this.request<LoginResult>('/auth/login', {method: 'POST', body: {login_account: login, password}, authentication: false}))
  }
  async suppliers(search = '', page = 1): Promise<Page<ManagedSupplier>> {
    if (this.portal !== 'admin') throw new ApiError(403, 'ACCESS_DENIED')
    return this.request(`/suppliers?search=${encodeURIComponent(search)}&page=${page}&page_size=20`)
  }
  async createSupplier(name: string, login: string, password: string, key: string): Promise<ManagedSupplier> {
    if (this.portal !== 'admin') throw new ApiError(403, 'ACCESS_DENIED')
    return this.request('/suppliers', {method: 'POST', body: {name, login_account: login, initial_password: password}, idempotencyKey: key})
  }
  async setSupplierStatus(supplier: ManagedSupplier, enabled: boolean, key: string): Promise<ManagedSupplier> {
    if (this.portal !== 'admin') throw new ApiError(403, 'ACCESS_DENIED')
    return this.request(`/suppliers/${encodeURIComponent(supplier.id)}/status`, {method: 'PUT', body: {enabled}, version: supplier.version, idempotencyKey: key})
  }
  async resetSupplier(supplier: ManagedSupplier, password: string, key: string): Promise<ManagedSupplier> {
    if (this.portal !== 'admin') throw new ApiError(403, 'ACCESS_DENIED')
    return this.request(`/suppliers/${encodeURIComponent(supplier.id)}/credential-reset`, {method: 'POST', body: {new_password: password}, version: supplier.version, idempotencyKey: key})
  }
  async scopedAccounts(supplier?: string, page = 1): Promise<Page<SupplierAccount>> {
    if ((this.portal === 'admin') !== Boolean(supplier)) throw new ApiError(403, 'ACCESS_DENIED')
    const prefix = supplier ? `/suppliers/${encodeURIComponent(supplier)}` : ''
    return this.request(`${prefix}/accounts?page=${page}&page_size=20`)
  }

  private supplierOnly(): void { if (this.portal !== 'supplier') throw new ApiError(403, 'ACCESS_DENIED') }
  private checkedProxy(p: OwnProxy): OwnProxy {
    if (!p || typeof p.id !== 'string' || !p.id || typeof p.name !== 'string' || typeof p.address !== 'string' || typeof p.region !== 'string' ||
      !['provisioning','active','disabled','needs_attention','deleting'].includes(p.status) || !Number.isSafeInteger(p.capacity) || p.capacity < 1 ||
      !Number.isSafeInteger(p.accounts) || p.accounts < 0 || !Number.isSafeInteger(p.held) || p.held < 0 || !Number.isSafeInteger(p.version) || p.version < 1) throw new ApiError(200,'INVALID_RESPONSE')
    try { const url = new URL(p.address); if (!['http:','https:','socks5:','socks5h:'].includes(url.protocol) || !url.hostname || (!url.port && !['http:','https:'].includes(url.protocol)) || url.username || url.password || url.search || url.hash) throw new Error('unsafe') }
    catch { throw new ApiError(200,'INVALID_RESPONSE') }
    return {id:p.id,name:p.name,address:p.address,region:p.region,status:p.status,capacity:p.capacity,accounts:p.accounts,held:p.held,version:p.version}
  }
  async onboardingOptions(): Promise<OnboardingOptions> {
    this.supplierOnly()
    const data = await this.request<OnboardingOptions>('/onboarding/options')
    if (!data || !Array.isArray(data.groups) || !Array.isArray(data.proxies) || data.groups.some(g => !g || !Number.isSafeInteger(g.id) || g.id <= 0 || typeof g.name !== 'string' || g.platform !== 'anthropic' || g.status !== 'active' || (g.group_ids !== undefined && (!Array.isArray(g.group_ids) || g.group_ids.some(id => !Number.isSafeInteger(id) || id <= 0))))) throw new ApiError(200,'INVALID_RESPONSE')
    return { groups: data.groups.map(g => ({id:g.id,name:g.name,platform:g.platform,status:g.status,group_ids:g.group_ids?.length ? [...new Set(g.group_ids)] : [g.id]})), proxies: data.proxies.map(p => this.checkedProxy(p)) }
  }
  async ownProxies(): Promise<{items: OwnProxy[]}> {
    this.supplierOnly(); const data = await this.request<{items: OwnProxy[]}>('/proxies'); if (!data || !Array.isArray(data.items)) throw new ApiError(200,'INVALID_RESPONSE'); return {items:data.items.map(p=>this.checkedProxy(p))}
  }
  async addProxy(input: ProxyInput, key: string): Promise<OwnProxy> {
    this.supplierOnly(); return this.checkedProxy(await this.request<OwnProxy>('/proxies', {method:'POST',body:input,idempotencyKey:key}))
  }
  async testOwnProxy(id: string): Promise<OwnProxy> {
    this.supplierOnly(); return this.checkedProxy(await this.request<OwnProxy>(`/proxies/${encodeURIComponent(id)}/test`, {method:'POST',body:{}}))
  }
  async deleteOwnProxy(id: string): Promise<void> {
    this.supplierOnly(); await this.request(`/proxies/${encodeURIComponent(id)}`, {method:'DELETE'})
  }
  async beginOnboarding(name: string, proxy: string, groups: number[], key: string): Promise<Authorization> {
    this.supplierOnly()
    const group_ids = [...new Set(groups)].filter(id => Number.isSafeInteger(id) && id > 0)
    const data = await this.request<Authorization>('/oauth/sessions', {method:'POST',body:{name,proxy_id:proxy,group_ids},idempotencyKey:key})
    try {
      const url = new URL(data.authorization_url)
      if (url.protocol !== 'https:' || url.host !== 'claude.com' || url.pathname !== '/cai/oauth/authorize' || url.username || url.password || url.hash || !url.searchParams.get('state') || !data.id || !Number.isFinite(Date.parse(data.expires_at))) throw new Error('invalid')
    } catch { throw new ApiError(200,'INVALID_RESPONSE') }
    return data
  }
  async cancelOnboarding(id: string): Promise<void> {
    this.supplierOnly(); await this.request(`/oauth/sessions/${encodeURIComponent(id)}`, {method:'DELETE'})
  }

}
