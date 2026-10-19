<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed } from 'vue'
import { formatBytes } from '../utils/files'

// UploadProgress shows how much of an upload has been sent: a dark gray to
// silver bar over the bytes, with the bytes and percentage above it. The
// upload button counts the files.
const props = defineProps({
  // Bytes sent so far, across every file.
  sent: { type: Number, default: 0 },
  // Bytes in all, across every file.
  total: { type: Number, default: 0 },
})

const percent = computed(() => (props.total > 0 ? Math.min(100, Math.round((props.sent / props.total) * 100)) : 0))
</script>

<template>
  <div data-test="upload-progress">
    <div class="flex items-baseline justify-between gap-3 text-[11px] font-bold tabular-nums">
      <span class="text-gray-500 dark:text-zinc-400" data-test="upload-progress-bytes">
        {{ formatBytes(Math.min(sent, total)) }} of {{ formatBytes(total) }}
      </span>
      <span class="text-gray-900 dark:text-zinc-100" data-test="upload-progress-percent">{{ percent }}%</span>
    </div>
    <div
      class="mt-1.5 h-2 overflow-hidden rounded-full bg-gray-100 dark:bg-zinc-800"
      role="progressbar"
      aria-label="Upload progress"
      aria-valuemin="0"
      aria-valuemax="100"
      :aria-valuenow="percent"
    >
      <div
        class="upload-progress-fill relative h-full overflow-hidden rounded-full bg-linear-to-r from-zinc-700 via-zinc-500 to-zinc-300 dark:from-zinc-500 dark:via-zinc-300 dark:to-zinc-100 transition-[width] duration-300 ease-out"
        :style="{ width: `${percent}%` }"
        data-test="upload-progress-bar"
      />
    </div>
  </div>
</template>

<style scoped>
/* A soft silver sheen sweeps along the bar while it fills. */
.upload-progress-fill::after {
  content: '';
  position: absolute;
  inset: 0;
  background: linear-gradient(90deg, transparent, rgb(255 255 255 / 0.45), transparent);
  transform: translateX(-100%);
  animation: upload-progress-sheen 1.6s ease-in-out infinite;
}

@keyframes upload-progress-sheen {
  to {
    transform: translateX(100%);
  }
}

@media (prefers-reduced-motion: reduce) {
  .upload-progress-fill::after {
    animation: none;
  }
}
</style>
