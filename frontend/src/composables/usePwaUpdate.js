// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { ref } from 'vue'
import { Workbox } from 'workbox-window'
import { PRECACHE_PROGRESS, percentOf } from '../utils/precacheProgress'

/** How often an open page asks the server for a new version. */
export const UPDATE_CHECK_INTERVAL = 60 * 60 * 1000

/** How long "Update now" waits for the new version before reloading anyway. */
export const RESTART_TIMEOUT = 10 * 1000

// One registration per page, so the state is module-wide: main.js registers,
// the banner in App.vue reads.
const needRefresh = ref(false)
// { phase: 'downloading' | 'restarting', percent: number | null } or null.
const progress = ref(null)
let workbox = null
let reload = null

/**
 * The update banner's view of the service worker.
 *
 * Nothing is ever offered when no worker was registered (development, tests),
 * so the banner simply never shows there.
 */
export function usePwaUpdate() {
  return { needRefresh, progress, update, dismiss }
}

/** Hides the "new version" offer; the next page load offers it again. */
function dismiss() {
  needRefresh.value = false
}

/**
 * Switches to the waiting version. The page reloads once it takes over (see
 * the `controlling` listener); the timeout covers a worker that is gone.
 */
function update() {
  if (!workbox) return
  needRefresh.value = false
  progress.value = { phase: 'restarting', percent: null }
  workbox.messageSkipWaiting()
  setTimeout(reload, RESTART_TIMEOUT)
}

/**
 * Follows the progress messages a new worker posts while it downloads.
 *
 * A first visit installs too, and that is not an update to announce. The last
 * file's message can land after the install has ended, and would put a
 * finished bar back up for good, so only an installing worker counts.
 */
export function onProgressMessage(container) {
  let watched = null
  return (event) => {
    if (event.data?.type !== PRECACHE_PROGRESS) return
    if (!container.controller) return
    const worker = event.source
    if (worker && worker.state !== 'installing') return

    progress.value = { phase: 'downloading', percent: percentOf(event.data) }

    // The worker leaves 'installing' whether it succeeded or failed: either
    // way the bar is done, and on success the "Update now" offer takes over.
    if (worker && worker !== watched) {
      watched = worker
      worker.addEventListener('statechange', () => {
        if (worker.state !== 'installing' && progress.value?.phase === 'downloading') {
          progress.value = null
        }
      })
    }
  }
}

/**
 * Registers the service worker at `<basePath>/sw.js` and keeps it current.
 *
 * The URL is absolute under the base path: a relative `sw.js` resolves
 * against the page, so a first visit to a deep link (/files/...) would ask
 * for /files/sw.js and never register.
 *
 * Browsers only look for a new worker on navigation, and a tab can stay open
 * for days, so this also checks every hour and whenever the tab comes back
 * into view. A new version downloads in the background (the progress bar),
 * then waits for "Update now".
 */
export function registerServiceWorker({
  basePath = '',
  container = globalThis.navigator?.serviceWorker,
  createWorkbox = (url, options) => new Workbox(url, options),
  enabled = import.meta.env.PROD,
  interval = UPDATE_CHECK_INTERVAL,
  reloadPage = () => window.location.reload(),
} = {}) {
  if (!enabled || !container) return null

  const wb = createWorkbox(`${basePath}/sw.js`, { scope: `${basePath}/` })
  workbox = wb
  reload = reloadPage

  wb.addEventListener('waiting', () => {
    progress.value = null
    needRefresh.value = true
  })
  // A new version took over: this tab's or another tab's "Update now". The
  // old version's lazy-loaded files are gone from the cache and the server
  // by then, so the page must reload rather than keep running. isUpdate is
  // false on a first install, which never reloads.
  wb.addEventListener('controlling', (event) => {
    if (event.isUpdate) reloadPage()
  })
  container.addEventListener('message', onProgressMessage(container))

  const check = () => {
    if (navigator.onLine === false) return
    wb.update().catch(() => {})
  }

  wb.register()
    .then(() => {
      setInterval(check, interval)
      document.addEventListener('visibilitychange', () => {
        if (document.visibilityState === 'visible') check()
      })
    })
    // No worker (blocked, private mode, a failed fetch): the app still works.
    .catch(() => {})

  return wb
}

/** Test seam: forgets the registration and resets the shared state. */
export function resetPwaUpdate() {
  workbox = null
  reload = null
  needRefresh.value = false
  progress.value = null
}
