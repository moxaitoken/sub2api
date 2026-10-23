import type { UsageAccount, UsageMetrics, UsageReport } from './types'

const object = (v: unknown): Record<string, unknown> => {
  if (!v || typeof v !== 'object' || Array.isArray(v)) throw new Error('invalid usage')
  return v as Record<string, unknown>
}
const text = (v: unknown): string => { if (typeof v !== 'string') throw new Error('invalid usage'); return v }
const integer = (v: unknown): number => { if (!Number.isSafeInteger(v) || Number(v) < 0) throw new Error('invalid usage'); return Number(v) }
const timestamp = (v: unknown): string => { const s = text(v); if (!Number.isFinite(Date.parse(s))) throw new Error('invalid usage'); return s }
function metrics(value: unknown): UsageMetrics {
  const v = object(value), out = {} as UsageMetrics
  for (const key of ['record_count', 'input_tokens', 'output_tokens', 'cache_read_tokens', 'cache_creation_tokens', 'tokens', 'cache_tokens', 'total_cost_usd'] as const) {
    const s = text(v[key]); if (!(key === 'total_cost_usd' ? /^\d+(\.\d+)?$/ : /^\d+$/).test(s) || s.length > 100) throw new Error('invalid usage')
    out[key] = s
  }
  return out
}
function account(value: unknown): UsageAccount {
  const v = object(value)
  if (typeof v.archived !== 'boolean') throw new Error('invalid usage')
  const id = text(v.id); if (!/^[0-9a-f-]{36}$/i.test(id)) throw new Error('invalid usage')
  return { id, name: text(v.name), archived: v.archived }
}
function state(value: unknown): 'complete' | 'partial' { if (value !== 'complete' && value !== 'partial') throw new Error('invalid usage'); return value }
function list(v: unknown): unknown[] { if (!Array.isArray(v) || v.length > 1000) throw new Error('invalid usage'); return v }
// Construct an allowlist: upstream/customer identifiers are never copied into UI state.
export function checkedUsage(value: unknown): UsageReport {
  const v = object(value)
  if (!['today', '7d', '30d'].includes(text(v.period)) || v.timezone !== 'Asia/Shanghai' || v.request_attempts !== null) throw new Error('invalid usage')
  return {
    period: v.period as UsageReport['period'], timezone: 'Asia/Shanghai', account_id: text(v.account_id),
    start: timestamp(v.start), end: timestamp(v.end), observed_at: timestamp(v.observed_at),
    summary: metrics(v.summary), status: state(v.status), request_attempts: null,
    total: integer(v.total), page: integer(v.page), page_size: integer(v.page_size), pages: integer(v.pages),
    account_options: list(v.account_options).map(account),
    daily: list(v.daily).map(value => { const d = object(value), date = text(d.date); if (!/^\d{4}-\d{2}-\d{2}$/.test(date)) throw new Error('invalid usage'); return { date, ...metrics(d) } }),
    accounts: list(v.accounts).map(value => { const a = object(value); return { ...account(a), ...metrics(a), sync_state: state(a.sync_state), synced_at: a.synced_at === null ? null : timestamp(a.synced_at) } })
  }
}
