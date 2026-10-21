// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'

vi.mock('../src/api', () => ({ fetchAdminUser: vi.fn(), listAdminUserCreditActivities: vi.fn() }))

import AdminUserDetail from '../src/components/AdminUserDetail.vue'
import { fetchAdminUser, listAdminUserCreditActivities } from '../src/api'

const DAY_MS = 24 * 60 * 60 * 1000
const ago = (days) => new Date(Date.now() - days * DAY_MS).toISOString()

const USER = {
  id: 'u1',
  email: 'ann@example.com',
  name: 'Ann',
  picture: 'https://example.com/a.png',
  provider: 'github',
  credits: 940,
  creditsLimit: 1000,
  monthlyGrant: 250,
  creditsRenewAt: ago(-3),
  createdAt: '2026-10-01T00:00:00Z',
  used: 60,
  folders: 2,
}

const mountDetail = () => mount(AdminUserDetail, { props: { id: 'u1' } })

describe('AdminUserDetail', () => {
  beforeEach(() => {
    vi.clearAllMocks()
  })

  it('shows the user, their usage and their credit history', async () => {
    fetchAdminUser.mockResolvedValue(USER)
    listAdminUserCreditActivities
      .mockResolvedValueOnce({ activities: [{ id: 'c', ops: 'spend', change: -60, current: 940, agent: 'Claude Code', createdAt: ago(0) }], next: 'c' })
      .mockResolvedValueOnce({ activities: [{ id: 'b', ops: 'grant', change: 1000, current: 1000, createdAt: ago(40) }], next: 'b' })
      .mockResolvedValueOnce({ activities: [{ id: 'a', ops: 'admin_grant', change: 5, current: 5, createdAt: ago(50) }], next: '' })
    const wrapper = mountDetail()
    expect(wrapper.find('[data-test="admin-user-loading"]').exists()).toBe(true)
    await flushPromises()

    expect(fetchAdminUser).toHaveBeenCalledWith('u1')
    expect(listAdminUserCreditActivities.mock.calls).toEqual([['u1', ''], ['u1', 'c']])
    expect(wrapper.find('[data-test="admin-user-name"]').text()).toBe('Ann')
    expect(wrapper.find('[data-test="admin-user-id"]').text()).toBe('u1')
    expect(wrapper.text()).toContain('github')
    expect(wrapper.find('img').attributes('src')).toBe(USER.picture)
    expect(wrapper.find('[data-test="admin-user-balance"]').text()).toBe('940')
    expect(wrapper.find('[data-test="admin-user-used"]').text()).toBe('60')
    expect(wrapper.find('[data-test="admin-user-monthly"]').text()).toBe('250')
    expect(wrapper.find('[data-test="admin-user-renewal"]').text()).toBe('Renews in 3 days')
    expect(wrapper.find('[data-test="admin-user-folders"]').text()).toBe('2')
    expect(wrapper.findAll('[data-test="credits-row"]')).toHaveLength(2)
    expect(wrapper.find('[data-test="credits-agent"]').text()).toContain('Claude Code')

    await wrapper.find('[data-test="credits-older"]').trigger('click')
    await flushPromises()
    expect(wrapper.findAll('[data-test="credits-row"]')).toHaveLength(3)
    expect(wrapper.text()).toContain('Admin grant')

    await wrapper.find('[data-test="admin-user-grant"]').trigger('click')
    await wrapper.find('[data-test="admin-user-back"]').trigger('click')
    expect(wrapper.emitted('grant')).toEqual([['ann@example.com']])
    expect(wrapper.emitted('back')).toHaveLength(1)
  })

  it('falls back to the email without a name or picture', async () => {
    fetchAdminUser.mockResolvedValue({ ...USER, name: '', picture: '', provider: '' })
    listAdminUserCreditActivities.mockResolvedValue({ activities: [], next: '' })
    const wrapper = mountDetail()
    await flushPromises()
    expect(wrapper.find('[data-test="admin-user-name"]').text()).toBe('ann@example.com')
    expect(wrapper.find('img').exists()).toBe(false)
    expect(wrapper.find('[data-test="credits-empty"]').exists()).toBe(true)
  })

  it('says when no user has the ID', async () => {
    fetchAdminUser.mockRejectedValue(Object.assign(new Error('user not found'), { status: 404 }))
    const wrapper = mountDetail()
    await flushPromises()
    expect(wrapper.find('[data-test="admin-user-missing"]').text()).toContain('u1')
    expect(listAdminUserCreditActivities).not.toHaveBeenCalled()
    expect(wrapper.emitted('error')).toBeUndefined()
  })

  it('reports other failures', async () => {
    const err = new Error('Failed to load user (500)')
    fetchAdminUser.mockRejectedValue(err)
    const wrapper = mountDetail()
    await flushPromises()
    expect(wrapper.find('[data-test="admin-user-failed"]').exists()).toBe(true)
    expect(wrapper.emitted('error')).toEqual([[err]])
  })

  it('reports a failed history load', async () => {
    const err = new Error('Failed to load credit history (500)')
    fetchAdminUser.mockResolvedValue(USER)
    listAdminUserCreditActivities.mockRejectedValue(err)
    const wrapper = mountDetail()
    await flushPromises()
    expect(wrapper.emitted('error')).toEqual([[err]])
    expect(wrapper.find('[data-test="admin-user-name"]').exists()).toBe(true)
  })
})
