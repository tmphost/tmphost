import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { usePlatformStore } from '../src/stores/platformStore'
import { useToasts } from '../src/composables/useToasts'
import { creditsPercent, renewsIn } from '../src/utils/credits'
import { loginErrorMessage } from '../src/utils/loginErrors'

describe('platformStore', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('accepts known platforms only', () => {
    const store = usePlatformStore()
    expect(store.isWeb).toBe(true)
    store.setPlatform('desktop')
    expect(store.isDesktop).toBe(true)
    expect(store.isWeb).toBe(false)
    store.setPlatform('toaster')
    expect(store.platform).toBe('web')
  })
})

describe('useToasts', () => {
  beforeEach(() => vi.useFakeTimers())
  afterEach(() => {
    useToasts().toasts.value = []
    vi.useRealTimers()
  })

  it('adds, expires and caps toasts', () => {
    const { toasts, notifyInfo, notifySuccess, notifyError, removeToast } = useToasts()
    const id = notifyInfo('hello', 'Title')
    expect(toasts.value[0]).toMatchObject({ id, message: 'hello', type: 'info', title: 'Title' })
    notifySuccess('ok')
    notifyError('bad')
    expect(toasts.value.map((t) => t.type)).toEqual(['info', 'success', 'error'])

    vi.advanceTimersByTime(4000)
    expect(toasts.value.map((t) => t.type)).toEqual(['error'])
    vi.advanceTimersByTime(4000)
    expect(toasts.value).toEqual([])

    for (let i = 0; i < 6; i++) notifyInfo(`m${i}`)
    expect(toasts.value.map((t) => t.message)).toEqual(['m2', 'm3', 'm4', 'm5'])
    removeToast(toasts.value[0].id)
    expect(toasts.value).toHaveLength(3)
  })

  it('addToast defaults to info', () => {
    const { toasts, addToast } = useToasts()
    addToast('plain')
    expect(toasts.value[0]).toMatchObject({ type: 'info', title: null })
  })
})

describe('credits', () => {
  it('creditsPercent clamps and handles missing data', () => {
    expect(creditsPercent(null)).toBe(0)
    expect(creditsPercent({ credits: 5, creditsLimit: 0 })).toBe(0)
    expect(creditsPercent({ credits: 125, creditsLimit: 250 })).toBe(50)
    expect(creditsPercent({ credits: 300, creditsLimit: 250 })).toBe(100)
    expect(creditsPercent({ credits: -1, creditsLimit: 250 })).toBe(0)
  })

  it('renewsIn describes the next reset', () => {
    const now = Date.parse('2026-10-18T12:00:00Z')
    expect(renewsIn('garbage', now)).toBe('')
    expect(renewsIn(undefined, now)).toBe('')
    expect(renewsIn('2026-10-18T00:00:00Z', now)).toBe('Renews today')
    expect(renewsIn('2026-10-19T00:00:00Z', now)).toBe('Renews tomorrow')
    expect(renewsIn('2026-11-17T00:00:00Z', now)).toBe('Renews in 30 days')
    expect(renewsIn(new Date(Date.now() + 36 * 3600 * 1000).toISOString())).toBe('Renews in 2 days')
  })
})

describe('loginErrorMessage', () => {
  it('maps known codes and falls back', () => {
    expect(loginErrorMessage(undefined)).toBe('')
    expect(loginErrorMessage('cancelled')).toBe('Sign-in was cancelled.')
    expect(loginErrorMessage('email_not_verified')).toMatch(/verified email/)
    expect(loginErrorMessage('weird')).toMatch(/Something went wrong/)
  })
})

describe('providers', () => {
  it('slugs names like the server', async () => {
    const { providerSlug } = await import('../src/utils/providers')
    expect(providerSlug(' My  Okta! ')).toBe('my-okta')
    expect(providerSlug('--A_b--')).toBe('a-b')
    expect(providerSlug('')).toBe('')
    expect(providerSlug(undefined)).toBe('')
  })

  it('renders SVG as a data URI', async () => {
    const { svgDataUri } = await import('../src/utils/providers')
    expect(svgDataUri('<svg a="#"/>')).toBe('data:image/svg+xml;charset=utf-8,%3Csvg%20a%3D%22%23%22%2F%3E')
  })
})
