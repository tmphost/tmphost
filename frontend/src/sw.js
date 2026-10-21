// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// The service worker: it keeps the built app shell in the cache, so the app
// installs as a PWA and loads offline, and it downloads each new version in
// the background before the page offers to switch to it.
//
// It routes nothing but the precached build files. Every other request
// (/api, /admin/v1, /auth sign-in redirects, /oauth2, /mcp, uploads) goes
// straight to the network: caching or answering those with the app shell
// breaks sign-in and serves stale data.

import { addPlugins, cleanupOutdatedCaches, precacheAndRoute } from 'workbox-precaching'
import { SKIP_WAITING, precacheProgressPlugin, precacheTotal } from './utils/precacheProgress'

// No skipWaiting on install: a new version waits until the page asks for it,
// since swapping files under a running page breaks its lazy-loaded routes.
self.addEventListener('message', (event) => {
  if (event.data?.type === SKIP_WAITING) self.skipWaiting()
})

// Named once: the build injects the file list at its only mention.
const manifest = self.__WB_MANIFEST

// Tells open pages how far the download of a new version has got. Only an
// update: a first install has no page running an older version to tell.
addPlugins([
  precacheProgressPlugin({
    total: precacheTotal(manifest),
    report: async (message) => {
      if (!self.registration.active) return
      const pages = await self.clients.matchAll({ type: 'window', includeUncontrolled: true })
      for (const page of pages) page.postMessage(message)
    },
  }),
])
precacheAndRoute(manifest)
cleanupOutdatedCaches()
