<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed, onMounted, ref } from 'vue'
import AdminOverview from '../components/AdminOverview.vue'
import AdminUserDetail from '../components/AdminUserDetail.vue'
import AdminUsers from '../components/AdminUsers.vue'
import BrandLogo from '../components/BrandLogo.vue'
import { fetchSettings, grantCredits, setupLogin, setupLogout } from '../api'
import { useToasts } from '../composables/useToasts'

// MAX_GRANT mirrors the server's credit.MaxAdminGrant.
const MAX_GRANT = 1000000

const INPUT =
  'w-full rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-3 py-2 text-sm text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-500 focus:border-gray-400 dark:focus:border-zinc-500 focus:outline-none'
const LABEL = 'block text-[11px] font-bold text-gray-500 dark:text-zinc-400 mb-1'
const PRIMARY =
  'rounded-lg bg-black dark:bg-white px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-white dark:text-zinc-900 hover:bg-gray-800 dark:hover:bg-zinc-200 disabled:opacity-50 transition-colors'
const SECONDARY =
  'rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-600 transition-colors'

// The admin menu: one section at a time.
const SECTIONS = [
  { id: 'overview', label: 'Overview', icon: 'M3 3v18h18M7 15l4-4 3 3 5-6' },
  {
    id: 'users',
    label: 'Users',
    icon: 'M15 19.128a9.38 9.38 0 0 0 2.625.372 9.337 9.337 0 0 0 4.121-.952 4.125 4.125 0 0 0-7.533-2.493M15 19.128v-.003c0-1.113-.285-2.16-.786-3.07M15 19.128v.106A12.318 12.318 0 0 1 8.624 21c-2.331 0-4.512-.645-6.374-1.766l-.001-.109a6.375 6.375 0 0 1 11.964-3.07M12 6.375a3.375 3.375 0 1 1-6.75 0 3.375 3.375 0 0 1 6.75 0zm8.25 2.25a2.625 2.625 0 1 1-5.25 0 2.625 2.625 0 0 1 5.25 0z',
  },
  { id: 'grant', label: 'Grant credits', icon: 'M12 4.5v15m7.5-7.5h-15' },
]

const TARGETS = [
  { id: 'all', label: 'All users' },
  { id: 'one', label: 'One user' },
]

const { notifyError, notifySuccess } = useToasts()

// 'loading' | 'login' | 'admin'
const phase = ref('loading')
const token = ref('')
const loginError = ref('')
const busy = ref(false)

const section = ref(SECTIONS[0].id)
// The user whose page is open in the users section; empty for the list.
const userId = ref('')

const target = ref('all')
const email = ref('')
const credits = ref(100)
const grantError = ref('')
// The last grant: { credits, users, email, expiresAt }.
const result = ref(null)

const canGrant = computed(
  () => Number.isInteger(credits.value) && credits.value > 0 && credits.value <= MAX_GRANT && (target.value === 'all' || email.value.trim() !== ''),
)

function formatDate(iso) {
  return new Date(iso).toLocaleString(undefined, { dateStyle: 'medium', timeStyle: 'short' })
}

async function refresh() {
  // There is no admin state to load: the settings call only tells whether
  // the root session is still valid.
  try {
    phase.value = (await fetchSettings()) ? 'admin' : 'login'
  } catch (err) {
    notifyError(err.message)
    phase.value = 'login'
  }
}

async function login() {
  busy.value = true
  loginError.value = ''
  try {
    await setupLogin(token.value)
    token.value = ''
    await refresh()
  } catch (err) {
    loginError.value = err.message
  } finally {
    busy.value = false
  }
}

// openSection shows a section; the users section starts at the list.
function openSection(id) {
  section.value = id
  userId.value = ''
}

// grantTo opens the grant form for one user.
function grantTo(userEmail) {
  target.value = 'one'
  email.value = userEmail
  result.value = null
  grantError.value = ''
  section.value = 'grant'
}

// onError signs out when the root session is gone, else reports the error.
function onError(err) {
  if (err.status === 401) {
    phase.value = 'login'
    return
  }
  notifyError(err.message)
}

