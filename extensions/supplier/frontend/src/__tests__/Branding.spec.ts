import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { mount } from '@vue/test-utils'
import { describe, expect, it } from 'vitest'
import PortalIcon from '../components/PortalIcon.vue'

describe('Goccmax portal branding', () => {
  it('uses a G-shaped mark in the shared brand icon', () => {
    const icon = mount(PortalIcon, { props: { name: 'brand', size: 30 } })
    expect(icon.get('path').attributes('d')).toBe('M19 6.5A8 8 0 1 0 20 15v-3h-8')
    expect(icon.attributes('width')).toBe('30')
    expect(icon.attributes('aria-hidden')).toBe('true')
    icon.unmount()
  })

  it('uses Goccmax as the browser tab title', () => {
    const html = readFileSync(resolve(process.cwd(), 'index.html'), 'utf8')
    const document = new DOMParser().parseFromString(html, 'text/html')
    expect(document.title).toBe('Goccmax')
  })
})
