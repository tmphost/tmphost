// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('../src/api', () => ({ createFolderPin: vi.fn() }))

import { createFolderPin } from '../src/api'
import PinModal from '../src/components/PinModal.vue'
import { useToasts } from '../src/composables/useToasts'

const folder = { id: 'f1', pinMode: 'off', openUrl: 'https://f.test/f1/' }

function deferred() {
  let resolve, reject
  const promise = new Promise((res, rej) => ((resolve = res), (reject = rej)))
  return { promise, resolve, reject }
}

const lastToast = () => useToasts().toasts.value.at(-1)
const find = (wrapper, test) => wrapper.find(`[data-test="${test}"]`)

describe('PinModal', () => {
  let writeText
  beforeEach(() => {
    useToasts().toasts.value = []
    writeText = vi.fn().mockResolvedValue()
    Object.defineProperty(navigator, 'clipboard', { value: { writeText }, configurable: true })
  })
  afterEach(() => {
    vi.resetAllMocks()
    delete navigator.clipboard
  })

  it('renders nothing without a folder', () => {
    const wrapper = mount(PinModal, { props: { folder: null } })
    expect(find(wrapper, 'pin-modal').exists()).toBe(false)
  })

  it('adds a PIN and shows it once', async () => {
    const wrapper = mount(PinModal, { props: { folder, title: 'Report' } })
    expect(wrapper.find('#pin-modal-title').text()).toBe('Add a PIN to Report')
    expect(find(wrapper, 'pin-message').text()).toContain('before they see')
    expect(find(wrapper, 'pin-confirm').text()).toBe('Add PIN')

    const made = deferred()
    createFolderPin.mockReturnValueOnce(made.promise)
    await find(wrapper, 'pin-confirm').trigger('click')
    expect(createFolderPin).toHaveBeenCalledWith('f1')
    expect(find(wrapper, 'pin-confirm').text()).toBe('Making…')
    // Busy, it can't be closed.
    await find(wrapper, 'pin-cancel').trigger('click')
    await find(wrapper, 'pin-modal').trigger('keydown', { key: 'Escape' })
    expect(wrapper.emitted('close')).toBeUndefined()

    made.resolve({ ...folder, pinMode: 'folder', pin: '042917' })
    await flushPromises()
    expect(wrapper.emitted('changed')).toEqual([[{ ...folder, pinMode: 'folder', pin: '042917' }]])
    expect(wrapper.find('#pin-modal-title').text()).toBe('PIN for Report')
    expect(find(wrapper, 'pin-value').text()).toBe('042917')

    await find(wrapper, 'pin-copy').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenLastCalledWith('042917')
    expect(lastToast()).toMatchObject({ type: 'success', message: 'PIN copied' })
    await find(wrapper, 'pin-copy-link').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenLastCalledWith('https://f.test/f1/?pin=042917')
    expect(lastToast()).toMatchObject({ type: 'success', message: 'Link with PIN copied' })

    await find(wrapper, 'pin-done').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(1)
  })

  it('replaces a PIN, and starts over for the next folder', async () => {
    const wrapper = mount(PinModal, { props: { folder: { id: 'f2', pinMode: 'folder', openUrl: 'https://f.test/?a=1' }, title: 'Site' } })
    expect(wrapper.find('#pin-modal-title').text()).toBe('New PIN for Site')
    expect(find(wrapper, 'pin-message').text()).toContain('stops working')
    expect(find(wrapper, 'pin-confirm').text()).toBe('New PIN')
    createFolderPin.mockResolvedValueOnce({ id: 'f2', pinMode: 'folder', pin: '111111' })
    await find(wrapper, 'pin-confirm').trigger('click')
    await flushPromises()
    await find(wrapper, 'pin-copy-link').trigger('click')
    await flushPromises()
    expect(writeText).toHaveBeenLastCalledWith('https://f.test/?a=1&pin=111111')

    await wrapper.setProps({ folder: { id: 'f3' } })
    expect(find(wrapper, 'pin-value').exists()).toBe(false)
    expect(find(wrapper, 'pin-confirm').text()).toBe('Add PIN')
    await wrapper.setProps({ folder: null })
    expect(find(wrapper, 'pin-modal').exists()).toBe(false)
  })

  it('has no link to copy without an open URL', async () => {
    const wrapper = mount(PinModal, { props: { folder: { id: 'f4' } } })
    createFolderPin.mockResolvedValueOnce({ id: 'f4', pinMode: 'folder', pin: '222222' })
    await find(wrapper, 'pin-confirm').trigger('click')
    await flushPromises()
    expect(find(wrapper, 'pin-value').text()).toBe('222222')
    expect(find(wrapper, 'pin-copy-link').exists()).toBe(false)
  })

  it('reports a failure and closes on cancel', async () => {
    const wrapper = mount(PinModal, { props: { folder } })
    createFolderPin.mockRejectedValueOnce(new Error('Failed to make a PIN'))
    await find(wrapper, 'pin-confirm').trigger('click')
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'Failed to make a PIN' })
    expect(wrapper.emitted('changed')).toBeUndefined()
    expect(find(wrapper, 'pin-confirm').attributes('disabled')).toBeUndefined()
    await find(wrapper, 'pin-modal').trigger('click')
    await find(wrapper, 'pin-cancel').trigger('click')
    expect(wrapper.emitted('close')).toHaveLength(2)
  })
})
