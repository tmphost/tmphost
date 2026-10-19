// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('../src/api', () => ({ fetchFolder: vi.fn(), extendFolder: vi.fn() }))

import { extendFolder, fetchFolder } from '../src/api'
import ExtendModal from '../src/components/ExtendModal.vue'
import { useToasts } from '../src/composables/useToasts'

const NOW = Date.parse('2026-10-18T12:00:00Z')
const limits = { maxTtlMinutes: 432000, billingBlockMinutes: 30, billingUnitBytes: 100 }
// Expires in 45 minutes.
const folder = { id: 'f1', expiresAt: '2026-10-18T12:45:00Z' }
const files = [{ size: 2048 }, { size: 12 }]

function deferred() {
  let resolve, reject
  const promise = new Promise((res, rej) => ((resolve = res), (reject = rej)))
  return { promise, resolve, reject }
}

function mountModal(props = {}) {
  return mount(ExtendModal, { props: { folder, title: 'Report', limits, credits: 100, now: NOW, ...props } })
}

const lastToast = () => useToasts().toasts.value.at(-1)
const find = (wrapper, test) => wrapper.find(`[data-test="${test}"]`)

describe('ExtendModal', () => {
  beforeEach(() => {
    useToasts().toasts.value = []
    fetchFolder.mockResolvedValue({ folder, files })
  })
  afterEach(() => {
    vi.resetAllMocks()
  })

  it('renders nothing without a folder', () => {
    const wrapper = mountModal({ folder: null })
    expect(find(wrapper, 'extend-modal').exists()).toBe(false)
    expect(fetchFolder).not.toHaveBeenCalled()
  })

  it('slides through the extensions and prices them per file', async () => {
    const listing = deferred()
    fetchFolder.mockReturnValueOnce(listing.promise)
    const wrapper = mountModal()
    expect(wrapper.find('#extend-modal-title').text()).toBe('Extend Report')
    expect(find(wrapper, 'extend-now').text()).toContain('Expires in 45m.')
    expect(find(wrapper, 'extend-cost').text()).toBe('Loading files…')
    expect(find(wrapper, 'extend-confirm').attributes('disabled')).toBeDefined()
    listing.resolve({ folder, files })
    await flushPromises()
    expect(fetchFolder).toHaveBeenCalledWith('f1')

    expect(find(wrapper, 'extend-value').text()).toBe('30 min')
    expect(find(wrapper, 'extend-slider').attributes('max')).toBe('6')
    // 21 + 1 billing units, one started block.
    expect(find(wrapper, 'extend-cost').text()).toBe('New expiry in 1h 15m · 22 credits')
    await find(wrapper, 'extend-slider').setValue(2)
    expect(find(wrapper, 'extend-value').text()).toBe('2 h')
    expect(find(wrapper, 'extend-slider').attributes('aria-valuetext')).toBe('2 h')
    expect(find(wrapper, 'extend-cost').text()).toBe('New expiry in 2h 45m · 88 credits')
    expect(find(wrapper, 'extend-credits-warning').exists()).toBe(false)
  })

  it('extends the folder and reports what it cost', async () => {
    const extended = deferred()
    extendFolder.mockReturnValueOnce(extended.promise)
    const wrapper = mountModal()
    await flushPromises()
    await find(wrapper, 'extend-slider').setValue(1)
    await find(wrapper, 'extend-confirm').trigger('click')
    expect(extendFolder).toHaveBeenCalledWith('f1', 60)
    expect(find(wrapper, 'extend-confirm').text()).toBe('Extending…')
    expect(find(wrapper, 'extend-slider').attributes('disabled')).toBeDefined()

    // Busy: neither the backdrop, Escape nor Cancel close it.
    await find(wrapper, 'extend-modal').trigger('click')
    await find(wrapper, 'extend-modal').trigger('keydown', { key: 'Escape' })
    await find(wrapper, 'extend-cancel').trigger('click')
    expect(wrapper.emitted('cancel')).toBeUndefined()

    const updated = { ...folder, expiresAt: '2026-10-18T13:45:00Z' }
    extended.resolve({ folder: updated, creditsSpent: 44 })
    await flushPromises()
    expect(wrapper.emitted('extended')).toEqual([[{ folder: updated, creditsSpent: 44, minutes: 60 }]])
  })

  it('toasts a failed extension and can be retried', async () => {
    extendFolder.mockRejectedValueOnce(new Error('insufficient credits: 22 needed'))
    const wrapper = mountModal()
    await flushPromises()
    await find(wrapper, 'extend-confirm').trigger('click')
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'insufficient credits: 22 needed' })
    expect(find(wrapper, 'extend-confirm').text()).toBe('Extend')
    expect(wrapper.emitted('extended')).toBeUndefined()
  })

  it('closes on the backdrop, Escape and Cancel, not on clicks inside', async () => {
    const wrapper = mountModal()
    await flushPromises()
    await wrapper.find('[role="dialog"]').trigger('click')
    expect(wrapper.emitted('cancel')).toBeUndefined()
    await find(wrapper, 'extend-modal').trigger('click')
    await find(wrapper, 'extend-modal').trigger('keydown', { key: 'Escape' })
    await find(wrapper, 'extend-cancel').trigger('click')
    expect(wrapper.emitted('cancel')).toHaveLength(3)
  })

  it('warns when the credits left do not cover it', async () => {
    const wrapper = mountModal({ credits: 10 })
    await flushPromises()
    expect(find(wrapper, 'extend-credits-warning').text()).toBe('This costs 22 credits but you have 10 left.')
    const unknown = mountModal({ credits: undefined })
    await flushPromises()
    expect(find(unknown, 'extend-credits-warning').exists()).toBe(false)
  })

  it('says when the folder cannot live any longer', async () => {
    const wrapper = mountModal({ limits: { ...limits, maxTtlMinutes: 60 } })
    await flushPromises()
    expect(find(wrapper, 'extend-maxed').text()).toBe('This folder already lives as long as it can.')
    expect(find(wrapper, 'extend-slider').exists()).toBe(false)
    expect(find(wrapper, 'extend-confirm').exists()).toBe(false)
    expect(find(wrapper, 'extend-cancel').text()).toBe('Close')

    // Without limits there is nothing to offer either.
    const noLimits = mountModal({ limits: null, folder: { id: 'f2', expiresAt: '2026-10-18T11:00:00Z' } })
    await flushPromises()
    expect(find(noLimits, 'extend-maxed').exists()).toBe(true)
  })

  it('closes when the files cannot be listed', async () => {
    fetchFolder.mockRejectedValueOnce(new Error('folder not found'))
    const wrapper = mountModal()
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'folder not found' })
    expect(wrapper.emitted('cancel')).toHaveLength(1)
  })

  it('ignores a slow listing once another folder is shown', async () => {
    const slow = deferred()
    const failing = deferred()
    fetchFolder.mockReturnValueOnce(slow.promise).mockReturnValueOnce(failing.promise)
    const wrapper = mountModal()
    await wrapper.setProps({ folder: { id: 'f2', expiresAt: folder.expiresAt } })
    await wrapper.setProps({ folder: null })
    slow.resolve({ folder, files })
    failing.reject(new Error('late'))
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'late' })
    expect(wrapper.emitted('cancel')).toBeUndefined()

    // Reopening loads the folder afresh.
    await wrapper.setProps({ folder })
    await flushPromises()
    expect(find(wrapper, 'extend-cost').text()).toBe('New expiry in 1h 15m · 22 credits')
  })
})
