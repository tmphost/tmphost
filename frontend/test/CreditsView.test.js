// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it, vi } from 'vitest'
import { flushPromises, mount } from '@vue/test-utils'
import { ref } from 'vue'

vi.mock('../src/api', () => ({ listCreditActivities: vi.fn() }))

import CreditsView from '../src/views/CreditsView.vue'
import { listCreditActivities } from '../src/api'
import { useToasts } from '../src/composables/useToasts'

const DAY_MS = 24 * 60 * 60 * 1000
const ago = (days) => new Date(Date.now() - days * DAY_MS).toISOString()
const lastToast = () => useToasts().toasts.value.at(-1)

function mountView(user = { credits: 990, creditsLimit: 1000, creditsRenewAt: ago(-3) }) {
  return mount(CreditsView, { global: { provide: { currentUser: { user: ref(user) } } } })
}

describe('CreditsView', () => {
  beforeEach(() => {
    useToasts().toasts.value = []
  })

  it('loads pages until the chart range is covered, then older ones on demand', async () => {
    listCreditActivities
      .mockResolvedValueOnce({ activities: [{ id: 'c', ops: 'spend', change: -10, current: 990, createdAt: ago(0) }], next: 'c' })
      .mockResolvedValueOnce({ activities: [{ id: 'b', ops: 'grant', change: 1000, current: 1000, createdAt: ago(40) }], next: 'b' })
      .mockResolvedValueOnce({ activities: [{ id: 'a', ops: 'mystery', change: 0, current: 0, createdAt: ago(50) }], next: '' })
    const wrapper = mountView()
    expect(wrapper.text()).toContain('Loading...')
    await flushPromises()

    expect(listCreditActivities.mock.calls).toEqual([[''], ['c']])
    expect(wrapper.find('[data-test="credits-balance"]').text()).toBe('990')
    expect(wrapper.find('[data-test="credits-used"]').text()).toBe('10')
    expect(wrapper.find('[data-test="credits-renewal"]').text()).toBe('Renews in 3 days')
    expect(wrapper.findAll('[data-test="chart-line"]').map((l) => l.attributes('stroke'))).toEqual(['#aec477', '#a8a3d9'])

    const rows = () => wrapper.findAll('[data-test="credits-row"]')
    expect(rows().map((r) => r.find('p').text())).toEqual(['Upload', 'Sign-up grant'])
    expect(rows().map((r) => r.find('[data-test="credits-change"]').text())).toEqual(['-10', '+1000'])
    expect(rows()[1].find('[data-test="credits-change"]').classes()).toContain('text-emerald-600')
    expect(rows().map((r) => r.find('[data-test="credits-current"]').text())).toEqual(['990', '1000'])

    await wrapper.find('[data-test="credits-older"]').trigger('click')
    await flushPromises()
    expect(listCreditActivities).toHaveBeenLastCalledWith('b')
    expect(rows().at(-1).find('p').text()).toBe('mystery')
    expect(wrapper.find('[data-test="credits-older"]').exists()).toBe(false)
  })

  it('shows an empty history without a user', async () => {
    listCreditActivities.mockResolvedValueOnce({ activities: [], next: '' })
    const wrapper = mount(CreditsView)
    await flushPromises()
    expect(wrapper.find('[data-test="credits-empty"]').exists()).toBe(true)
    expect(wrapper.find('[data-test="credits-balance"]').text()).toBe('0')
    expect(wrapper.find('[data-test="credits-renewal"]').text()).toBe('')
  })

  it('reports load errors', async () => {
    listCreditActivities.mockRejectedValueOnce(new Error('Failed to load credit history (500)'))
    const wrapper = mountView()
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'Failed to load credit history (500)' })
    expect(wrapper.text()).not.toContain('Loading...')

    listCreditActivities.mockResolvedValueOnce({ activities: [{ id: 'x', ops: 'refund', change: 2, current: 5, createdAt: ago(0) }], next: 'x' })
    listCreditActivities.mockResolvedValueOnce({ activities: [{ id: 'w', ops: 'renewal', change: 1, current: 3, createdAt: ago(60) }], next: 'w' })
    const again = mountView()
    await flushPromises()
    listCreditActivities.mockRejectedValueOnce(new Error('offline'))
    await again.find('[data-test="credits-older"]').trigger('click')
    await flushPromises()
    expect(lastToast()).toMatchObject({ type: 'error', message: 'offline' })
    expect(again.find('[data-test="credits-older"]').attributes('disabled')).toBeUndefined()
  })
})
