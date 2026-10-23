import type { Identity } from '../types'

export function identity(): Identity {
  return {
    user: { id: '9223372036854775806', email: 'fixture@example.test', role: 'user', status: 'active' },
    supplier: { id: 'supplier-fixture', name: 'Fixture supplier', portal_status: 'active', version: 1 },
    csrf_token: 'fixture-csrf-not-a-real-secret'
  }
}

export function success(data?: unknown): Response {
  return new Response(JSON.stringify({ code: 0, message: 'ok', ...(data === undefined ? {} : { data }) }), { status: 200 })
}
