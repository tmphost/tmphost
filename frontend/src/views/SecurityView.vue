<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed, onMounted, ref } from 'vue'
import { fetchAccountPin, newTotp, saveAccountPin } from '../api'
import { useToasts } from '../composables/useToasts'

// SecurityView sets the PINs visitors enter before they see a file: one
// account PIN for every folder (a static PIN or an authenticator app's
// codes), and, while that is off, a PIN of its own for each folder an agent
// creates.

const MODES = [
  { value: 'off', label: 'Off', hint: 'Folders open for anyone with the link, unless they have a PIN of their own.' },
  { value: 'static', label: 'Static PIN', hint: 'One 6 digit PIN you choose, for every folder.' },
  { value: 'totp', label: 'Authenticator app', hint: 'The 6 digit code of an app such as Google Authenticator or 1Password, which changes every 30 seconds.' },
]
const PIN_PATTERN = /^[0-9]{6}$/

const { notifyError, notifySuccess } = useToasts()

// saved is what the server has; null until loaded.
const saved = ref(null)
const mode = ref('off')
const agentPins = ref(false)
// pin is a new static PIN; empty keeps the saved one.
const pin = ref('')
// totp is a new authenticator secret to scan, { secret, url, qrCode }, and
// code its current code, which proves it was scanned.
const totp = ref(null)
const code = ref('')
const settingUp = ref(false)
const saving = ref(false)

const keepsPin = computed(() => saved.value?.mode === 'static' && pin.value === '')
const keepsTotp = computed(() => saved.value?.mode === 'totp' && !totp.value)
const canSave = computed(() => {
  if (!saved.value || saving.value) return false
  if (mode.value === 'static') return PIN_PATTERN.test(pin.value) || keepsPin.value
  if (mode.value === 'totp') return (Boolean(totp.value) && PIN_PATTERN.test(code.value)) || keepsTotp.value
  return true
})

function load(settings) {
  saved.value = settings
  mode.value = settings.mode
  agentPins.value = settings.agentPins
  pin.value = ''
  totp.value = null
  code.value = ''
}

onMounted(async () => {
  try {
    load(await fetchAccountPin())
  } catch (err) {
    notifyError(err.message)
  }
})

// setUpTotp asks for a new secret to scan; nothing changes until it is saved
// with a code.
async function setUpTotp() {
  settingUp.value = true
  try {
    totp.value = await newTotp()
    code.value = ''
  } catch (err) {
    notifyError(err.message)
  } finally {
    settingUp.value = false
  }
}

async function save() {
  const body = { mode: mode.value, agentPins: agentPins.value }
  if (mode.value === 'static' && pin.value) body.pin = pin.value
  if (mode.value === 'totp' && totp.value) Object.assign(body, { secret: totp.value.secret, code: code.value })
  saving.value = true
  try {
    load(await saveAccountPin(body))
    notifySuccess('PIN settings saved')
  } catch (err) {
    notifyError(err.message)
  } finally {
    saving.value = false
  }
}

// digits keeps only the digits of a typed PIN or code.
const digits = (value) => value.replace(/\D/g, '').slice(0, 6)
</script>

