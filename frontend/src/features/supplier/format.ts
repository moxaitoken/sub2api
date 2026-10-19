import type { ModelFamily, ReviewStatus, SettlementStatus, UsageTotals } from './types'

/** Total tokens of a usage bucket, including cache traffic. */
export function totalTokens(t: UsageTotals): number {
  return t.input_tokens + t.output_tokens + t.cache_creation_tokens + t.cache_read_tokens
}

/** Settlement amounts are account cost in USD; show 4 decimals so small lines stay visible. */
export function formatAmount(amount: number | null | undefined): string {
  return `$${(amount ?? 0).toFixed(4)}`
}

export const FAMILY_ORDER: ModelFamily[] = ['opus', 'fable', 'sonnet', 'haiku', 'other']

export function reviewBadgeClass(status: ReviewStatus): string {
  return {
    pending: 'badge badge-warning',
    approved: 'badge badge-success',
    rejected: 'badge badge-danger',
  }[status]
}

export function settlementBadgeClass(status: SettlementStatus): string {
  return {
    draft: 'badge badge-gray',
    confirmed: 'badge badge-primary',
    paid: 'badge badge-success',
    void: 'badge badge-danger',
  }[status]
}

/** YYYY-MM-DD for a local date, used for the backend's inclusive date bounds. */
export function toDateParam(date: Date): string {
  const y = date.getFullYear()
  const m = String(date.getMonth() + 1).padStart(2, '0')
  const d = String(date.getDate()).padStart(2, '0')
  return `${y}-${m}-${d}`
}

/** Default dashboard range: the last `days` days including today. */
export function defaultRange(days = 7, now = new Date()): { start: string; end: string } {
  const start = new Date(now)
  start.setDate(start.getDate() - (days - 1))
  return { start: toDateParam(start), end: toDateParam(now) }
}

/** First and last day of the previous calendar month, the usual settlement period. */
export function previousMonthRange(now = new Date()): { start: string; end: string } {
  const first = new Date(now.getFullYear(), now.getMonth() - 1, 1)
  const last = new Date(now.getFullYear(), now.getMonth(), 0)
  return { start: toDateParam(first), end: toDateParam(last) }
}

/** Pulls the authorization code out of a pasted callback URL, or returns the input as-is. */
export function extractAuthCode(input: string): string {
  const value = input.trim()
  if (!value.includes('code=')) return value
  try {
    const url = new URL(value)
    return url.searchParams.get('code') ?? value
  } catch {
    const match = value.match(/[?&#]code=([^&#]+)/)
    return match ? decodeURIComponent(match[1]) : value
  }
}
