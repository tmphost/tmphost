import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'

vi.mock('../src/api', () => ({
  fetchAuthConfig: vi.fn(),
  providerLoginUrl: (provider, redirect) => `/api/v1/auth/${provider}/login${redirect ? `?redirect_url=${redirect}` : ''}`,
}))

import { fetchAuthConfig } from '../src/api'
import LoginView from '../src/views/LoginView.vue'
import { useToasts } from '../src/composables/useToasts'

async function mountAt(path) {
  const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/login', component: LoginView }, { path: '/setup', component: { render: () => null } }] })
  await router.push(path)
  const wrapper = mount(LoginView, { global: { plugins: [router] } })
  return wrapper
}

describe('LoginView', () => {
  beforeEach(() => {
    useToasts().toasts.value = []
  })
  afterEach(() => {
    useToasts().toasts.value = []
  })

  it('shows the brand and a spinner while loading', async () => {
    fetchAuthConfig.mockReturnValue(new Promise(() => {}))
    const wrapper = await mountAt('/login')
    expect(wrapper.text()).toContain('tmphost.io')
    expect(wrapper.find('[data-test="brand-logo"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="login-loading"]').exists()).toBe(true)
  })

  it('renders a button per known provider, carrying the redirect', async () => {
    fetchAuthConfig.mockResolvedValue(['google', 'github', 'myspace'])
    const wrapper = await mountAt('/login?redirect=/deep')
    await flushPromises()
    const google = wrapper.find('[data-test="login-google"]')
    const github = wrapper.find('[data-test="login-github"]')
    expect(google.text()).toContain('Continue with Google')
    expect(google.attributes('href')).toBe('/api/v1/auth/google/login?redirect_url=/deep')
    expect(github.attributes('href')).toBe('/api/v1/auth/github/login?redirect_url=/deep')
    expect(wrapper.find('[data-test="login-myspace"]').exists()).toBe(false)
    expect(wrapper.text()).toContain('250 free operations')
    expect(wrapper.find('[data-test="login-setup"]').attributes('href')).toBe('/setup')
  })

  it('ignores a non-string redirect', async () => {
    fetchAuthConfig.mockResolvedValue(['google'])
    const wrapper = await mountAt('/login?redirect=a&redirect=b')
    await flushPromises()
    expect(wrapper.find('[data-test="login-google"]').attributes('href')).toBe('/api/v1/auth/google/login')
  })

  it('explains when no provider is configured', async () => {
    fetchAuthConfig.mockResolvedValue([])
    const wrapper = await mountAt('/login')
    await flushPromises()
    expect(wrapper.find('[data-test="login-no-providers"]').exists()).toBe(true)
  })

  it('reports a config failure', async () => {
    fetchAuthConfig.mockRejectedValue(new Error('Failed to load sign-in options (503)'))
    const wrapper = await mountAt('/login')
    await flushPromises()
    expect(wrapper.find('[data-test="login-config-error"]').text()).toBe('Sign-in is unavailable right now.')
    expect(useToasts().toasts.value[0]).toMatchObject({ type: 'error', message: 'Failed to load sign-in options (503)' })
  })

  it('shows the callback error from the query', async () => {
    fetchAuthConfig.mockResolvedValue(['google'])
    const wrapper = await mountAt('/login?error=invalid_state')
    await flushPromises()
    expect(wrapper.find('[data-test="login-error"]').text()).toContain('sign-in session expired')
  })
})
