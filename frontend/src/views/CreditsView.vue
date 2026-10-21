<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed, inject, onMounted, ref } from 'vue'
import CreditHistory from '../components/CreditHistory.vue'
import StatCharts from '../components/StatCharts.vue'
import StatTile from '../components/StatTile.vue'
import { listCreditActivities } from '../api'
import { useCreditHistory } from '../composables/useCreditHistory'
import { useToasts } from '../composables/useToasts'
import { CHART_COLORS, HISTORY_DAYS, dailyCreditSeries, renewsIn } from '../utils/credits'

const { user } = inject('currentUser', { user: ref(null) })
const { notifyError } = useToasts()

const { activities, next, loading, loadingOlder, loadRange, loadOlder } = useCreditHistory(listCreditActivities, (err) => notifyError(err.message))

const series = computed(() => dailyCreditSeries(activities.value))
const usedTotal = computed(() => series.value.used.reduce((sum, d) => sum + d.count, 0))
const renewal = computed(() => renewsIn(user.value?.creditsRenewAt))
const charts = computed(() => [
  { id: 'used', title: 'Credits used', hint: 'Per day', color: CHART_COLORS.used, data: series.value.used },
  { id: 'balance', title: 'Balance', hint: 'End of day', color: CHART_COLORS.balance, data: series.value.balance },
])

onMounted(loadRange)
</script>

<template>
  <div class="flex flex-col">
    <header class="px-4 pb-4 border-b border-gray-100 dark:border-zinc-800">
      <h1 class="text-lg font-bold text-gray-900 dark:text-zinc-100 tracking-tight">Credits</h1>
      <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">
        Every change to your operation credits: the sign-up grant, renewals, uploads, refunds and admin grants.
      </p>
    </header>

    <div class="px-4 pt-4 flex flex-col gap-4">
      <!-- Summary -->
      <div class="grid grid-cols-2 md:grid-cols-3 gap-4">
        <StatTile label="Balance" :value="user?.credits ?? 0" :color="CHART_COLORS.balance" test="credits-balance" />
        <StatTile :label="`Used, ${HISTORY_DAYS} days`" :value="usedTotal" :color="CHART_COLORS.used" test="credits-used" />
        <StatTile class="col-span-2 md:col-span-1" label="Monthly grant" :value="user?.monthlyGrant ?? 0" test="credits-monthly">
          <span class="text-[10px] text-gray-400 dark:text-zinc-500" data-test="credits-renewal">{{ renewal }}</span>
        </StatTile>
      </div>

      <StatCharts :charts="charts" :loading="loading" />

      <CreditHistory :activities="activities" :next="next" :loading="loading" :loading-older="loadingOlder" @older="loadOlder" />
    </div>
  </div>
</template>
