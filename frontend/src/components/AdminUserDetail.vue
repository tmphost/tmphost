<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
// One user as root sees them: credits, usage, folders and credit history.
import { computed, onMounted, ref } from 'vue'
import CreditHistory from './CreditHistory.vue'
import StatCharts from './StatCharts.vue'
import StatTile from './StatTile.vue'
import { fetchAdminUser, listAdminUserCreditActivities } from '../api'
import { useCreditHistory } from '../composables/useCreditHistory'
import { CHART_COLORS, HISTORY_DAYS, dailyCreditSeries, renewsIn } from '../utils/credits'

const props = defineProps({
  // The user's base62 ID.
  id: { type: String, required: true },
})
const emit = defineEmits(['back', 'grant', 'error'])

const SECONDARY =
  'rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-3 py-2 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-700 transition-colors'
const PRIMARY =
  'rounded-lg bg-black dark:bg-white px-3 py-2 text-[11px] font-black uppercase tracking-widest text-white dark:text-zinc-900 hover:bg-gray-800 dark:hover:bg-zinc-200 transition-colors'

const user = ref(null)
const missing = ref(false)
const failed = ref(false)

const { activities, next, loading, loadingOlder, loadRange, loadOlder } = useCreditHistory(
  (before) => listAdminUserCreditActivities(props.id, before),
  (err) => emit('error', err),
)

const series = computed(() => dailyCreditSeries(activities.value))
const charts = computed(() => [
  { id: 'used', title: 'Credits used', hint: 'Per day', color: CHART_COLORS.used, data: series.value.used },
  { id: 'balance', title: 'Balance', hint: 'End of day', color: CHART_COLORS.balance, data: series.value.balance },
])

function formatDate(iso) {
  return new Date(iso).toLocaleDateString(undefined, { dateStyle: 'medium' })
}

onMounted(async () => {
  try {
    user.value = await fetchAdminUser(props.id)
  } catch (err) {
    if (err.status === 404) {
      missing.value = true
      return
    }
    failed.value = true
    emit('error', err)
    return
  }
  await loadRange()
})
</script>

<template>
  <div class="flex flex-col gap-4" data-test="admin-user">
    <div>
      <button type="button" :class="SECONDARY" data-test="admin-user-back" @click="emit('back')">← All users</button>
    </div>

    <p v-if="missing" class="rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-4 py-6 text-xs text-gray-500 dark:text-zinc-400" data-test="admin-user-missing">
      No user has the ID <span class="font-mono">{{ id }}</span>.
    </p>

    <p v-else-if="failed" class="rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-4 py-6 text-xs text-gray-500 dark:text-zinc-400" data-test="admin-user-failed">
      This user could not be loaded.
    </p>

    <template v-else-if="user">
      <header class="flex flex-wrap items-center gap-3 rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-4">
        <img v-if="user.picture" :src="user.picture" alt="" class="w-10 h-10 rounded-full shrink-0" referrerpolicy="no-referrer" />
        <div class="grow min-w-0">
          <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100 truncate" data-test="admin-user-name">{{ user.name || user.email }}</h2>
          <p class="text-xs text-gray-500 dark:text-zinc-400 truncate">{{ user.email }}</p>
          <p class="text-[10px] text-gray-400 dark:text-zinc-500">
            <span class="font-mono" data-test="admin-user-id">{{ user.id }}</span>
            <template v-if="user.provider"> · {{ user.provider }}</template> · joined {{ formatDate(user.createdAt) }}
          </p>
        </div>
        <button type="button" :class="PRIMARY" data-test="admin-user-grant" @click="emit('grant', user.email)">Grant credits</button>
      </header>

      <div class="grid grid-cols-2 md:grid-cols-4 gap-4">
        <StatTile label="Balance" :value="user.credits.toLocaleString()" :color="CHART_COLORS.balance" test="admin-user-balance" />
        <StatTile :label="`Used, ${HISTORY_DAYS} days`" :value="user.used.toLocaleString()" :color="CHART_COLORS.used" test="admin-user-used" />
        <StatTile label="Monthly grant" :value="user.monthlyGrant.toLocaleString()" test="admin-user-monthly">
          <span class="text-[10px] text-gray-400 dark:text-zinc-500" data-test="admin-user-renewal">{{ renewsIn(user.creditsRenewAt) }}</span>
        </StatTile>
        <StatTile label="Live folders" :value="user.folders" test="admin-user-folders" />
      </div>

      <StatCharts :charts="charts" :loading="loading" />

      <CreditHistory :activities="activities" :next="next" :loading="loading" :loading-older="loadingOlder" @older="loadOlder" />
    </template>

    <p v-else class="px-1 py-6 text-[10px] font-black text-gray-300 dark:text-zinc-500 animate-pulse" data-test="admin-user-loading">Loading...</p>
  </div>
</template>
