<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed } from 'vue'
import { usePwaUpdate } from '../composables/usePwaUpdate'

// The progress bar while a new version downloads, then the "Update now" offer,
// then the bar again until the page reloads under the new version.
const { needRefresh, progress, update, dismiss } = usePwaUpdate()

const label = computed(() => {
  if (progress.value?.phase === 'downloading') return 'Downloading a new version…'
  if (progress.value?.phase === 'restarting') return 'Restarting with the new version…'
  return 'A new version is available.'
})
</script>

<template>
  <Transition name="slide-down">
    <div
      v-if="progress || needRefresh"
      class="fixed inset-x-0 top-0 z-[200] flex items-center justify-between gap-3 px-4 py-2.5 overflow-hidden bg-black dark:bg-zinc-800 text-white text-xs font-medium border-b border-transparent dark:border-zinc-700"
      data-test="update-banner"
    >
      <div class="flex items-center gap-2 min-w-0">
        <svg :class="['w-3.5 h-3.5 shrink-0 text-emerald-400', progress ? 'animate-spin' : '']" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
          <path stroke-linecap="round" stroke-linejoin="round" d="M4 4v5h.582m15.356 2A8.001 8.001 0 004.582 9m0 0H9m11 11v-5h-.581m0 0a8.003 8.003 0 01-15.357-2m15.357 2H15" />
        </svg>
        <span class="truncate" data-test="update-label">{{ label }}</span>
      </div>
      <span
        v-if="progress && progress.percent !== null"
        class="shrink-0 tabular-nums text-zinc-300"
        data-test="update-percent"
      >{{ Math.floor(progress.percent) }}%</span>
      <div v-else-if="!progress" class="flex items-center gap-2 shrink-0">
        <button
          type="button"
          class="px-3 py-1 bg-white text-black text-[11px] font-black uppercase tracking-widest rounded-lg hover:bg-gray-100 active:scale-95 transition-all"
          data-test="update-now"
          @click="update"
        >
          Update now
        </button>
        <button
          type="button"
          class="p-0.5 text-zinc-400 hover:text-white transition-colors"
          title="Dismiss"
          data-test="update-dismiss"
          @click="dismiss"
        >
          <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2.5">
            <path stroke-linecap="round" stroke-linejoin="round" d="M6 18L18 6M6 6l12 12" />
          </svg>
        </button>
      </div>
      <!-- Filled to the download percentage, or sweeping without a number. -->
      <div
        v-if="progress"
        class="absolute inset-x-0 bottom-0 h-0.5 bg-white/15"
        role="progressbar"
        aria-valuemin="0"
        aria-valuemax="100"
        :aria-valuenow="progress.percent ?? undefined"
        :aria-label="label"
        data-test="update-progress"
      >
        <div
          v-if="progress.percent !== null"
          class="h-full bg-emerald-400 transition-[width] duration-300 ease-out"
          :style="{ width: `${progress.percent}%` }"
        ></div>
        <div v-else class="update-progress-sweep h-full w-1/3 bg-emerald-400"></div>
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.update-progress-sweep {
  animation: update-progress-sweep 1.4s ease-in-out infinite;
}
@keyframes update-progress-sweep {
  from { transform: translateX(-100%); }
  to { transform: translateX(300%); }
}
.slide-down-enter-active,
.slide-down-leave-active {
  transition: transform 0.25s ease, opacity 0.25s ease;
}
.slide-down-enter-from,
.slide-down-leave-to {
  transform: translateY(-100%);
  opacity: 0;
}
</style>
