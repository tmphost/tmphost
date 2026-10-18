import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

function json(body, status = 200) {
  return { ok: status >= 200 && status < 300, status, json: async () => body }
}

async function loadApi(basePath) {
  vi.resetModules()
  window.__TMPHOST_BASE_PATH__ = basePath
  return import('../src/api.js')
}

describe('api', () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn()
  })
  afterEach(() => {
    delete window.__TMPHOST_BASE_PATH__
  })

  it('builds the API base from the injected base path', async () => {
    expect((await loadApi('/app/')).API_BASE_URL).toBe('/app/api/v1')
    expect((await loadApi(undefined)).API_BASE_URL).toBe('/api/v1')
  })

  it('fetchUser caches the user and dedupes concurrent calls', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValue(json({ user: { id: 'u1' } }))
    const [a, b] = await Promise.all([api.fetchUser(), api.fetchUser()])
    expect(a).toEqual({ id: 'u1' })
    expect(b).toBe(a)
    expect(await api.fetchUser()).toBe(a)
    expect(fetch).toHaveBeenCalledTimes(1)
    expect(fetch).toHaveBeenCalledWith('/api/v1/users/me', { credentials: 'same-origin' })

    api.clearUserCache()
    await api.fetchUser()
    expect(fetch).toHaveBeenCalledTimes(2)
  })

  it('fetchUser returns null without a session', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValue(json({ error: 'unauthorized' }, 401))
    expect(await api.fetchUser()).toBeNull()
  })

  it('fetchUser throws on server errors and can retry', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValueOnce(json({}, 500))
    await expect(api.fetchUser()).rejects.toThrow('Failed to load user (500)')
    fetch.mockResolvedValueOnce(json({ user: { id: 'u2' } }))
    expect(await api.fetchUser()).toEqual({ id: 'u2' })
  })

  it('fetchAuthConfig returns providers', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValue(json({ providers: ['google'] }))
    expect(await api.fetchAuthConfig()).toEqual(['google'])
    fetch.mockResolvedValue(json({}, 503))
    await expect(api.fetchAuthConfig()).rejects.toThrow('Failed to load sign-in options (503)')
  })

  it('providerLoginUrl encodes the redirect', async () => {
    const api = await loadApi('')
    expect(api.providerLoginUrl('github')).toBe('/api/v1/auth/github/login')
    expect(api.providerLoginUrl('google', '/a?b=1')).toBe('/api/v1/auth/google/login?redirect_url=%2Fa%3Fb%3D1')
  })

  it('logout posts and clears the cache even on failure', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValue(json({ user: { id: 'u1' } }))
    await api.fetchUser()

    fetch.mockResolvedValueOnce(json(null, 204))
    await api.logout()
    expect(fetch).toHaveBeenLastCalledWith('/api/v1/auth/logout', { method: 'POST', credentials: 'same-origin' })

    fetch.mockResolvedValueOnce(json({ user: { id: 'u1' } }))
    await api.fetchUser()
    fetch.mockRejectedValueOnce(new Error('offline'))
    await expect(api.logout()).rejects.toThrow('offline')
    fetch.mockResolvedValueOnce(json({}, 401))
    expect(await api.fetchUser()).toBeNull()
  })
})

describe('setup api', () => {
  beforeEach(() => {
    globalThis.fetch = vi.fn()
  })

  it('setupLogin posts the token and surfaces API errors', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValueOnce(json(null, 204))
    await api.setupLogin('tok')
    expect(fetch).toHaveBeenCalledWith('/api/v1/setup/login', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: '{"token":"tok"}',
    })

    fetch.mockResolvedValueOnce(json({ error: 'invalid root access token' }, 401))
    await expect(api.setupLogin('x')).rejects.toMatchObject({ message: 'invalid root access token', status: 401 })

    fetch.mockResolvedValueOnce({ ok: false, status: 502, json: async () => { throw new Error('html') } })
    await expect(api.setupLogin('x')).rejects.toThrow('Sign-in failed (502)')

    fetch.mockResolvedValueOnce(json({}, 500))
    await expect(api.setupLogin('x')).rejects.toThrow('Sign-in failed (500)')
  })

  it('setupLogout posts', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValueOnce(json(null, 204))
    await api.setupLogout()
    expect(fetch).toHaveBeenCalledWith('/api/v1/setup/logout', { method: 'POST', credentials: 'same-origin' })
  })

  it('fetchSettings returns state, null without a session, throws otherwise', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValueOnce(json({ settings: {}, secretsSet: {}, restartRequired: false }))
    expect(await api.fetchSettings()).toEqual({ settings: {}, secretsSet: {}, restartRequired: false })
    fetch.mockResolvedValueOnce(json({ error: 'unauthorized' }, 401))
    expect(await api.fetchSettings()).toBeNull()
    fetch.mockResolvedValueOnce(json({}, 500))
    await expect(api.fetchSettings()).rejects.toThrow('Failed to load settings (500)')
  })

  it('saveSettings puts and surfaces validation errors', async () => {
    const api = await loadApi('')
    fetch.mockResolvedValueOnce(json({ settings: { credits: { monthlyGrant: 9 } } }))
    expect(await api.saveSettings({ credits: { monthlyGrant: 9 } })).toEqual({ settings: { credits: { monthlyGrant: 9 } } })
    expect(fetch).toHaveBeenCalledWith('/api/v1/setup/settings', {
      method: 'PUT',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: '{"settings":{"credits":{"monthlyGrant":9}}}',
    })
    fetch.mockResolvedValueOnce(json({ error: 'invalid settings: sqlite DSN is required' }, 400))
    await expect(api.saveSettings({})).rejects.toMatchObject({ message: 'invalid settings: sqlite DSN is required', status: 400 })
  })
})
