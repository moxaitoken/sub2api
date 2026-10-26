export interface PoolMetric { value: string | null; known: number; total: number; status: 'ok' | 'partial' | 'unavailable' }
export interface QuotaWindow { utilization: number | null; resets_at: string | null }
export interface AccountQuota { source: 'passive'; updated_at: string | null; five_hour: QuotaWindow | null; seven_day: QuotaWindow | null; seven_day_fable: QuotaWindow | null }
export interface PoolOverview {
  observed_at: string; rpm: PoolMetric; tpm: PoolMetric; uncached_tpm: PoolMetric; concurrency: PoolMetric; high_quota: PoolMetric
  windows: Record<'five_hour' | 'seven_day' | 'fable', {maximum: number | null; known: number; total: number; updated_at?: string | null; resets_at?: string | null}>
  account_quotas: Record<string, AccountQuota>; quota_fetched_at: string | null
}
const date = (v: unknown) => typeof v === 'string' && Number.isFinite(Date.parse(v))
const count = (v: unknown): v is number => typeof v === 'number' && Number.isSafeInteger(v) && v >= 0
const percent = (v: unknown) => v === null || (typeof v === 'number' && Number.isFinite(v) && v >= 0)
export function checkedOverview(input: unknown): PoolOverview {
  const v = input as PoolOverview
  if (!v || !date(v.observed_at) || (v.quota_fetched_at !== null && !date(v.quota_fetched_at))) throw new Error('Invalid overview')
  for (const key of ['rpm','tpm','uncached_tpm','concurrency','high_quota'] as const) {
    const m = v[key]
    if (!m || !count(m.known) || !count(m.total) || m.known > m.total || !['ok','partial','unavailable'].includes(m.status) || !(m.value === null || typeof m.value === 'string' && /^\d{1,80}$/.test(m.value))) throw new Error('Invalid metric')
    if ((m.status === 'unavailable' && (m.value !== null || m.known !== 0)) || (m.status !== 'unavailable' && m.value === null) || (m.status === 'ok' && m.known !== m.total)) throw new Error('Invalid completeness')
  }
  for (const key of ['five_hour','seven_day','fable'] as const) { const q = v.windows?.[key]; if (!q || !percent(q.maximum) || !count(q.known) || !count(q.total) || q.known > q.total || (q.updated_at != null && !date(q.updated_at)) || (q.resets_at != null && !date(q.resets_at))) throw new Error('Invalid quota') }
  if (!v.account_quotas || typeof v.account_quotas !== 'object' || Array.isArray(v.account_quotas)) throw new Error('Invalid accounts')
  for (const q of Object.values(v.account_quotas)) {
    if (!q || q.source !== 'passive' || (q.updated_at !== null && !date(q.updated_at))) throw new Error('Invalid snapshot')
    for (const w of [q.five_hour,q.seven_day,q.seven_day_fable]) if (w !== null && (!w || !percent(w.utilization) || (w.resets_at !== null && !date(w.resets_at)))) throw new Error('Invalid window')
  }
  return v
}
