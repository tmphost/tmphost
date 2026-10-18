import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('../src/api', () => ({
  fetchSettings: vi.fn(),
  saveSettings: vi.fn(),
  setupLogin: vi.fn(),
  setupLogout: vi.fn(),
}))

import { fetchSettings, saveSettings, setupLogin, setupLogout } from '../src/api'
import SetupView from '../src/views/SetupView.vue'
import { useToasts } from '../src/composables/useToasts'

function state(overrides = {}) {
  return {
    settings: {
      credits: { monthlyGrant: 250 },
      database: {
        driver: 'sqlite',
        sqlite: { dsn: './_storage/tmphost.db' },
        postgres: { dsn: '' },
      },
      storage: { s3: { enabled: false, endpoint: '', region: '', bucket: '', accessKeyId: '', secretAccessKey: '', usePathStyle: false } },
      auth: { google: { clientId: 'g', clientSecret: '' }, github: { clientId: '', clientSecret: '' } },
    },
    secretsSet: { postgresDsn: true, s3SecretAccessKey: false, googleClientSecret: true, githubClientSecret: false },
    restartRequired: false,
    ...overrides,
  }
}

async function mountView() {
  const wrapper = mount(SetupView)
  await flushPromises()
  return wrapper
}

describe('SetupView', () => {
  beforeEach(() => {
    useToasts().toasts.value = []
  })
  afterEach(() => {
    useToasts().toasts.value = []
  })

  it('shows a spinner while loading', () => {
    fetchSettings.mockReturnValue(new Promise(() => {}))
    expect(mount(SetupView).find('[data-test="setup-loading"]').exists()).toBe(true)
  })

  it('asks for the root token without a session, then loads settings', async () => {
    fetchSettings.mockResolvedValueOnce(null).mockResolvedValueOnce(state())
    setupLogin.mockResolvedValue()
    const wrapper = await mountView()
    expect(wrapper.find('[data-test="setup-login"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="setup-unlock"]').attributes('disabled')).toBeDefined()

    await wrapper.find('[data-test="setup-token"]').setValue('root-token')
    await wrapper.find('[data-test="setup-login"]').trigger('submit')
    await flushPromises()
    expect(setupLogin).toHaveBeenCalledWith('root-token')
    expect(wrapper.find('[data-test="setup-form"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="setup-grant"]').element.value).toBe('250')
  })

  it('shows a wrong-token error', async () => {
    fetchSettings.mockResolvedValue(null)
    setupLogin.mockRejectedValue(new Error('invalid root access token'))
    const wrapper = await mountView()
    await wrapper.find('[data-test="setup-token"]').setValue('nope')
    await wrapper.find('[data-test="setup-login"]').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-test="setup-login-error"]').text()).toBe('invalid root access token')
  })

  it('falls back to sign-in when loading fails', async () => {
    fetchSettings.mockRejectedValue(new Error('Failed to load settings (500)'))
    const wrapper = await mountView()
    expect(wrapper.find('[data-test="setup-login"]').exists()).toBe(true)
    expect(useToasts().toasts.value[0].message).toBe('Failed to load settings (500)')
  })

  it('renders sections, masks saved secrets and switches database driver', async () => {
    fetchSettings.mockResolvedValue(state())
    const wrapper = await mountView()
    expect(wrapper.find('[data-test="setup-sqlite"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="setup-restart"]').exists()).toBe(false)

    await wrapper.find('[data-test="setup-driver-postgres"]').trigger('click')
    expect(wrapper.find('[data-test="setup-postgres"]').exists()).toBe(true)
    expect(wrapper.find('#pg-dsn').attributes('placeholder')).toContain('saved')
    expect(wrapper.find('#pg-dsn').attributes('type')).toBe('password')
    expect(wrapper.find('#s3-secret').attributes('placeholder')).toBe('Not set')
    expect(wrapper.find('#google-secret').attributes('placeholder')).toContain('saved')
    expect(wrapper.find('#github-secret').attributes('placeholder')).toBe('Not set')

    await wrapper.find('[data-test="setup-driver-sqlite"]').trigger('click')
    expect(wrapper.find('[data-test="setup-sqlite"]').exists()).toBe(true)
  })

  it('shows an example DSN when none is saved', async () => {
    fetchSettings.mockResolvedValue(state({ secretsSet: { ...state().secretsSet, postgresDsn: false } }))
    const wrapper = await mountView()
    await wrapper.find('[data-test="setup-driver-postgres"]').trigger('click')
    expect(wrapper.find('#pg-dsn').attributes('placeholder')).toMatch(/^postgres:\/\//)
  })

  it('saves the edited settings and shows restart notice', async () => {
    fetchSettings.mockResolvedValue(state())
    saveSettings.mockImplementation(async (settings) => state({ settings: { ...settings, auth: { ...settings.auth, github: { clientId: 'h', clientSecret: '' } } }, restartRequired: true }))
    const wrapper = await mountView()

    await wrapper.find('[data-test="setup-grant"]').setValue('400')
    await wrapper.find('[data-test="setup-s3-enabled"]').setValue(true)
    await wrapper.find('#s3-bucket').setValue('files')
    await wrapper.find('#github-id').setValue('h')
    await wrapper.find('#github-secret').setValue('hsecret')
    await wrapper.find('[data-test="setup-form"]').trigger('submit')
    await flushPromises()

    const sent = saveSettings.mock.calls[0][0]
    expect(sent.credits.monthlyGrant).toBe(400)
    expect(sent.storage.s3).toMatchObject({ enabled: true, bucket: 'files' })
    expect(sent.auth.github).toEqual({ clientId: 'h', clientSecret: 'hsecret' })
    expect(wrapper.find('[data-test="setup-restart"]').exists()).toBe(true)
    expect(wrapper.find('#github-secret').element.value).toBe('', 'secret cleared after save')
    expect(useToasts().toasts.value.at(-1)).toMatchObject({ type: 'success', message: 'Settings saved' })
  })

  it('binds every field', async () => {
    fetchSettings.mockResolvedValue(state())
    saveSettings.mockImplementation(async (settings) => state({ settings }))
    const wrapper = await mountView()
    await wrapper.find('#sqlite-dsn').setValue('/data/x.db')
    await wrapper.find('[data-test="setup-driver-postgres"]').trigger('click')
    const values = {
      '#pg-dsn': 'postgres://app:pw@db:6543/prod?sslmode=require',
      '#s3-endpoint': 'https://s3.example.com', '#s3-region': 'auto', '#s3-key': 'AK', '#s3-secret': 'SK',
      '#google-id': 'g2', '#google-secret': 'gs2',
    }
    for (const [sel, value] of Object.entries(values)) await wrapper.find(sel).setValue(value)
    await wrapper.find('input[type="checkbox"]:not([data-test])').setValue(true)
    await wrapper.find('[data-test="setup-form"]').trigger('submit')
    await flushPromises()

    expect(saveSettings.mock.calls[0][0]).toMatchObject({
      database: {
        driver: 'postgres',
        sqlite: { dsn: '/data/x.db' },
        postgres: { dsn: 'postgres://app:pw@db:6543/prod?sslmode=require' },
      },
      storage: { s3: { endpoint: 'https://s3.example.com', region: 'auto', accessKeyId: 'AK', secretAccessKey: 'SK', usePathStyle: true } },
      auth: { google: { clientId: 'g2', clientSecret: 'gs2' } },
    })
  })

  it('shows validation errors from the server', async () => {
    fetchSettings.mockResolvedValue(state())
    saveSettings.mockRejectedValue(Object.assign(new Error('invalid settings: sqlite DSN is required'), { status: 400 }))
    const wrapper = await mountView()
    await wrapper.find('[data-test="setup-form"]').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-test="setup-save-error"]').text()).toContain('sqlite DSN is required')
    expect(useToasts().toasts.value.at(-1).type).toBe('error')
  })

  it('returns to sign-in when the root session expired', async () => {
    fetchSettings.mockResolvedValue(state())
    saveSettings.mockRejectedValue(Object.assign(new Error('unauthorized'), { status: 401 }))
    const wrapper = await mountView()
    await wrapper.find('[data-test="setup-form"]').trigger('submit')
    await flushPromises()
    expect(wrapper.find('[data-test="setup-login-error"]').text()).toContain('session expired')
  })

  it('signs out, even if the request fails', async () => {
    for (const fail of [false, true]) {
      fetchSettings.mockResolvedValue(state())
      if (fail) setupLogout.mockRejectedValueOnce(new Error('offline'))
      else setupLogout.mockResolvedValueOnce()
      const wrapper = await mountView()
      await wrapper.find('[data-test="setup-logout"]').trigger('click')
      await flushPromises()
      expect(wrapper.find('[data-test="setup-login"]').exists()).toBe(true)
      wrapper.unmount()
    }
  })
})
