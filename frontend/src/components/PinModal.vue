<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed, ref, watch } from 'vue'
import { createFolderPin } from '../api'
import { useClipboard } from '../composables/useClipboard'
import { useToasts } from '../composables/useToasts'

// PinModal gives a folder a new PIN of its own and shows it the one time
// it can be seen. The parent opens it by setting folder, takes the updated
// folder from changed, and closes it on close.
const props = defineProps({
  folder: { type: Object, default: null },
  // The name the folder is shown under.
  title: { type: String, default: '' },
})
const emit = defineEmits(['changed', 'close'])

const { notifyError } = useToasts()
const { copy } = useClipboard()

const busy = ref(false)
// The new PIN; empty until it is made.
const pin = ref('')

const replaces = computed(() => props.folder?.pinMode === 'folder')
// pinUrl opens the folder without the PIN page.
const pinUrl = computed(() => {
  const url = props.folder?.openUrl
  if (!url || !pin.value) return ''
  return `${url}${url.includes('?') ? '&' : '?'}pin=${pin.value}`
})

watch(
  () => props.folder?.id,
  () => {
    busy.value = false
    pin.value = ''
  },
)

function close() {
  if (!busy.value) emit('close')
}

async function confirm() {
  busy.value = true
  try {
    const folder = await createFolderPin(props.folder.id)
    pin.value = folder.pin
    emit('changed', folder)
  } catch (err) {
    notifyError(err.message)
  } finally {
    busy.value = false
  }
}
</script>

<template>
  <div
    v-if="folder"
    class="fixed inset-0 z-[150] flex items-center justify-center bg-black/40 backdrop-blur-sm px-4"
    data-test="pin-modal"
    @click.self="close"
    @keydown.esc="close"
  >
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="pin-modal-title"
      class="w-full max-w-sm bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-sm p-5"
    >
      <h2 id="pin-modal-title" class="text-sm font-bold text-gray-900 dark:text-zinc-100 truncate">
        {{ pin ? 'PIN for' : replaces ? 'New PIN for' : 'Add a PIN to' }} {{ title }}
      </h2>

      <template v-if="pin">
        <p class="mt-1.5 text-xs text-gray-500 dark:text-zinc-400">
          Share it with the people who should see the folder. It is shown only now.
        </p>
        <p
          class="mt-4 rounded-lg bg-gray-50 dark:bg-zinc-800 py-3 text-center font-mono text-2xl font-bold tracking-[0.4em] text-gray-900 dark:text-zinc-100"
          data-test="pin-value"
        >{{ pin }}</p>
        <div class="mt-5 flex flex-wrap justify-end gap-2">
          <button
            v-if="pinUrl"
            type="button"
            class="rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-600 transition-colors"
            title="Copy a link that opens the folder without asking for the PIN"
            data-test="pin-copy-link"
            @click="copy(pinUrl, 'Link with PIN copied')"
          >Copy link</button>
          <button
            type="button"
            class="rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-600 transition-colors"
            data-test="pin-copy"
            @click="copy(pin, 'PIN copied')"
          >Copy PIN</button>
          <button
            type="button"
            class="rounded-lg bg-black dark:bg-white px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-white dark:text-zinc-900 hover:bg-gray-800 dark:hover:bg-zinc-200 transition-colors"
            data-test="pin-done"
            @click="close"
          >Done</button>
        </div>
      </template>

      <template v-else>
        <p class="mt-1.5 text-xs text-gray-500 dark:text-zinc-400" data-test="pin-message">
          <template v-if="replaces">The folder gets a new 6 digit PIN. The current one stops working, and visitors have to enter the new one.</template>
          <template v-else>Visitors have to enter a 6 digit PIN before they see the folder's files.</template>
        </p>
        <div class="mt-5 flex justify-end gap-2">
          <button
            type="button"
            :disabled="busy"
            class="rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-600 disabled:opacity-50 transition-colors"
            data-test="pin-cancel"
            @click="close"
          >Cancel</button>
          <button
            type="button"
            :disabled="busy"
            class="rounded-lg bg-black dark:bg-white px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-white dark:text-zinc-900 hover:bg-gray-800 dark:hover:bg-zinc-200 disabled:opacity-50 transition-colors"
            data-test="pin-confirm"
            @click="confirm"
          >{{ busy ? 'Making…' : replaces ? 'New PIN' : 'Add PIN' }}</button>
        </div>
      </template>
    </div>
  </div>
</template>
