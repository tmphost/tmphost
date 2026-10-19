// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('../src/api', () => ({
  fetchSettings: vi.fn(),
  grantCredits: vi.fn(),
  setupLogin: vi.fn(),
  setupLogout: vi.fn(),
}))

import { fetchSettings, grantCredits, setupLogin, setupLogout } from '../src/api'
import AdminView from '../src/views/AdminView.vue'
import { useToasts } from '../src/composables/useToasts'

const EXPIRES = '2026-11-18T00:00:00Z'

async function mountView() {
  const wrapper = mount(AdminView)
  await flushPromises()
  return wrapper
}

async function signedIn() {
  fetchSettings.mockResolvedValue({ settings: {} })
  return mountView()
}

function deferred() {
  let resolve
  const promise = new Promise((r) => {
    resolve = r
  })
  return { promise, resolve }
}

describe('AdminView', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    useToasts().toasts.value = []
  })
  afterEach(() => {
    useToasts().toasts.value = []
  })

  it('shows a spinner while checking the root session', () => {
    fetchSettings.mockReturnValue(new Promise(() => {}))
    expect(mount(AdminView).find('[data-test="admin-loading"]').exists()).toBe(true)
  })

  it('asks for the root token without a session, then opens the admin page', async () => {
    fetchSettings.mockResolvedValueOnce(null).mockResolvedValueOnce({ settings: {} })
    setupLogin.mockResolvedValue()
    const wrapper = await mountView()
    expect(wrapper.find('[data-test="admin-login"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="admin-unlock"]').attributes('disabled')).toBeDefined()

    await wrapper.find('[data-test="admin-token"]').setValue('root-token')
    await wrapper.find('[data-test="admin-login"]').trigger('submit')
    await flushPromises()
    expect(setupLogin).toHaveBeenCalledWith('root-token')
    expect(wrapper.find('[data-test="admin-page"]').exists()).toBe(true)
  })

  it('shows a wrong-token error', async () => {
    fetchSettings.mockResolvedValue(null)
    setupLogin.mockRejectedValue(new Error('invalid root access token'))
    const wrapper = await mountView()
    await wrapper.find('[data-test="admin-token"]').setValue('nope')
    await wrapper.find('[data-test="admin-login"]').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-test="admin-login-error"]').text()).toBe('invalid root access token')
  })

  it('falls back to sign-in when the session check fails', async () => {
    fetchSettings.mockRejectedValue(new Error('Failed to load settings (500)'))
    const wrapper = await mountView()
    expect(wrapper.find('[data-test="admin-login"]').exists()).toBe(true)
    expect(useToasts().toasts.value[0].message).toBe('Failed to load settings (500)')
  })

  it('grants credits to all users', async () => {
    const wrapper = await signedIn()
    const pending = deferred()
    grantCredits.mockReturnValue(pending.promise)
    expect(wrapper.find('[data-test="admin-email"]').exists()).toBe(false)
    expect(wrapper.find('[data-test="admin-grant-submit"]').text()).toBe('Grant to all users')

    await wrapper.find('[data-test="admin-credits"]').setValue('500')
    await wrapper.find('[data-test="admin-grant"]').trigger('submit')
    expect(grantCredits).toHaveBeenCalledWith({ credits: 500, email: '', all: true })
    expect(wrapper.find('[data-test="admin-grant-submit"]').attributes('disabled')).toBeDefined()

    pending.resolve({ users: 3, expiresAt: EXPIRES })
    await flushPromises()
    expect(wrapper.find('[data-test="admin-grant-result"]').text()).toContain('Granted 500 credits to 3 users')
    expect(useToasts().toasts.value[0].message).toBe('Granted 500 credits to 3 users')
  })

  it('grants credits to one user by email', async () => {
    const wrapper = await signedIn()
    grantCredits.mockResolvedValue({ users: 1, expiresAt: EXPIRES })
    await wrapper.find('[data-test="admin-target-one"]').trigger('click')
    expect(wrapper.find('[data-test="admin-target-one"]').attributes('aria-checked')).toBe('true')
    expect(wrapper.find('[data-test="admin-grant-submit"]').text()).toBe('Grant')
    expect(wrapper.find('[data-test="admin-grant-submit"]').attributes('disabled')).toBeDefined()

    await wrapper.find('[data-test="admin-email"]').setValue(' a@b.c ')
    await wrapper.find('[data-test="admin-grant"]').trigger('submit')
    await flushPromises()
    expect(grantCredits).toHaveBeenCalledWith({ credits: 100, email: 'a@b.c', all: false })
    expect(wrapper.find('[data-test="admin-grant-result"]').text()).toContain('Granted 100 credits to a@b.c')
    expect(useToasts().toasts.value[0].message).toBe('Granted 100 credits to 1 user')
  })

  it('names a single user when everyone is one user', async () => {
    const wrapper = await signedIn()
    grantCredits.mockResolvedValue({ users: 1, expiresAt: EXPIRES })
    await wrapper.find('[data-test="admin-grant"]').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-test="admin-grant-result"]').text()).toContain('Granted 100 credits to 1 user.')
  })

  it.each([['0'], ['1.5'], ['1000001'], ['']])('does not grant %s credits', async (value) => {
    const wrapper = await signedIn()
    await wrapper.find('[data-test="admin-credits"]').setValue(value)
    expect(wrapper.find('[data-test="admin-grant-submit"]').attributes('disabled')).toBeDefined()
  })

  it('shows grant errors in place of the last result', async () => {
    const wrapper = await signedIn()
    grantCredits.mockResolvedValueOnce({ users: 2, expiresAt: EXPIRES })
    await wrapper.find('[data-test="admin-grant"]').trigger('submit')
    await flushPromises()
    grantCredits.mockRejectedValue(Object.assign(new Error('user not found'), { status: 404 }))
    await wrapper.find('[data-test="admin-grant"]').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-test="admin-grant-error"]').text()).toBe('user not found')
    expect(wrapper.find('[data-test="admin-grant-result"]').exists()).toBe(false)
  })

  it('asks to sign in again when the root session expired', async () => {
    const wrapper = await signedIn()
    grantCredits.mockRejectedValue(Object.assign(new Error('unauthorized'), { status: 401 }))
    await wrapper.find('[data-test="admin-grant"]').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-test="admin-login"]').exists()).toBe(true)
  })

  it('signs out, even when the server call fails', async () => {
    const wrapper = await signedIn()
    setupLogout.mockRejectedValueOnce(new Error('offline'))
    await wrapper.find('[data-test="admin-logout"]').trigger('click')
    await flushPromises()
    expect(setupLogout).toHaveBeenCalled()
    expect(wrapper.find('[data-test="admin-login"]').exists()).toBe(true)
  })
})
