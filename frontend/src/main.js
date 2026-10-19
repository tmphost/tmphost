// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { createWebHistory } from 'vue-router'
import { createtmphostApp } from './app'
import { registerServiceWorker } from './composables/usePwaUpdate'

const basePath = window.__TMPHOST_BASE_PATH__ || ''

const { app } = createtmphostApp({
  history: createWebHistory(basePath || '/'),
  platform: 'web',
})
app.mount('#app')

// Browser only: installs the PWA and downloads new versions in the background.
registerServiceWorker({ basePath })
