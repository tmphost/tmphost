// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import {
  RESTART_TIMEOUT,
  UPDATE_CHECK_INTERVAL,
  onProgressMessage,
  registerServiceWorker,
  resetPwaUpdate,
  usePwaUpdate,
} from '../src/composables/usePwaUpdate'
import {
  PRECACHE_PROGRESS,
  percentOf,
  precacheProgressPlugin,
  precacheTotal,
} from '../src/utils/precacheProgress'
import { Workbox } from 'workbox-window'
import UpdateBanner from '../src/components/UpdateBanner.vue'

// A stand-in for workbox-window's Workbox that lets a test fire its events.
function fakeWorkbox({ register = () => Promise.resolve({}), update = () => Promise.resolve() } = {}) {
  const listeners = {}
  return {
    url: null,
    options: null,
    addEventListener: (type, fn) => {
      listeners[type] = fn
    },
    emit: (type, event = {}) => listeners[type](event),
    register: vi.fn(register),
    update: vi.fn(update),
    messageSkipWaiting: vi.fn(),
  }
}

// A stand-in for navigator.serviceWorker.
function fakeContainer(controller = {}) {
  const listeners = []
  return {
    controller,
    addEventListener: (type, fn) => listeners.push(fn),
    post: (data, source) => listeners.forEach((fn) => fn({ data, source })),
  }
}

// A worker whose state a test can move along.
function fakeWorker(state = 'installing') {
  const listeners = []
  return {
    state,
    addEventListener: (type, fn) => listeners.push(fn),
    moveTo(next) {
      this.state = next
      listeners.forEach((fn) => fn())
    },
  }
}

function setup(options = {}) {
  const wb = fakeWorkbox(options)
  const container = fakeContainer()
  const reloadPage = vi.fn()
  registerServiceWorker({
    basePath: '/base',
    container,
    createWorkbox: (url, opts) => Object.assign(wb, { url, options: opts }),
    enabled: true,
    reloadPage,
  })
  return { wb, container, reloadPage }
}

describe('precacheProgress', () => {
  it('reports install fetches only', async () => {
    const report = vi.fn()
    const plugin = precacheProgressPlugin({ total: 2, report })
    await plugin.handlerDidComplete({ event: { type: 'fetch' } })
    await plugin.handlerDidComplete({})
    await plugin.handlerDidComplete({ event: { type: 'install' } })
    await plugin.handlerDidComplete({ event: { type: 'install' } })
    expect(report.mock.calls).toEqual([
      [{ type: PRECACHE_PROGRESS, done: 1, total: 2 }],
      [{ type: PRECACHE_PROGRESS, done: 2, total: 2 }],
    ])
  })

  it('counts each URL once', () => {
    expect(precacheTotal(['a.js', { url: 'a.js', revision: '1' }, { url: 'b.js' }])).toBe(2)
  })

  it('turns a message into a capped percentage', () => {
    expect(percentOf({ done: 1, total: 4 })).toBe(25)
    expect(percentOf({ done: 5, total: 4 })).toBe(100)
    expect(percentOf({ done: 1, total: 0 })).toBeNull()
  })
})

