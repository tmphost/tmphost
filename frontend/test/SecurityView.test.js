// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('../src/api', () => ({ fetchAccountPin: vi.fn(), newTotp: vi.fn(), saveAccountPin: vi.fn() }))

import { fetchAccountPin, newTotp, saveAccountPin } from '../src/api'
import SecurityView from '../src/views/SecurityView.vue'
import { useToasts } from '../src/composables/useToasts'

const TOTP = { secret: 'JBSWY3DPEHPK3PXP', url: 'otpauth://totp/x', qrCode: 'data:image/png;base64,AAAA' }

function deferred() {
  let resolve, reject
  const promise = new Promise((res, rej) => ((resolve = res), (reject = rej)))
  return { promise, resolve, reject }
}

async function mountView(settings = { mode: 'off', agentPins: false }) {
  fetchAccountPin.mockResolvedValueOnce(settings)
  const wrapper = mount(SecurityView)
  await flushPromises()
  return wrapper
}

const lastToast = () => useToasts().toasts.value.at(-1)
const find = (wrapper, test) => wrapper.find(`[data-test="${test}"]`)
const saveDisabled = (wrapper) => find(wrapper, 'security-save').attributes('disabled') !== undefined

async function choose(wrapper, mode) {
  await find(wrapper, `pin-mode-${mode}`).find('input').setValue()
}

describe('SecurityView', () => {
  beforeEach(() => {
    useToasts().toasts.value = []
  })
  afterEach(() => {
    vi.resetAllMocks()
  })

  it('loads the settings', async () => {
    const load = deferred()
    fetchAccountPin.mockReturnValueOnce(load.promise)
    const wrapper = mount(SecurityView)
    expect(find(wrapper, 'security-loading').exists()).toBe(true)
    load.resolve({ mode: 'static', agentPins: true })
    await flushPromises()
    expect(find(wrapper, 'security-form').exists()).toBe(true)
    expect(find(wrapper, 'pin-mode-static').find('input').element.checked).toBe(true)
    expect(find(wrapper, 'pin-mode-static').find('[data-test="pin-mode-current"]').exists()).toBe(true)
    expect(find(wrapper, 'pin-mode-off').find('[data-test="pin-mode-current"]').exists()).toBe(false)
    expect(find(wrapper, 'agent-pins').find('input').element.checked).toBe(true)
  })

  it('reports a failed load', async () => {
    fetchAccountPin.mockRejectedValueOnce(new Error('Failed to load PIN settings'))
    const wrapper = mount(SecurityView)
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'Failed to load PIN settings' })
    expect(find(wrapper, 'security-loading').exists()).toBe(true)
  })

  it('sets a static PIN', async () => {
    const wrapper = await mountView()
    expect(saveDisabled(wrapper)).toBe(false)
    await choose(wrapper, 'static')
    const input = find(wrapper, 'static-pin')
    expect(wrapper.find('label[for="static-pin"]').text()).toBe('PIN')
    expect(input.attributes('placeholder')).toBe('6 digits')
    expect(saveDisabled(wrapper)).toBe(true)
    await input.setValue('12a34')
    expect(input.element.value).toBe('1234')
    expect(saveDisabled(wrapper)).toBe(true)
    await input.setValue('1234567')
    expect(input.element.value).toBe('123456')
    expect(saveDisabled(wrapper)).toBe(false)

    const save = deferred()
    saveAccountPin.mockReturnValueOnce(save.promise)
    await find(wrapper, 'security-form').trigger('submit')
    expect(saveAccountPin).toHaveBeenCalledWith({ mode: 'static', agentPins: false, pin: '123456' })
    expect(find(wrapper, 'security-save').text()).toBe('Saving…')
    expect(saveDisabled(wrapper)).toBe(true)
    save.resolve({ mode: 'static', agentPins: false })
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'success', message: 'PIN settings saved' })
    expect(find(wrapper, 'static-pin').element.value).toBe('')
    expect(wrapper.find('label[for="static-pin"]').text()).toBe('New PIN')
    expect(find(wrapper, 'static-pin').attributes('placeholder')).toBe('Leave empty to keep the current PIN')
  })

  it('keeps the static PIN and changes the agent setting', async () => {
    const wrapper = await mountView({ mode: 'static', agentPins: false })
    expect(saveDisabled(wrapper)).toBe(false)
    await find(wrapper, 'agent-pins').find('input').setValue(true)
    saveAccountPin.mockResolvedValueOnce({ mode: 'static', agentPins: true })
    await find(wrapper, 'security-form').trigger('submit')
    await flushPromises()
    expect(saveAccountPin).toHaveBeenCalledWith({ mode: 'static', agentPins: true })
  })

  it('sets up an authenticator app', async () => {
    const wrapper = await mountView()
    await choose(wrapper, 'totp')
    expect(find(wrapper, 'totp-set').exists()).toBe(false)
    expect(find(wrapper, 'totp-setup').text()).toBe('Show QR code')
    expect(saveDisabled(wrapper)).toBe(true)

    const key = deferred()
    newTotp.mockReturnValueOnce(key.promise)
    await find(wrapper, 'totp-setup').trigger('click')
    expect(find(wrapper, 'totp-setup').text()).toBe('Loading…')
    expect(find(wrapper, 'totp-setup').attributes('disabled')).toBeDefined()
    key.resolve(TOTP)
    await flushPromises()
    expect(find(wrapper, 'totp-qr').attributes('src')).toBe(TOTP.qrCode)
    expect(find(wrapper, 'totp-secret').text()).toBe(TOTP.secret)
    expect(saveDisabled(wrapper)).toBe(true)
    await find(wrapper, 'totp-code').setValue('12 34 56')
    expect(find(wrapper, 'totp-code').element.value).toBe('123456')
    expect(saveDisabled(wrapper)).toBe(false)

    saveAccountPin.mockResolvedValueOnce({ mode: 'totp', agentPins: false })
    await find(wrapper, 'security-form').trigger('submit')
    await flushPromises()
    expect(saveAccountPin).toHaveBeenCalledWith({ mode: 'totp', agentPins: false, secret: TOTP.secret, code: '123456' })
    expect(find(wrapper, 'totp-qr').exists()).toBe(false)
    expect(find(wrapper, 'totp-set').exists()).toBe(true)
    expect(find(wrapper, 'totp-setup').text()).toBe('Use another app')
    expect(saveDisabled(wrapper)).toBe(false)
  })

  it('reports a failed authenticator setup', async () => {
    const wrapper = await mountView({ mode: 'totp', agentPins: false })
    newTotp.mockRejectedValueOnce(new Error('nope'))
    await find(wrapper, 'totp-setup').trigger('click')
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'nope' })
    expect(find(wrapper, 'totp-setup').attributes('disabled')).toBeUndefined()
  })

  it('turns the account PIN off and reports a failed save', async () => {
    const wrapper = await mountView({ mode: 'totp', agentPins: true })
    await choose(wrapper, 'off')
    expect(find(wrapper, 'totp').exists()).toBe(false)
    saveAccountPin.mockRejectedValueOnce(new Error('Failed to save'))
    await find(wrapper, 'security-form').trigger('submit')
    await flushPromises()
    expect(saveAccountPin).toHaveBeenCalledWith({ mode: 'off', agentPins: true })
    expect(lastToast()).toMatchObject({ type: 'error', message: 'Failed to save' })
    expect(find(wrapper, 'pin-mode-off').find('input').element.checked).toBe(true)
    expect(saveDisabled(wrapper)).toBe(false)
  })
})
