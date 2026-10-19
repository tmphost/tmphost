// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { defineStore } from 'pinia'

// One tooltip for the whole app, rendered in App.vue, so it is never clipped
// by the sidebar's overflow. Copied from agentrq.
export const useTooltipStore = defineStore('tooltip', {
  state: () => ({
    visible: false,
    text: '',
    style: { top: '0px', left: '0px' },
  }),
  actions: {
    show(event, text, position = 'right') {
      const rect = event.currentTarget.getBoundingClientRect()

      let top
      let left
      if (position === 'bottom') {
        top = rect.bottom + 8
        left = rect.left + rect.width / 2
      } else if (position === 'top') {
        top = rect.top - 8
        left = rect.left + rect.width / 2
      } else {
        top = rect.top + rect.height / 2
        left = rect.right + 12
      }

      this.visible = true
      this.text = text
      this.style = {
        top: `${top}px`,
        left: `${left}px`,
        transform: position === 'top' ? 'translate(-50%, -100%)' : position === 'bottom' ? 'translateX(-50%)' : 'translateY(-50%)',
      }
    },
    hide() {
      this.visible = false
    },
  },
})
