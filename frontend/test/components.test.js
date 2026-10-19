// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import BrandLogo from '../src/components/BrandLogo.vue'
import ToastStack from '../src/components/ToastStack.vue'
import UploadProgress from '../src/components/UploadProgress.vue'
import { useToasts } from '../src/composables/useToasts'

describe('BrandLogo', () => {
  it('renders plain and tile variants', () => {
    const plain = mount(BrandLogo)
    expect(plain.classes()).toContain('text-gray-900')
    expect(plain.findAll('path')).toHaveLength(6)
    const tile = mount(BrandLogo, { props: { tile: true } })
    expect(tile.classes()).toContain('bg-black')
    expect(tile.find('svg').classes()).toContain('w-3/4')
  })
})

describe('ToastStack', () => {
  afterEach(() => {
    useToasts().toasts.value = []
  })

  it('renders toasts and dismisses them', async () => {
    const { toasts } = useToasts()
    toasts.value = [
      { id: 1, type: 'error', title: 'Oops', message: 'bad' },
      { id: 2, type: 'mystery', title: null, message: 'plain' },
    ]
    const wrapper = mount(ToastStack)
    await wrapper.vm.$nextTick()
    expect(wrapper.find('[data-test="toast-error"]').text()).toContain('Oops')
    expect(wrapper.find('[data-test="toast-mystery"] span').classes()).toContain('bg-gray-400')
    await wrapper.find('[data-test="toast-error"] button').trigger('click')
    expect(toasts.value.map((t) => t.id)).toEqual([2])
  })
})

describe('UploadProgress', () => {
  it('fills the bar by the bytes sent', () => {
    const half = mount(UploadProgress, { props: { sent: 512, total: 1024 } })
    expect(half.find('[data-test="upload-progress-bytes"]').text()).toBe('512 B of 1.0 KB')
    expect(half.find('[role="progressbar"]').attributes('aria-valuenow')).toBe('50')
    expect(half.find('[data-test="upload-progress-bar"]').attributes('style')).toContain('width: 50%')
    // A progress event can count a few header bytes past the body.
    const over = mount(UploadProgress, { props: { sent: 2000, total: 1024 } })
    expect(over.find('[data-test="upload-progress-percent"]').text()).toBe('100%')
    expect(over.find('[data-test="upload-progress-bytes"]').text()).toBe('1.0 KB of 1.0 KB')
    const empty = mount(UploadProgress)
    expect(empty.find('[data-test="upload-progress-percent"]').text()).toBe('0%')
  })
})
