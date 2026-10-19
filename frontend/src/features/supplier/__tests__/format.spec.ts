import { describe, expect, it } from 'vitest'
import { defaultRange, extractAuthCode, formatAmount, previousMonthRange, toDateParam, totalTokens } from '../format'

describe('supplier format helpers', () => {
  it('sums all token kinds including cache traffic', () => {
    expect(
      totalTokens({
        request_count: 1,
        input_tokens: 10,
        output_tokens: 20,
        cache_creation_tokens: 30,
        cache_read_tokens: 40,
        amount: 0,
      }),
    ).toBe(100)
  })

  it('formats amounts with four decimals', () => {
    expect(formatAmount(1.23456)).toBe('$1.2346')
    expect(formatAmount(undefined)).toBe('$0.0000')
  })

  it('builds inclusive local date ranges', () => {
    const now = new Date(2026, 9, 19, 15, 30)
    expect(toDateParam(now)).toBe('2026-10-19')
    expect(defaultRange(7, now)).toEqual({ start: '2026-10-13', end: '2026-10-19' })
    expect(previousMonthRange(now)).toEqual({ start: '2026-09-01', end: '2026-09-30' })
    expect(previousMonthRange(new Date(2026, 0, 5))).toEqual({ start: '2025-12-01', end: '2025-12-31' })
  })

  it('extracts the authorization code from a pasted callback URL', () => {
    expect(extractAuthCode('  abc123  ')).toBe('abc123')
    expect(extractAuthCode('https://console.anthropic.com/oauth/code/callback?code=xyz%2B1&state=s')).toBe('xyz+1')
    expect(extractAuthCode('callback#code=def&state=s')).toBe('def')
  })
})