describe('registerServiceWorker', () => {
  beforeEach(() => {
    vi.useFakeTimers()
  })
  afterEach(() => {
    vi.useRealTimers()
    resetPwaUpdate()
  })

  it('does nothing in development or without service workers', () => {
    expect(registerServiceWorker()).toBeNull()
    expect(registerServiceWorker({ enabled: true })).toBeNull()
  })

  it('registers under the base path, not relative to the page', async () => {
    const { wb } = setup()
    expect(wb.url).toBe('/base/sw.js')
    expect(wb.options).toEqual({ scope: '/base/' })
    expect(wb.register).toHaveBeenCalled()
  })

  it('checks for a new version hourly and when the tab is shown, while online', async () => {
    const { wb } = setup()
    await flushPromises()
    vi.advanceTimersByTime(UPDATE_CHECK_INTERVAL)
    expect(wb.update).toHaveBeenCalledTimes(1)

    const visibility = vi.spyOn(document, 'visibilityState', 'get')
    visibility.mockReturnValue('hidden')
    document.dispatchEvent(new Event('visibilitychange'))
    expect(wb.update).toHaveBeenCalledTimes(1)
    visibility.mockReturnValue('visible')
    document.dispatchEvent(new Event('visibilitychange'))
    expect(wb.update).toHaveBeenCalledTimes(2)

    vi.spyOn(navigator, 'onLine', 'get').mockReturnValue(false)
    vi.advanceTimersByTime(UPDATE_CHECK_INTERVAL)
    expect(wb.update).toHaveBeenCalledTimes(2)
  })

  it('ignores a failed update check', async () => {
    const { wb } = setup({ update: () => Promise.reject(new Error('offline')) })
    await flushPromises()
    vi.advanceTimersByTime(UPDATE_CHECK_INTERVAL)
    await flushPromises()
    expect(wb.update).toHaveBeenCalled()
  })

  it('never polls when registration fails', async () => {
    const { wb } = setup({ register: () => Promise.reject(new Error('blocked')) })
    await flushPromises()
    vi.advanceTimersByTime(UPDATE_CHECK_INTERVAL)
    expect(wb.update).not.toHaveBeenCalled()
  })

  it('offers a waiting version and switches to it on request', async () => {
    const { wb, reloadPage } = setup()
    const { needRefresh, progress, update } = usePwaUpdate()
    progress.value = { phase: 'downloading', percent: 100 }
    wb.emit('waiting')
    expect(needRefresh.value).toBe(true)
    expect(progress.value).toBeNull()

    update()
    expect(needRefresh.value).toBe(false)
    expect(progress.value).toEqual({ phase: 'restarting', percent: null })
    expect(wb.messageSkipWaiting).toHaveBeenCalled()

    // The worker took over: reload into the new version.
    wb.emit('controlling', { isUpdate: true })
    expect(reloadPage).toHaveBeenCalledTimes(1)
  })

  it('reloads anyway when the new version never takes over', () => {
    const { reloadPage } = setup()
    usePwaUpdate().update()
    vi.advanceTimersByTime(RESTART_TIMEOUT)
    expect(reloadPage).toHaveBeenCalledTimes(1)
  })

  it('does not reload on a first install', () => {
    const { wb, reloadPage } = setup()
    wb.emit('controlling', { isUpdate: false })
    expect(reloadPage).not.toHaveBeenCalled()
  })

  it('reloads the page by default', () => {
    const wb = fakeWorkbox()
    const reload = vi.fn()
    vi.stubGlobal('location', { reload })
    registerServiceWorker({ container: fakeContainer(), createWorkbox: () => wb, enabled: true })
    wb.emit('controlling', { isUpdate: true })
    expect(reload).toHaveBeenCalled()
    vi.unstubAllGlobals()
  })

  it('builds a real Workbox by default', async () => {
    // Workbox reads navigator.serviceWorker itself, which jsdom lacks.
    const container = { ...fakeContainer(), register: vi.fn(async () => Promise.reject(new Error('no'))) }
    Object.defineProperty(navigator, 'serviceWorker', { value: container, configurable: true })
    try {
      const wb = registerServiceWorker({ enabled: true })
      expect(wb).toBeInstanceOf(Workbox)
      await vi.runAllTimersAsync()
      expect(container.register).toHaveBeenCalledWith('/sw.js', { scope: '/' })
    } finally {
      delete navigator.serviceWorker
    }
  })

  it('does nothing on update or dismiss without a registration', () => {
    const { needRefresh, progress, update, dismiss } = usePwaUpdate()
    update()
    expect(progress.value).toBeNull()
    needRefresh.value = true
    dismiss()
    expect(needRefresh.value).toBe(false)
  })

  it('shows download progress from the worker messages', () => {
    const { container } = setup()
    const { progress } = usePwaUpdate()
    const worker = fakeWorker()
    container.post({ type: PRECACHE_PROGRESS, done: 1, total: 4 }, worker)
    expect(progress.value).toEqual({ phase: 'downloading', percent: 25 })
    container.post({ type: PRECACHE_PROGRESS, done: 2, total: 4 }, worker)
    expect(progress.value.percent).toBe(50)
    worker.moveTo('installed')
    expect(progress.value).toBeNull()
  })
})

