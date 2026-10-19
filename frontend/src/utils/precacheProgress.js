// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// Shared by the service worker and the page, so it imports nothing: whatever
// this file pulls in ends up inside sw.js.

/** The message a new service worker posts to open pages as it caches itself. */
export const PRECACHE_PROGRESS = 'PRECACHE_PROGRESS'

/** The message that tells a waiting service worker to take over. */
export const SKIP_WAITING = 'SKIP_WAITING'

/**
 * A precache plugin that reports each file of a new version as it is cached.
 *
 * Workbox caches the manifest one entry at a time during install, so finished
 * entries over the manifest's length is the download's progress. A file
 * count, not bytes: the sizes are not known until each file arrives.
 * `handlerDidComplete` also fires for ordinary fetches, hence the install check.
 */
export function precacheProgressPlugin({ total, report }) {
  let done = 0
  return {
    handlerDidComplete: async ({ event }) => {
      if (event?.type !== 'install') return
      done += 1
      await report({ type: PRECACHE_PROGRESS, done, total })
    },
  }
}

/**
 * How many files an install caches: one per URL. The build can list a file
 * twice, and Workbox caches it once, so counting entries would stop the bar
 * short of 100.
 */
export function precacheTotal(manifest) {
  return new Set(manifest.map((entry) => (typeof entry === 'string' ? entry : entry.url))).size
}

/** A 0-100 percentage from a progress message, or null when there is no total. */
export function percentOf({ done, total }) {
  if (!total) return null
  return Math.min(100, (done / total) * 100)
}
