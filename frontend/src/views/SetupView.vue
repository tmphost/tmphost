<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { onMounted, ref } from 'vue'
import BrandLogo from '../components/BrandLogo.vue'
import { fetchSettings, saveSettings, setupLogin, setupLogout } from '../api'
import { useToasts } from '../composables/useToasts'

const PG_DSN_EXAMPLE = 'postgres://user:password@host:5432/tmphost?sslmode=disable'

const INPUT =
  'w-full rounded-lg border border-gray-200 bg-white px-3 py-2 text-sm text-gray-900 placeholder:text-gray-400 focus:border-gray-400 focus:outline-none'
const LABEL = 'block text-[11px] font-bold text-gray-500 mb-1'
const PRIMARY =
  'rounded-lg bg-black px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-white hover:bg-gray-800 disabled:opacity-50 transition-colors'
const SECONDARY =
  'rounded-lg border border-gray-200 bg-white px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-gray-900 hover:border-gray-300 transition-colors'

const { notifyError, notifySuccess } = useToasts()

// 'loading' | 'login' | 'settings'
const phase = ref('loading')
const token = ref('')
const loginError = ref('')
const busy = ref(false)

const form = ref(null)
const secretsSet = ref({})
const restartRequired = ref(false)
const saveError = ref('')

function load(state) {
  // Work on a copy; secrets always arrive blank.
  form.value = JSON.parse(JSON.stringify(state.settings))
  secretsSet.value = state.secretsSet
  restartRequired.value = state.restartRequired
  phase.value = 'settings'
}

