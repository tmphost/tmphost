// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

import { defineConfig } from 'vite'
import vue from '@vitejs/plugin-vue'
import tailwindcss from '@tailwindcss/vite'
import { VitePWA } from 'vite-plugin-pwa'

// The Go server proxies nothing in development; Vite forwards the API and the
// sign-in routes to it.
// Point TMPHOSTIO_BASE_URL at this dev server so OAuth callbacks land here and
// the session cookie is set on the same origin as the app.
const apiTarget = process.env.TMPHOSTIO_DEV_API_TARGET || 'http://localhost:2027'

export default defineConfig({
  // Relative asset URLs; the Go server rewrites them under its base path.
  base: '',
  plugins: [
    vue(),
    tailwindcss(),
    VitePWA({
      // Our own src/sw.js, with only the precache list injected.
      strategies: 'injectManifest',
      srcDir: 'src',
      filename: 'sw.js',
      // src/main.js registers it under the injected base path, and
      // public/manifest.webmanifest is hand-written with relative URLs: the
      // plugin's own registration and manifest would use paths that break
      // under a base path or on a deep link.
      injectRegister: false,
      manifest: false,
      injectManifest: {
        globPatterns: ['**/*.{js,css,html,ico,png,svg,woff2,webmanifest}'],
      },
    }),
  ],
  server: {
    proxy: {
      '/api': { target: apiTarget },
      '/auth': { target: apiTarget },
    },
  },
})
