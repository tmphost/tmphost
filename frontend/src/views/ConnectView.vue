<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { BASE_PATH, fetchMcpUrl } from '../api'
import { useClipboard } from '../composables/useClipboard'

const INSTALL_BASE = 'https://raw.githubusercontent.com/tmphost/tmphost/main'

// How long the check mark replaces the copy icon.
const COPIED_MS = 1500

const ICON_BUTTON =
  'absolute right-1.5 inline-flex items-center justify-center size-7 rounded-lg text-gray-400 dark:text-zinc-500 hover:text-gray-900 dark:hover:text-zinc-100 hover:bg-gray-100 dark:hover:bg-zinc-800 transition-colors'
const ICONS = {
  copy: 'M8 8V5a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1h-3M5 8h10a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V9a1 1 0 0 1 1-1z',
  check: 'm5 13 4 4L19 7',
}

const { copy } = useClipboard()

// The snippet whose copy button shows the check mark.
const copiedId = ref(null)
let copiedTimer = null

async function copySnippet(snippet) {
  if (!(await copy(snippet.text, `${snippet.label} copied`))) return
  clearTimeout(copiedTimer)
  copiedId.value = snippet.id
  copiedTimer = setTimeout(() => {
    copiedId.value = null
  }, COPIED_MS)
}

onBeforeUnmount(() => clearTimeout(copiedTimer))

const serverUrl = `${window.location.origin}${BASE_PATH}`
// The MCP endpoint is below the app URL unless the server names an MCP domain.
const mcpUrl = ref(`${serverUrl}/mcp`)

onMounted(async () => {
  try {
    mcpUrl.value = (await fetchMcpUrl()) || mcpUrl.value
  } catch {
    // Keep the app URL's endpoint, which always works.
  }
})

const mcpSnippets = (url) => [
  { id: 'mcp-url', label: 'MCP server URL', text: url },
  {
    id: 'mcp-json',
    label: 'JSON config',
    text: JSON.stringify({ mcpServers: { tmphost: { type: 'http', url } } }, null, 2),
  },
  { id: 'mcp-claude', label: 'Claude Code', text: `claude mcp add --transport http tmphost ${url}` },
]

const CLI_SNIPPETS = [
  { id: 'cli-install-unix', label: 'Install on macOS / Linux', text: `curl -fsSL ${INSTALL_BASE}/install.sh | sh` },
  { id: 'cli-install-windows', label: 'Install on Windows (PowerShell)', text: `irm ${INSTALL_BASE}/install.ps1 | iex` },
  { id: 'cli-login', label: 'Sign in (opens the browser)', text: `tmphost login --server ${serverUrl}` },
  {
    id: 'cli-login-device',
    label: 'Sign in without a browser (SSH, containers)',
    text: `tmphost login --device --server ${serverUrl}`,
  },
  {
    id: 'cli-examples',
    label: 'Examples',
    text: ['tmphost file upload ./index.html --ttl 1h', 'tmphost file get --id <id>', 'tmphost file url --id <id>'].join('\n'),
  },
]

const SECTIONS = computed(() => [
  {
    id: 'mcp',
    label: 'MCP',
    title: 'MCP clients',
    // The official MCP logo (modelcontextprotocol/docs/logo), scaled from its 195 viewBox to 24.
    icon: 'M3.08 12.04L11.43 3.69C12.59 2.54 14.46 2.54 15.61 3.69V3.69C16.76 4.84 16.76 6.71 15.61 7.87L9.3 14.18M9.39 14.09L15.61 7.87C16.76 6.71 18.63 6.71 19.79 7.87L19.83 7.91C20.98 9.06 20.98 10.93 19.83 12.09L12.27 19.64C11.89 20.03 11.89 20.65 12.27 21.04L13.83 22.59M13.52 5.78L7.34 11.96C6.19 13.11 6.19 14.98 7.34 16.13V16.13C8.49 17.29 10.37 17.29 11.52 16.13L17.7 9.95',
    intro: 'Add tmphost to Claude, Cursor or any MCP client that speaks streamable HTTP.',
    note: 'The client opens your browser to sign in and shows a consent screen. No secret or key is needed.',
    snippets: mcpSnippets(mcpUrl.value),
  },
  {
    id: 'cli',
    label: 'CLI',
    title: 'CLI',
    // A terminal prompt.
    icon: 'm5 8 4 4-4 4m6 0h8',
    intro: 'Install the tmphost command line tool, then sign in once.',
    note: 'Login opens your browser to sign in; the CLI keeps the session on this machine.',
    snippets: CLI_SNIPPETS,
  },
])

