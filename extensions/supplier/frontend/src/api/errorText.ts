import { ApiError } from './client'

export function errorKey(error: unknown): string {
  if (!(error instanceof ApiError)) return 'errors.generic'
  const reasons: Record<string, string> = {
    PROXY_CHECK_FAILED:'proxyCheckFailed', PROXY_CLEANUP_PENDING:'proxyCleanupPending',
    INVALID_PROXY:'proxyInput', PROXY_DNS_REJECTED:'proxyDns', PROXY_ADDRESS_REJECTED:'proxyAddress', PROXY_NOT_AVAILABLE:'proxyUnavailable', PROXY_STATE_DRIFT:'proxyUnavailable', INVALID_SUPPLIER_INPUT: 'invalid', NETWORK_ERROR: 'network', UNAUTHORIZED: 'unauthorized', INVALID_CREDENTIALS: 'unauthorized',
    SUPPLIER_DISABLED: 'supplierDisabled', FORBIDDEN: 'forbidden',
    ACCOUNT_SERVICE_NOT_CONFIGURED: 'accountConfiguration', ACCOUNT_STATE_DRIFT: 'accountDrift',
    OAUTH_STATE_MISMATCH: 'authorization', OAUTH_EXPIRED: 'authorization', INVALID_AUTHORIZATION_CODE: 'authorization',
    INVALID_AUTHORIZATION_RESPONSE: 'invalid', ACCOUNT_IDENTITY_UNVERIFIED: 'accountIdentity',
    RESOURCE_VERSION_CONFLICT: 'accountConflict', ACCOUNT_STATE_CONFLICT: 'accountConflict',
    INVALID_ACCOUNT_NAME: 'invalid', INVALID_REVIEW_REASON: 'invalid', IDEMPOTENCY_KEY_REQUIRED: 'invalid', IF_MATCH_REQUIRED: 'invalid', INVALID_VERSION: 'invalid', INVALID_PAGE: 'invalid',
    UPSTREAM_UNAVAILABLE: 'upstream', UPSTREAM_ACTION_REQUIRED: 'actionRequired',
    CSRF_INVALID: 'csrf', INVALID_ORIGIN: 'csrf', RATE_LIMITED: 'rateLimited',
    CAPTCHA_REQUIRED: 'captcha', CAPTCHA_INVALID: 'captcha', CHALLENGE_EXPIRED: 'twoFactor',
    INVALID_2FA_CODE: 'twoFactor', INVALID_RESPONSE: 'invalid', VALIDATION_ERROR: 'invalid', INVALID_REQUEST: 'invalid',
    AUTHENTICATION_REQUIRED: 'unauthorized', ACCESS_DENIED: 'forbidden', SERVICE_UNAVAILABLE: 'upstream',
    ORIGIN_REJECTED: 'csrf', LOGIN_RATE_LIMIT: 'rateLimited',
    UPSTREAM_UNAUTHORIZED: 'unauthorized', UPSTREAM_FORBIDDEN: 'forbidden', UPSTREAM_LOCKED: 'actionRequired',
    UPSTREAM_RATE_LIMITED: 'rateLimited', UPSTREAM_TRANSPORT_ERROR: 'upstream', UPSTREAM_BAD_RESPONSE: 'upstream',
    UPSTREAM_REJECTED: 'invalid', UPSTREAM_INVALID_TOKEN: 'unauthorized'
  }
  return `errors.${reasons[error.reason] || (error.status === 401 ? 'unauthorized' : error.status === 403 ? 'forbidden' : 'generic')}`
}
