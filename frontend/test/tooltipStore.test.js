// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useTooltipStore } from '../src/stores/tooltipStore'

const event = {
  currentTarget: { getBoundingClientRect: () => ({ top: 100, bottom: 132, left: 10, right: 50, width: 40, height: 32 }) },
}

describe('tooltipStore', () => {
  beforeEach(() => setActivePinia(createPinia()))

  it('places the tooltip right of the target by default', () => {
    const tooltip = useTooltipStore()
    expect(tooltip.visible).toBe(false)
    tooltip.show(event, 'Files')
    expect(tooltip.visible).toBe(true)
    expect(tooltip.text).toBe('Files')
    expect(tooltip.style).toEqual({ top: '116px', left: '62px', transform: 'translateY(-50%)' })
    tooltip.hide()
    expect(tooltip.visible).toBe(false)
  })

  it('places the tooltip below or above the target', () => {
    const tooltip = useTooltipStore()
    tooltip.show(event, 'Below', 'bottom')
    expect(tooltip.style).toEqual({ top: '140px', left: '30px', transform: 'translateX(-50%)' })
    tooltip.show(event, 'Above', 'top')
    expect(tooltip.style).toEqual({ top: '92px', left: '30px', transform: 'translate(-50%, -100%)' })
  })
})