// The open tab lives in the route (?tab=cli) so a link can open it; MCP is the default.
const route = useRoute()
const active = computed(() => SECTIONS.value.find((section) => section.id === route.query.tab) || SECTIONS.value[0])
</script>

<template>
  <div class="flex flex-col">
    <header class="px-4 pb-4 border-b border-gray-100 dark:border-zinc-800">
      <h1 class="text-lg font-bold text-gray-900 dark:text-zinc-100 tracking-tight">Connect</h1>
      <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">
        Use tmphost from an MCP client or the <code class="font-mono">tmphost</code> CLI. Both sign in with your account
        over OAuth.
      </p>
    </header>

    <div class="px-4 pt-4 flex flex-col md:flex-row gap-4 md:gap-8 min-w-0">
      <!-- Menu: a row above the content on phones, a column beside it from md up. -->
      <nav class="w-full md:w-48 shrink-0" aria-label="Connect with">
        <ul class="flex md:flex-col gap-1">
          <li v-for="section in SECTIONS" :key="section.id" class="flex-1 md:flex-none">
            <router-link
              :to="{ query: { ...route.query, tab: section.id } }"
              replace
              :aria-current="active.id === section.id ? 'page' : undefined"
              :class="[
                'flex items-center justify-center md:justify-start gap-2.5 px-3 py-2 rounded-lg text-xs transition-colors',
                active.id === section.id
                  ? 'bg-gray-200/70 dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 font-semibold'
                  : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-100 dark:hover:bg-zinc-800 hover:text-gray-900 dark:hover:text-zinc-100',
              ]"
              :data-test="`connect-tab-${section.id}`"
            >
              <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
                <path stroke-linecap="round" stroke-linejoin="round" :d="section.icon" />
              </svg>
              {{ section.label }}
            </router-link>
          </li>
        </ul>
      </nav>

      <section
        :key="active.id"
        class="flex-1 min-w-0"
        :aria-labelledby="`connect-${active.id}-title`"
        :data-test="`connect-${active.id}`"
      >
        <h2 :id="`connect-${active.id}-title`" class="text-sm font-bold text-gray-900 dark:text-zinc-100">{{ active.title }}</h2>
        <p class="mt-1 text-xs text-gray-500 dark:text-zinc-400">{{ active.intro }}</p>
        <p
          v-if="active.id === 'mcp'"
          class="mt-1 text-xs text-gray-500 dark:text-zinc-400"
          data-test="connect-mcp-tool"
        >
          It exposes one tool, <code class="font-mono text-gray-900 dark:text-zinc-100">file</code>, with the actions
          <code class="font-mono text-gray-900 dark:text-zinc-100">upload</code>, <code class="font-mono text-gray-900 dark:text-zinc-100">download</code>,
          <code class="font-mono text-gray-900 dark:text-zinc-100">get</code> and <code class="font-mono text-gray-900 dark:text-zinc-100">url</code>.
        </p>

        <div v-for="snippet in active.snippets" :key="snippet.id" class="mt-4">
          <p class="text-[11px] font-bold text-gray-500 dark:text-zinc-400 mb-1">{{ snippet.label }}</p>
          <div class="relative">
            <pre
              class="rounded-lg border border-gray-100 dark:border-zinc-800 bg-zinc-50 dark:bg-zinc-950 py-3 pl-3 pr-11 text-xs font-mono text-gray-900 dark:text-zinc-100 overflow-x-auto"
              :data-test="`snippet-${snippet.id}`"
            >{{ snippet.text }}</pre>
            <button
              type="button"
              :class="[ICON_BUTTON, snippet.text.includes('\n') ? 'top-1.5' : 'top-1/2 -translate-y-1/2']"
              :aria-label="`Copy ${snippet.label}`"
              :title="copiedId === snippet.id ? 'Copied' : `Copy ${snippet.label}`"
              :data-test="`copy-${snippet.id}`"
              @click="copySnippet(snippet)"
            >
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
                <path stroke-linecap="round" stroke-linejoin="round" :d="copiedId === snippet.id ? ICONS.check : ICONS.copy" />
              </svg>
            </button>
          </div>
        </div>

        <p class="mt-4 text-[11px] text-gray-400 dark:text-zinc-500" :data-test="`connect-${active.id}-note`">{{ active.note }}</p>
      </section>
    </div>
  </div>
</template>
