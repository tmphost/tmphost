// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { PRECACHE_PROGRESS, SKIP_WAITING } from '../src/utils/precacheProgress'

const addPlugins = vi.fn()
const precacheAndRoute = vi.fn()
const cleanupOutdatedCaches = vi.fn()
vi.mock('workbox-precaching', () => ({ addPlugins, precacheAndRoute, cleanupOutdatedCaches }))

// In jsdom `self` is the window, so the worker globals go on it.
const manifest = [{ url: 'index.html', revision: '1' }, { url: 'assets/a.js' }, 'assets/a.js']
let page
let messageListeners

beforeEach(async () => {
  vi.resetModules()
  addPlugins.mockClear()
  messageListeners = []
  page = { postMessage: vi.fn() }
  self.__WB_MANIFEST = manifest
  self.skipWaiting = vi.fn()
  self.registration = { active: {} }
  self.clients = { matchAll: vi.fn(async () => [page]) }
  vi.spyOn(self, 'addEventListener').mockImplementation((type, fn) => {
    if (type === 'message') messageListeners.push(fn)
  })
  await import('../src/sw.js')
})

afterEach(() => {
  for (const key of ['__WB_MANIFEST', 'skipWaiting', 'registration', 'clients']) delete self[key]
})

describe('service worker', () => {
  it('precaches the build and drops older caches', () => {
    expect(precacheAndRoute).toHaveBeenCalledWith(manifest)
    expect(cleanupOutdatedCaches).toHaveBeenCalled()
  })

  it('takes over only when the page asks', () => {
    messageListeners[0]({ data: { type: 'OTHER' } })
    messageListeners[0]({})
    expect(self.skipWaiting).not.toHaveBeenCalled()
    messageListeners[0]({ data: { type: SKIP_WAITING } })
    expect(self.skipWaiting).toHaveBeenCalled()
  })

  it('tells open pages how far an update has got', async () => {
    const [plugin] = addPlugins.mock.calls[0][0]
    await plugin.handlerDidComplete({ event: { type: 'install' } })
    expect(self.clients.matchAll).toHaveBeenCalledWith({ type: 'window', includeUncontrolled: true })
    expect(page.postMessage).toHaveBeenCalledWith({ type: PRECACHE_PROGRESS, done: 1, total: 2 })
  })

  it('stays quiet on a first install', async () => {
    self.registration = { active: null }
    const [plugin] = addPlugins.mock.calls[0][0]
    await plugin.handlerDidComplete({ event: { type: 'install' } })
    expect(page.postMessage).not.toHaveBeenCalled()
  })
})