async function refresh() {
  try {
    const state = await fetchSettings()
    if (state) load(state)
    else phase.value = 'login'
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

async function logout() {
  try {
    await setupLogout()
  } catch {
    // The cookie expires on its own; forget the session here regardless.
  }
  form.value = null
  phase.value = 'login'
}

async function save() {
  busy.value = true
  saveError.value = ''
  try {
    load(await saveSettings(form.value))
    notifySuccess('Settings saved')
  } catch (err) {
    if (err.status === 401) {
      phase.value = 'login'
      loginError.value = 'Your setup session expired. Sign in again.'
      return
    }
    saveError.value = err.message
    notifyError(err.message)
  } finally {
    busy.value = false
  }
}

function secretPlaceholder(isSet) {
  return isSet ? '•••••••• (saved — leave blank to keep)' : 'Not set'
}

onMounted(refresh)
</script>

<template>
  <div class="min-h-[100dvh] bg-zinc-50 px-4 py-10">
    <div v-if="phase === 'loading'" class="flex justify-center py-24" data-test="setup-loading">
      <svg class="animate-spin h-5 w-5 text-gray-400" fill="none" viewBox="0 0 24 24">
        <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
        <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
      </svg>
    </div>

    <!-- Root sign-in -->
    <div v-else-if="phase === 'login'" class="min-h-[80dvh] flex items-center justify-center">
      <form
        class="w-full max-w-sm bg-white border border-gray-200 rounded-xl shadow-sm p-8"
        data-test="setup-login"
        @submit.prevent="login"
      >
        <div class="text-center mb-8">
          <BrandLogo tile class="w-12 h-12 mx-auto mb-5" />
          <h1 class="text-2xl font-black text-gray-900 tracking-tight">Setup</h1>
          <p class="mt-1.5 text-sm text-gray-500">Enter the root access token to configure tmphost.io.</p>
        </div>
        <label for="root-token" :class="LABEL">Root access token</label>
        <input
          id="root-token"
          v-model="token"
          type="password"
          autocomplete="current-password"
          required
          :class="INPUT"
          data-test="setup-token"
        />
        <p v-if="loginError" role="alert" class="mt-3 text-xs font-medium text-rose-600" data-test="setup-login-error">
          {{ loginError }}
        </p>
        <button type="submit" :disabled="busy || !token" :class="[PRIMARY, 'mt-5 w-full']" data-test="setup-unlock">
          Unlock setup
        </button>
      </form>
    </div>

    <!-- Settings -->
    <form v-else class="mx-auto w-full max-w-2xl" data-test="setup-form" @submit.prevent="save">
      <header class="flex flex-wrap items-center gap-3 mb-6">
        <BrandLogo tile class="w-9 h-9" />
        <div class="grow min-w-0">
          <h1 class="text-lg font-bold text-gray-900 tracking-tight">Setup</h1>
          <p class="text-xs text-gray-500">Server settings for tmphost.io. Only root can see this page.</p>
        </div>
        <button type="button" :class="SECONDARY" data-test="setup-logout" @click="logout">Sign out</button>
        <button type="submit" :disabled="busy" :class="PRIMARY" data-test="setup-save">Save</button>
      </header>

      <p
        v-if="restartRequired"
        class="mb-4 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs font-medium text-amber-800"
        data-test="setup-restart"
      >
        Saved. Database, storage and sign-in changes take effect after the server restarts.
      </p>
      <p
        v-if="saveError"
        role="alert"
        class="mb-4 rounded-lg border border-rose-100 bg-rose-50 px-3 py-2 text-xs font-medium text-rose-600"
        data-test="setup-save-error"
      >
        {{ saveError }}
      </p>

      <div class="space-y-4">
        <!-- Credits -->
        <section class="rounded-xl border border-gray-200 bg-white p-5">
          <h2 class="text-sm font-bold text-gray-900">Credits</h2>
          <p class="text-xs text-gray-500 mb-4">Applies immediately: new accounts and the daily 00:00 UTC renewal use it.</p>
          <label for="monthly-grant" :class="LABEL">Operations per account, every 30 days</label>
          <input
            id="monthly-grant"
            v-model.number="form.credits.monthlyGrant"
            type="number"
            min="0"
            :class="[INPUT, 'max-w-[12rem]']"
            data-test="setup-grant"
          />
        </section>

        <!-- Database -->
        <section class="rounded-xl border border-gray-200 bg-white p-5">
          <h2 class="text-sm font-bold text-gray-900">Database</h2>
          <p class="text-xs text-gray-500 mb-4">Requires a restart. Data is not copied between databases.</p>
          <div class="inline-flex rounded-lg border border-gray-200 p-0.5 mb-4" role="radiogroup">
            <button
              v-for="option in [{ value: 'sqlite', label: 'SQLite' }, { value: 'postgres', label: 'PostgreSQL' }]"
              :key="option.value"
              type="button"
              role="radio"
              :aria-checked="form.database.driver === option.value"
              :class="[
                'rounded-md px-3 py-1.5 text-xs font-bold transition-colors',
                form.database.driver === option.value ? 'bg-black text-white' : 'text-gray-500 hover:text-gray-900',
              ]"
              :data-test="`setup-driver-${option.value}`"
              @click="form.database.driver = option.value"
            >
              {{ option.label }}
            </button>
          </div>

          <div v-if="form.database.driver === 'sqlite'" data-test="setup-sqlite">
            <label for="sqlite-dsn" :class="LABEL">SQLite DSN</label>
            <input id="sqlite-dsn" v-model="form.database.sqlite.dsn" :class="INPUT" />
          </div>
          <div v-else data-test="setup-postgres">
            <label for="pg-dsn" :class="LABEL">PostgreSQL DSN</label>
            <input
              id="pg-dsn"
              v-model="form.database.postgres.dsn"
              type="password"
              autocomplete="off"
              spellcheck="false"
              :placeholder="secretsSet.postgresDsn ? secretPlaceholder(true) : PG_DSN_EXAMPLE"
              :class="INPUT"
            />
            <p class="mt-1 text-[11px] text-gray-400">A postgres:// URL or key=value string. It holds the password, so it is never shown again.</p>
          </div>
        </section>

        <!-- Storage -->
        <section class="rounded-xl border border-gray-200 bg-white p-5">
          <div class="flex items-start justify-between gap-4">
            <div>
              <h2 class="text-sm font-bold text-gray-900">S3 storage</h2>
              <p class="text-xs text-gray-500 mb-4">Any S3-compatible bucket. Requires a restart.</p>
            </div>
            <label class="flex items-center gap-2 text-xs font-bold text-gray-700">
              <input v-model="form.storage.s3.enabled" type="checkbox" class="accent-black" data-test="setup-s3-enabled" />
              Enabled
            </label>
          </div>
          <div class="grid grid-cols-1 sm:grid-cols-2 gap-3" :class="{ 'opacity-50': !form.storage.s3.enabled }">
            <div class="sm:col-span-2">
              <label for="s3-endpoint" :class="LABEL">Endpoint (blank for AWS)</label>
              <input id="s3-endpoint" v-model="form.storage.s3.endpoint" placeholder="https://" :class="INPUT" />
            </div>
            <div>
              <label for="s3-region" :class="LABEL">Region</label>
              <input id="s3-region" v-model="form.storage.s3.region" :class="INPUT" />
            </div>
            <div>
              <label for="s3-bucket" :class="LABEL">Bucket</label>
              <input id="s3-bucket" v-model="form.storage.s3.bucket" :class="INPUT" />
            </div>
            <div>
              <label for="s3-key" :class="LABEL">Access key ID</label>
              <input id="s3-key" v-model="form.storage.s3.accessKeyId" :class="INPUT" />
            </div>
            <div>
              <label for="s3-secret" :class="LABEL">Secret access key</label>
              <input
                id="s3-secret"
                v-model="form.storage.s3.secretAccessKey"
                type="password"
                autocomplete="new-password"
                :placeholder="secretPlaceholder(secretsSet.s3SecretAccessKey)"
                :class="INPUT"
              />
            </div>
            <label class="sm:col-span-2 flex items-center gap-2 text-xs text-gray-700">
              <input v-model="form.storage.s3.usePathStyle" type="checkbox" class="accent-black" />
              Use path-style URLs (MinIO and some providers)
            </label>
          </div>
        </section>

        <!-- Sign-in providers -->
        <section class="rounded-xl border border-gray-200 bg-white p-5">
          <h2 class="text-sm font-bold text-gray-900">Sign-in providers</h2>
          <p class="text-xs text-gray-500 mb-4">Clear a client ID to disable that provider. Requires a restart.</p>
          <div class="space-y-4">
            <div
              v-for="provider in [
                { key: 'google', label: 'Google', secretSet: secretsSet.googleClientSecret },
                { key: 'github', label: 'GitHub', secretSet: secretsSet.githubClientSecret },
              ]"
              :key="provider.key"
              class="grid grid-cols-1 sm:grid-cols-2 gap-3"
              :data-test="`setup-provider-${provider.key}`"
            >
              <div>
                <label :for="`${provider.key}-id`" :class="LABEL">{{ provider.label }} client ID</label>
                <input :id="`${provider.key}-id`" v-model="form.auth[provider.key].clientId" :class="INPUT" />
              </div>
              <div>
                <label :for="`${provider.key}-secret`" :class="LABEL">{{ provider.label }} client secret</label>
                <input
                  :id="`${provider.key}-secret`"
                  v-model="form.auth[provider.key].clientSecret"
                  type="password"
                  autocomplete="new-password"
                  :placeholder="secretPlaceholder(provider.secretSet)"
                  :class="INPUT"
                />
              </div>
            </div>
          </div>
        </section>
      </div>
    </form>
  </div>
</template>