describe('onProgressMessage', () => {
  afterEach(() => {
    resetPwaUpdate()
  })

  const { progress } = usePwaUpdate()
  const message = { type: PRECACHE_PROGRESS, done: 1, total: 2 }

  it('ignores other messages and first installs', () => {
    onProgressMessage({ controller: {} })({ data: { type: 'OTHER' } })
    onProgressMessage({ controller: {} })({})
    onProgressMessage({ controller: null })({ data: message })
    expect(progress.value).toBeNull()
  })

  it('ignores a late message from a worker that finished installing', () => {
    onProgressMessage({ controller: {} })({ data: message, source: fakeWorker('installed') })
    expect(progress.value).toBeNull()
  })

  it('accepts a message without a source', () => {
    onProgressMessage({ controller: {} })({ data: message, source: null })
    expect(progress.value).toEqual({ phase: 'downloading', percent: 50 })
  })

  it('leaves a restart alone when the worker finishes', () => {
    const handle = onProgressMessage({ controller: {} })
    const worker = fakeWorker()
    handle({ data: message, source: worker })
    progress.value = { phase: 'restarting', percent: null }
    worker.moveTo('installing')
    worker.moveTo('activated')
    expect(progress.value.phase).toBe('restarting')
  })
})

describe('UpdateBanner', () => {
  afterEach(() => {
    resetPwaUpdate()
  })

  it('stays hidden with nothing to offer', () => {
    expect(mount(UpdateBanner).find('[data-test="update-banner"]').exists()).toBe(false)
  })

  it('shows the download progress', async () => {
    const wrapper = mount(UpdateBanner)
    usePwaUpdate().progress.value = { phase: 'downloading', percent: 42.7 }
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-test="update-label"]').text()).toContain('Downloading a new version')
    expect(wrapper.find('[data-test="update-percent"]').text()).toBe('42%')
    expect(wrapper.find('[data-test="update-progress"] div').attributes('style')).toContain('42.7%')
    expect(wrapper.find('[data-test="update-now"]').exists()).toBe(false)
  })

  it('sweeps while restarting', async () => {
    const wrapper = mount(UpdateBanner)
    usePwaUpdate().progress.value = { phase: 'restarting', percent: null }
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-test="update-label"]').text()).toContain('Restarting')
    expect(wrapper.find('[data-test="update-percent"]').exists()).toBe(false)
    expect(wrapper.find('.update-progress-sweep').exists()).toBe(true)
    expect(wrapper.find('[data-test="update-progress"]').attributes('aria-valuenow')).toBeUndefined()
  })

  it('offers the update, and dismisses it', async () => {
    vi.useFakeTimers()
    const wb = fakeWorkbox()
    registerServiceWorker({ container: fakeContainer(), createWorkbox: () => wb, enabled: true, reloadPage: vi.fn() })
    const { needRefresh } = usePwaUpdate()
    const wrapper = mount(UpdateBanner)
    needRefresh.value = true
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-test="update-label"]').text()).toBe('A new version is available.')
    await wrapper.find('[data-test="update-dismiss"]').trigger('click')
    expect(needRefresh.value).toBe(false)

    needRefresh.value = true
    await wrapper.vm.$nextTick()
    await wrapper.find('[data-test="update-now"]').trigger('click')
    expect(wb.messageSkipWaiting).toHaveBeenCalled()
    expect(wrapper.find('[data-test="update-label"]').text()).toContain('Restarting')
    vi.useRealTimers()
  })
})
