<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed, ref, watch } from 'vue'
import { extendFolder, fetchFolder } from '../api'
import { useToasts } from '../composables/useToasts'
import { expiresIn, extendOptions, formatMinutes, uploadCost } from '../utils/files'

// ExtendModal makes a folder expire later, for credits: each file costs
// what an upload would for the added time. The parent opens it by setting
// folder and closes it on extended or cancel.
const props = defineProps({
  folder: { type: Object, default: null },
  // The name the folder is shown under.
  title: { type: String, default: '' },
  limits: { type: Object, default: null },
  credits: { type: Number, default: undefined },
  now: { type: Number, required: true },
})
const emit = defineEmits(['extended', 'cancel'])

const MINUTE_MS = 60 * 1000
const { notifyError } = useToasts()

const minutes = ref(0)
// The folder's file sizes; null until they are loaded.
const sizes = ref(null)
const busy = ref(false)

const remaining = computed(() => Math.max(0, Math.ceil((Date.parse(props.folder.expiresAt) - props.now) / MINUTE_MS)))
const choices = computed(() => (props.limits ? extendOptions(remaining.value, props.limits.maxTtlMinutes) : []))
// The slider steps through choices by index; minutes keeps the value.
const index = computed(() => Math.max(0, choices.value.indexOf(minutes.value)))
const cost = computed(() =>
  sizes.value && props.limits
    ? uploadCost(sizes.value, minutes.value, props.limits.billingBlockMinutes, props.limits.billingUnitBytes)
    : 0,
)
const tooCostly = computed(() => props.credits !== undefined && cost.value > props.credits)
const newExpiry = computed(() => new Date(Date.parse(props.folder.expiresAt) + minutes.value * MINUTE_MS).toISOString())

watch(
  () => props.folder?.id,
  async (id) => {
    if (!id) return
    minutes.value = choices.value[0]
    sizes.value = null
    busy.value = false
    try {
      const { files } = await fetchFolder(id)
      // Another folder may have been opened meanwhile.
      if (props.folder?.id === id) sizes.value = files.map((f) => f.size)
    } catch (err) {
      notifyError(err.message)
      if (props.folder?.id === id) emit('cancel')
    }
  },
  { immediate: true },
)

function cancel() {
  if (!busy.value) emit('cancel')
}

async function confirm() {
  busy.value = true
  try {
    const { folder, creditsSpent } = await extendFolder(props.folder.id, minutes.value)
    emit('extended', { folder, creditsSpent, minutes: minutes.value })
  } catch (err) {
    busy.value = false
    notifyError(err.message)
  }
}
</script>

<template>
  <div
    v-if="folder"
    class="fixed inset-0 z-[150] flex items-center justify-center bg-black/40 backdrop-blur-sm px-4"
    data-test="extend-modal"
    @click.self="cancel"
    @keydown.esc="cancel"
  >
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="extend-modal-title"
      class="w-full max-w-sm bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-sm p-5"
    >
      <h2 id="extend-modal-title" class="text-sm font-bold text-gray-900 dark:text-zinc-100 truncate">Extend {{ title }}</h2>
      <p class="mt-1.5 text-xs text-gray-500 dark:text-zinc-400" data-test="extend-now">
        Expires in {{ expiresIn(folder.expiresAt, now) }}. Each file costs what an upload would for the added time.
      </p>

      <p v-if="!choices.length" class="mt-4 text-xs font-medium text-gray-900 dark:text-zinc-100" data-test="extend-maxed">
        This folder already lives as long as it can.
      </p>
      <template v-else>
        <div class="mt-4">
          <div class="flex items-baseline justify-between mb-2">
            <label for="extend-minutes" class="text-[11px] font-bold text-gray-500 dark:text-zinc-400">Extend by</label>
            <span class="text-xs font-bold tabular-nums text-gray-900 dark:text-zinc-100" data-test="extend-value">
              {{ formatMinutes(minutes) }}
            </span>
          </div>
          <input
            id="extend-minutes"
            type="range"
            min="0"
            :max="choices.length - 1"
            step="1"
            :value="index"
            :disabled="busy"
            :aria-valuetext="formatMinutes(minutes)"
            class="w-full cursor-pointer accent-black dark:accent-white focus:outline-none focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-gray-300 dark:focus-visible:outline-zinc-600 disabled:cursor-not-allowed"
            data-test="extend-slider"
            @input="minutes = choices[$event.target.valueAsNumber]"
          />
        </div>

        <p
          v-if="tooCostly"
          class="mt-3 rounded-lg border border-amber-200 dark:border-amber-500/30 bg-amber-50 dark:bg-amber-500/10 px-3 py-2 text-xs font-medium text-amber-800 dark:text-amber-300"
          data-test="extend-credits-warning"
        >
          This costs {{ cost }} credits but you have {{ credits }} left.
        </p>
        <p class="mt-3 text-xs text-gray-500 dark:text-zinc-400" data-test="extend-cost">
          <template v-if="sizes">
            New expiry in {{ expiresIn(newExpiry, now) }} ·
            <span class="font-bold text-gray-900 dark:text-zinc-100">{{ cost }} credits</span>
          </template>
          <template v-else>Loading files…</template>
        </p>
      </template>

      <div class="mt-5 flex justify-end gap-2">
        <button
          type="button"
          :disabled="busy"
          class="rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-600 disabled:opacity-50 transition-colors"
          data-test="extend-cancel"
          @click="cancel"
        >
          {{ choices.length ? 'Cancel' : 'Close' }}
        </button>
        <button
          v-if="choices.length"
          type="button"
          :disabled="!sizes || busy"
          class="rounded-lg bg-black dark:bg-white px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-white dark:text-zinc-900 hover:bg-gray-800 dark:hover:bg-zinc-200 disabled:opacity-50 transition-colors"
          data-test="extend-confirm"
          @click="confirm"
        >
          {{ busy ? 'Extending…' : 'Extend' }}
        </button>
      </div>
    </div>
  </div>
</template>