async function logout() {
  try {
    await setupLogout()
  } catch {
    // The cookie expires on its own; forget the session here regardless.
  }
  result.value = null
  openSection(SECTIONS[0].id)
  phase.value = 'login'
}

async function grant() {
  busy.value = true
  grantError.value = ''
  result.value = null
  const all = target.value === 'all'
  try {
    const rs = await grantCredits({ credits: credits.value, email: email.value.trim(), all })
    result.value = { credits: credits.value, users: rs.users, email: all ? '' : email.value.trim(), expiresAt: rs.expiresAt }
    notifySuccess(`Granted ${credits.value} credits to ${rs.users === 1 ? '1 user' : `${rs.users} users`}`)
  } catch (err) {
    if (err.status === 401) {
      phase.value = 'login'
      return
    }
    grantError.value = err.message
  } finally {
    busy.value = false
  }
}

onMounted(refresh)
</script>

<template>
  <div class="min-h-[100dvh] bg-zinc-50 dark:bg-zinc-950 px-4 py-10">
    <div v-if="phase === 'loading'" class="flex justify-center py-24" data-test="admin-loading">
      <svg class="animate-spin h-5 w-5 text-gray-400 dark:text-zinc-500" fill="none" viewBox="0 0 24 24">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
      </svg>
    </div>

    <!-- Root sign-in -->
    <div v-else-if="phase === 'login'" class="min-h-[80dvh] flex items-center justify-center">
      <form
        class="w-full max-w-sm bg-white dark:bg-zinc-900 border border-gray-200 dark:border-zinc-800 rounded-xl shadow-sm p-8"
        data-test="admin-login"
        @submit.prevent="login"
      >
        <div class="text-center mb-8">
          <BrandLogo tile class="w-12 h-12 mx-auto mb-5" />
          <h1 class="text-2xl font-black text-gray-900 dark:text-zinc-100 tracking-tight">Admin</h1>
          <p class="mt-1.5 text-sm text-gray-500 dark:text-zinc-400">Enter the root access token to manage tmphost.io.</p>
        </div>
        <label for="admin-root-token" :class="LABEL">Root access token</label>
        <input
          id="admin-root-token"
          v-model="token"
          type="password"
          autocomplete="current-password"
          required
          :class="INPUT"
          data-test="admin-token"
        />
        <p v-if="loginError" role="alert" class="mt-3 text-xs font-medium text-rose-600 dark:text-rose-400" data-test="admin-login-error">
          {{ loginError }}
        </p>
        <button type="submit" :disabled="busy || !token" :class="[PRIMARY, 'mt-5 w-full']" data-test="admin-unlock">Unlock admin</button>
      </form>
    </div>

    <!-- Admin -->
    <div v-else class="mx-auto w-full max-w-5xl" data-test="admin-page">
      <header class="flex flex-wrap items-center gap-3 mb-6">
        <BrandLogo tile class="w-9 h-9" />
        <div class="grow min-w-0">
          <h1 class="text-lg font-bold text-gray-900 dark:text-zinc-100 tracking-tight">Admin</h1>
          <p class="text-xs text-gray-500 dark:text-zinc-400">Manage tmphost.io accounts. Only root can see this page.</p>
        </div>
        <button type="button" :class="SECONDARY" data-test="admin-logout" @click="logout">Sign out</button>
      </header>

      <div class="flex flex-col md:flex-row gap-4 md:gap-8 min-w-0">
        <!-- Menu: a scrolling row above the section on phones, a column beside it from md up. -->
        <nav class="w-full md:w-48 shrink-0" aria-label="Admin">
          <ul class="flex md:flex-col gap-1 overflow-x-auto no-scrollbar">
            <li v-for="item in SECTIONS" :key="item.id" class="shrink-0">
              <button
                type="button"
                :aria-current="section === item.id ? 'page' : undefined"
                :class="[
                  'w-full flex items-center gap-2.5 px-3 py-2 rounded-lg text-xs whitespace-nowrap transition-colors',
                  section === item.id
                    ? 'bg-gray-200/70 dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 font-semibold'
                    : 'text-gray-500 dark:text-zinc-400 hover:bg-gray-100 dark:hover:bg-zinc-800 hover:text-gray-900 dark:hover:text-zinc-100',
                ]"
                :data-test="`admin-tab-${item.id}`"
                @click="openSection(item.id)"
              >
                <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5" aria-hidden="true">
                  <path stroke-linecap="round" stroke-linejoin="round" :d="item.icon" />
                </svg>
                {{ item.label }}
              </button>
            </li>
          </ul>
        </nav>

        <div class="flex-1 min-w-0">
          <AdminOverview v-if="section === 'overview'" @error="onError" />
          <AdminUserDetail
            v-else-if="section === 'users' && userId"
            :id="userId"
            :key="userId"
            @back="userId = ''"
            @grant="grantTo"
            @error="onError"
          />
          <AdminUsers v-else-if="section === 'users'" @open="userId = $event" @error="onError" />
          <form
            v-else
            class="max-w-2xl rounded-xl border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-5"
            data-test="admin-grant"
            @submit.prevent="grant"
          >
            <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100">Grant credits</h2>
            <p class="text-xs text-gray-500 dark:text-zinc-400 mb-4">
              Granted credits expire 30 days later, at 00:00 UTC; whatever is left of them is taken back. Monthly renewals don't reset them.
            </p>

            <div class="space-y-4">
              <div>
                <span :class="LABEL">Grant to</span>
                <div class="inline-flex rounded-lg border border-gray-200 dark:border-zinc-800 p-0.5" role="radiogroup" aria-label="Grant to">
                  <button
                    v-for="t in TARGETS"
                    :key="t.id"
                    type="button"
                    role="radio"
                    :aria-checked="target === t.id"
                    :class="[
                      'px-3 py-1.5 rounded-md text-xs transition-colors',
                      target === t.id
                        ? 'bg-gray-200/70 dark:bg-zinc-800 text-gray-900 dark:text-zinc-100 font-semibold'
                        : 'text-gray-500 dark:text-zinc-400 hover:text-gray-900 dark:hover:text-zinc-100',
                    ]"
                    :data-test="`admin-target-${t.id}`"
                    @click="target = t.id"
                  >
                    {{ t.label }}
                  </button>
                </div>
              </div>

              <div v-if="target === 'one'">
                <label for="admin-email" :class="LABEL">User email</label>
                <input id="admin-email" v-model="email" type="email" placeholder="user@example.com" :class="INPUT" data-test="admin-email" />
              </div>

              <div>
                <label for="admin-credits" :class="LABEL">Credits</label>
                <input
                  id="admin-credits"
                  v-model.number="credits"
                  type="number"
                  min="1"
                  :max="MAX_GRANT"
                  step="1"
                  :class="INPUT"
                  data-test="admin-credits"
                />
                <p class="mt-1 text-[11px] text-gray-400 dark:text-zinc-500">Between 1 and {{ MAX_GRANT.toLocaleString() }} per user.</p>
              </div>
            </div>

            <p
              v-if="grantError"
              role="alert"
              class="mt-4 rounded-lg border border-rose-100 dark:border-rose-500/20 bg-rose-50 dark:bg-rose-500/10 px-3 py-2 text-xs font-medium text-rose-600 dark:text-rose-400"
              data-test="admin-grant-error"
            >
              {{ grantError }}
            </p>
            <p
              v-if="result"
              class="mt-4 rounded-lg border border-emerald-200 dark:border-emerald-500/30 bg-emerald-50 dark:bg-emerald-500/10 px-3 py-2 text-xs font-medium text-emerald-800 dark:text-emerald-300"
              data-test="admin-grant-result"
            >
              Granted {{ result.credits }} credits to {{ result.email || (result.users === 1 ? '1 user' : `${result.users} users`) }}. They expire
              {{ formatDate(result.expiresAt) }}.
            </p>

            <div class="mt-5 flex justify-end">
              <button type="submit" :disabled="busy || !canGrant" :class="PRIMARY" data-test="admin-grant-submit">
                {{ target === 'all' ? 'Grant to all users' : 'Grant' }}
              </button>
            </div>
          </form>
        </div>
      </div>
    </div>
  </div>
</template>