<template>
  <div class="flex flex-col">
    <header class="px-4 pb-4 border-b border-gray-100 dark:border-zinc-800">
      <h1 class="text-lg font-bold text-gray-900 dark:text-zinc-100 tracking-tight">Security</h1>
      <p class="text-xs text-gray-500 dark:text-zinc-400 mt-0.5">
        PINs visitors enter before they see your files. They enter one once per browser, or open a link with
        <span class="font-mono">?pin=</span> and the PIN appended.
      </p>
    </header>

    <p v-if="!saved" class="px-4 pt-4 text-xs text-gray-400 dark:text-zinc-500" data-test="security-loading">Loading…</p>

    <form v-else class="px-4 pt-4 flex flex-col gap-4 max-w-2xl" data-test="security-form" @submit.prevent="save">
      <section class="rounded-xl border border-gray-100 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-4">
        <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100">Account PIN</h2>
        <p class="mt-0.5 text-xs text-gray-500 dark:text-zinc-400">
          Guards every folder you own, the ones you have now and the ones to come.
        </p>

        <div class="mt-3 flex flex-col gap-2" role="radiogroup" aria-label="Account PIN">
          <label
            v-for="option in MODES"
            :key="option.value"
            :class="[
              'flex items-start gap-3 rounded-lg border px-3 py-2.5 cursor-pointer transition-colors',
              mode === option.value
                ? 'border-gray-900 dark:border-zinc-100'
                : 'border-gray-200 dark:border-zinc-800 hover:border-gray-300 dark:hover:border-zinc-700',
            ]"
            :data-test="`pin-mode-${option.value}`"
          >
            <input v-model="mode" type="radio" name="pin-mode" :value="option.value" class="mt-0.5 accent-black dark:accent-white" />
            <span class="min-w-0">
              <span class="block text-xs font-bold text-gray-900 dark:text-zinc-100">
                {{ option.label }}
                <span
                  v-if="saved.mode === option.value"
                  class="ml-1 rounded bg-gray-100 dark:bg-zinc-800 px-1.5 py-0.5 text-[10px] font-semibold text-gray-500 dark:text-zinc-400"
                  data-test="pin-mode-current"
                >Current</span>
              </span>
              <span class="block text-[11px] text-gray-500 dark:text-zinc-400">{{ option.hint }}</span>
            </span>
          </label>
        </div>

        <div v-if="mode === 'static'" class="mt-4">
          <label for="static-pin" class="text-[11px] font-bold text-gray-500 dark:text-zinc-400">
            {{ saved.mode === 'static' ? 'New PIN' : 'PIN' }}
          </label>
          <input
            id="static-pin"
            :value="pin"
            type="password"
            inputmode="numeric"
            autocomplete="new-password"
            maxlength="6"
            :placeholder="saved.mode === 'static' ? 'Leave empty to keep the current PIN' : '6 digits'"
            class="mt-1 block w-full max-w-xs rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-3 py-2 font-mono text-sm tracking-widest text-gray-900 dark:text-zinc-100 placeholder:tracking-normal placeholder:font-sans placeholder:text-xs placeholder:text-gray-400 dark:placeholder:text-zinc-500 focus:outline-none focus:border-gray-400 dark:focus:border-zinc-600"
            data-test="static-pin"
            @input="pin = digits($event.target.value)"
          />
        </div>

        <div v-if="mode === 'totp'" class="mt-4" data-test="totp">
          <template v-if="totp">
            <p class="text-xs text-gray-500 dark:text-zinc-400">
              Scan the code with your authenticator app, then enter the 6 digit code it shows.
            </p>
            <div class="mt-3 flex flex-col sm:flex-row gap-4 sm:items-center">
              <img
                :src="totp.qrCode"
                alt="QR code for your authenticator app"
                class="size-40 shrink-0 rounded-lg border border-gray-100 dark:border-zinc-800 bg-white p-1"
                data-test="totp-qr"
              />
              <div class="min-w-0 flex flex-col gap-3">
                <div>
                  <p class="text-[11px] font-bold text-gray-500 dark:text-zinc-400">Or type in the key</p>
                  <p class="mt-0.5 break-all font-mono text-xs text-gray-900 dark:text-zinc-100" data-test="totp-secret">{{ totp.secret }}</p>
                </div>
                <div>
                  <label for="totp-code" class="text-[11px] font-bold text-gray-500 dark:text-zinc-400">Code</label>
                  <input
                    id="totp-code"
                    :value="code"
                    inputmode="numeric"
                    autocomplete="one-time-code"
                    maxlength="6"
                    placeholder="000000"
                    class="mt-1 block w-32 rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-3 py-2 font-mono text-sm tracking-widest text-gray-900 dark:text-zinc-100 placeholder:text-gray-400 dark:placeholder:text-zinc-500 focus:outline-none focus:border-gray-400 dark:focus:border-zinc-600"
                    data-test="totp-code"
                    @input="code = digits($event.target.value)"
                  />
                </div>
              </div>
            </div>
          </template>
          <div v-else class="flex flex-wrap items-center gap-3">
            <p v-if="saved.mode === 'totp'" class="text-xs text-gray-500 dark:text-zinc-400" data-test="totp-set">
              Your authenticator app is set up.
            </p>
            <button
              type="button"
              :disabled="settingUp"
              class="rounded-lg border border-gray-200 dark:border-zinc-800 bg-white dark:bg-zinc-900 px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-gray-900 dark:text-zinc-100 hover:border-gray-300 dark:hover:border-zinc-600 disabled:opacity-50 transition-colors"
              data-test="totp-setup"
              @click="setUpTotp"
            >
              {{ settingUp ? 'Loading…' : saved.mode === 'totp' ? 'Use another app' : 'Show QR code' }}
            </button>
          </div>
        </div>
      </section>

      <section class="rounded-xl border border-gray-100 dark:border-zinc-800 bg-white dark:bg-zinc-900 p-4">
        <h2 class="text-sm font-bold text-gray-900 dark:text-zinc-100">Agent folders</h2>
        <label class="mt-2 flex items-start gap-3 cursor-pointer" data-test="agent-pins">
          <input v-model="agentPins" type="checkbox" class="mt-0.5 accent-black dark:accent-white" />
          <span class="min-w-0">
            <span class="block text-xs font-bold text-gray-900 dark:text-zinc-100">Give each folder an agent creates a PIN of its own</span>
            <span class="block text-[11px] text-gray-500 dark:text-zinc-400">
              The agent gets the PIN when it creates the folder, to pass on with the link. Only while the account PIN is off.
            </span>
          </span>
        </label>
      </section>

      <div>
        <button
          type="submit"
          :disabled="!canSave"
          class="rounded-lg bg-black dark:bg-white px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-white dark:text-zinc-900 hover:bg-gray-800 dark:hover:bg-zinc-200 disabled:opacity-50 transition-colors"
          data-test="security-save"
        >
          {{ saving ? 'Saving…' : 'Save' }}
        </button>
      </div>
    </form>
  </div>
</template>
