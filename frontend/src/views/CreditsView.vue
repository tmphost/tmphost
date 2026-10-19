<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed, inject, onMounted, ref } from 'vue'
import ChartSVG from '../components/ChartSVG.vue'
import { listCreditActivities } from '../api'
import { useToasts } from '../composables/useToasts'
import { CHART_COLORS, HISTORY_DAYS, dailyCreditSeries, formatChange, renewsIn } from '../utils/credits'

const DAY_MS = 24 * 60 * 60 * 1000

const OPS_LABELS = {
  grant: 'Sign-up grant',
  renewal: 'Renewal',
  spend: 'Upload',
  refund: 'Refund',
  admin_grant: 'Admin grant',
  expiry: 'Admin grant expired',
}

const CARD = 'bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl'
const CARD_LABEL = 'text-[10px] font-black uppercase tracking-widest text-gray-400 dark:text-zinc-500'

const { user } = inject('currentUser', { user: ref(null) })
const { notifyError } = useToasts()

const activities = ref([])
const next = ref('')
const loading = ref(true)
const loadingOlder = ref(false)

const series = computed(() => dailyCreditSeries(activities.value))
const usedTotal = computed(() => series.value.used.reduce((sum, d) => sum + d.count, 0))
const renewal = computed(() => renewsIn(user.value?.creditsRenewAt))

// loadPage appends the next page of history, older than what is shown.
async function loadPage() {
  const page = await listCreditActivities(next.value)
  activities.value.push(...page.activities)
  next.value = page.next
  return page.activities
}

// The charts need every activity of the last HISTORY_DAYS days, plus one
// before them for the opening balance.
async function loadRange() {
  const since = (Math.floor(Date.now() / DAY_MS) - HISTORY_DAYS + 1) * DAY_MS
  try {
    let page
    do {
      page = await loadPage()
    } while (next.value && Date.parse(page.at(-1).createdAt) >= since)
  } catch (err) {
    notifyError(err.message)
  } finally {
    loading.value = false
  }
}

async function loadOlder() {
  loadingOlder.value = true
  try {
    await loadPage()
  } catch (err) {
    notifyError(err.message)
  } finally {
    loadingOlder.value = false
  }
}

function opsLabel(ops) {
  return OPS_LABELS[ops] || ops
}

function formatTime(at) {
  return new Date(at).toLocaleString()
}

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
        <div :class="[CARD, 'p-4 flex flex-col gap-1.5']">
          <div class="flex items-center gap-1.5">
            <span class="w-1.5 h-1.5 rounded-full" :style="{ background: CHART_COLORS.balance }"></span>
            <span :class="CARD_LABEL">Balance</span>
          </div>
          <span class="text-2xl font-black text-gray-900 dark:text-zinc-100 tabular-nums leading-none" data-test="credits-balance">{{
            user?.credits ?? 0
          }}</span>
        </div>
        <div :class="[CARD, 'p-4 flex flex-col gap-1.5']">
          <div class="flex items-center gap-1.5">
            <span class="w-1.5 h-1.5 rounded-full" :style="{ background: CHART_COLORS.used }"></span>
            <span :class="CARD_LABEL">Used, {{ HISTORY_DAYS }} days</span>
          </div>
          <span class="text-2xl font-black text-gray-900 dark:text-zinc-100 tabular-nums leading-none" data-test="credits-used">{{
            usedTotal
          }}</span>
        </div>
        <div :class="[CARD, 'p-4 flex flex-col gap-1.5 col-span-2 md:col-span-1']">
          <span :class="CARD_LABEL">Monthly grant</span>
          <span class="text-2xl font-black text-gray-900 dark:text-zinc-100 tabular-nums leading-none" data-test="credits-monthly">{{
            user?.monthlyGrant ?? 0
          }}</span>
          <span class="text-[10px] text-gray-400 dark:text-zinc-500" data-test="credits-renewal">{{ renewal }}</span>
        </div>
      </div>

      <!-- Charts -->
      <div class="grid grid-cols-1 lg:grid-cols-2 gap-4">
        <div
          v-for="chart in [
            { id: 'used', title: 'Credits used', hint: 'Per day' },
            { id: 'balance', title: 'Balance', hint: 'End of day' },
          ]"
          :key="chart.id"
          :class="[CARD, 'p-5']"
          :data-test="`credits-chart-${chart.id}`"
        >
          <div class="flex items-center justify-between mb-4">
            <div class="flex items-center gap-2">
              <span class="w-2 h-2 rounded-full" :style="{ background: CHART_COLORS[chart.id] }"></span>
              <h2 class="text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100">{{ chart.title }}</h2>
            </div>
            <span class="text-gray-400 dark:text-zinc-500 text-[9px] font-black uppercase tracking-widest">{{ chart.hint }}</span>
          </div>
          <div class="h-48 w-full relative">
            <div v-if="loading" class="absolute inset-0 flex items-center justify-center text-[10px] font-black text-gray-300 dark:text-zinc-500 animate-pulse">
              Loading...
            </div>
            <ChartSVG v-else :data="series[chart.id]" :color="CHART_COLORS[chart.id]" />
          </div>
        </div>
      </div>

      <!-- History -->
      <section :class="CARD" data-test="credits-history">
        <h2 class="px-4 py-3 border-b border-gray-100 dark:border-zinc-800 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100">
          History
        </h2>
        <p v-if="!loading && activities.length === 0" class="px-4 py-6 text-xs text-gray-400 dark:text-zinc-500" data-test="credits-empty">
          No credit activity yet.
        </p>
        <ul class="divide-y divide-gray-100 dark:divide-zinc-800">
          <li v-for="a in activities" :key="a.id" class="flex items-center gap-3 px-4 py-2.5 text-xs" data-test="credits-row">
            <div class="flex-1 min-w-0">
              <p class="font-semibold text-gray-900 dark:text-zinc-100">{{ opsLabel(a.ops) }}</p>
              <p class="text-[10px] text-gray-400 dark:text-zinc-500">
                <span v-if="a.agent" class="font-semibold text-gray-600 dark:text-zinc-300" data-test="credits-agent">{{ a.agent }} · </span
                >{{ formatTime(a.createdAt) }}
              </p>
            </div>
            <span
              :class="[
                'font-bold tabular-nums',
                a.change > 0 ? 'text-emerald-600 dark:text-emerald-400' : 'text-gray-900 dark:text-zinc-100',
              ]"
              data-test="credits-change"
              >{{ formatChange(a.change) }}</span
            >
            <span class="w-16 text-right tabular-nums text-gray-500 dark:text-zinc-400" data-test="credits-current">{{ a.current }}</span>
          </li>
        </ul>
        <div v-if="next" class="px-4 py-3 border-t border-gray-100 dark:border-zinc-800">
          <button
            type="button"
            class="rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-3 py-1.5 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-700 disabled:opacity-50"
            :disabled="loadingOlder"
            data-test="credits-older"
            @click="loadOlder"
          >
            Load older
          </button>
        </div>
      </section>
    </div>
  </div>
</template>
