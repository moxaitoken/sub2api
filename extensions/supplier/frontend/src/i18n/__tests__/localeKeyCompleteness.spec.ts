import { describe, expect, it } from 'vitest'
import zh from '../locales/zh'
import en from '../locales/en'

function entries(value: Record<string, unknown>, prefix = ''): Record<string, string> {
  return Object.fromEntries(Object.entries(value).flatMap(([key, item]) => {
    const path = `${prefix}${key}`
    return typeof item === 'string' ? [[path, item]] : Object.entries(entries(item as Record<string, unknown>, `${path}.`))
  }))
}

describe('locale completeness', () => {
  it('has matching Chinese/English keys and interpolation placeholders', () => {
    const chinese = entries(zh)
    const english = entries(en)
    expect(Object.keys(chinese).sort()).toEqual(Object.keys(english).sort())
    for (const key of Object.keys(chinese)) {
      expect(chinese[key].match(/\{[^}]+\}/g) || [], key).toEqual(english[key].match(/\{[^}]+\}/g) || [])
    }
  })
})
