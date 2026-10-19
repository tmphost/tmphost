<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed, ref } from 'vue'
import BrandLogo from './BrandLogo.vue'
import { creditsPercent, renewsIn } from '../utils/credits'

const props = defineProps({
  user: { type: Object, default: null },
  collapsed: { type: Boolean, default: false },
  mobileOpen: { type: Boolean, default: false },
})
const emit = defineEmits(['toggle-collapse', 'logout'])

const NAV = [
  {
    section: 'Overview',
    items: [
      { to: '/', label: 'Files', icon: 'M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2V7z' },
      {
        to: '/connect',
        label: 'Connect',
        icon: 'M13.19 8.688a4.5 4.5 0 0 1 1.242 7.244l-4.5 4.5a4.5 4.5 0 0 1-6.364-6.364l1.757-1.757m13.35-.622 1.757-1.757a4.5 4.5 0 0 0-6.364-6.364l-4.5 4.5a4.5 4.5 0 0 0 1.242 7.244',
      },
    ],
  },
]

const isUserMenuOpen = ref(false)

// The mobile drawer always shows labels, whatever the desktop state.
const compact = computed(() => props.collapsed && !props.mobileOpen)
const percent = computed(() => creditsPercent(props.user))
const renewal = computed(() => renewsIn(props.user?.creditsRenewAt))
const displayName = computed(() => props.user?.name || props.user?.email || 'Account')
const initial = computed(() => displayName.value.charAt(0).toUpperCase())

function closeUserMenu() {
  isUserMenuOpen.value = false
}

function logout() {
  closeUserMenu()
  emit('logout')
}
</script>

<template>
  <nav
    :class="[
      mobileOpen ? 'flex w-[280px] bg-white border-r border-gray-200' : 'hidden',
      'md:flex md:relative md:bg-zinc-50 md:border-0 fixed inset-y-0 left-0 z-[100] shrink-0 flex-col h-full text-gray-900 transition-[width] duration-200',
      compact ? 'md:w-16' : 'md:w-64',
    ]"
    data-test="sidebar"
  >
    <div :class="['flex flex-col min-h-0 grow', compact ? 'px-2 py-4' : 'p-4']">
      <!-- Header -->
      <div :class="['flex', compact ? 'flex-col items-center gap-2' : 'items-center gap-1']">
        <router-link to="/" :class="['flex items-center gap-2.5 min-w-0 p-1', compact ? '' : 'grow']">
          <BrandLogo tile class="w-7 h-7" />
          <span v-if="!compact" class="text-sm font-bold truncate" data-test="sidebar-brand">tmphost.io</span>
        </router-link>
        <button
          type="button"
          class="hidden md:inline-flex items-center justify-center size-8 shrink-0 rounded-lg text-gray-400 hover:text-gray-900 hover:bg-gray-100 transition-colors"
          :title="collapsed ? 'Expand sidebar' : 'Collapse sidebar'"
          data-test="sidebar-toggle"
          @click="emit('toggle-collapse')"
        >
          <svg
            :class="['w-4 h-4 transition-transform', collapsed ? 'rotate-180' : '']"
            viewBox="0 0 24 24"
            fill="none"
            stroke="currentColor"
            stroke-width="2"
            stroke-linecap="round"
            stroke-linejoin="round"
          >
            <path d="m15 18-6-6 6-6" />
          </svg>
        </button>
      </div>

      <!-- Navigation -->
      <div class="mt-6 flex-1 min-h-0 overflow-y-auto no-scrollbar space-y-0.5">
        <template v-for="group in NAV" :key="group.section">
          <p v-if="!compact" class="px-2 pb-1 text-[11px] font-medium text-gray-400">{{ group.section }}</p>
          <router-link
            v-for="item in group.items"
            :key="item.to"
            :to="item.to"
            :title="compact ? item.label : undefined"
            :class="[
              'flex items-center gap-2.5 px-2 py-1.5 rounded-lg text-xs transition-colors',
              compact ? 'justify-center' : '',
              $route.path === item.to
                ? 'bg-gray-200/70 text-gray-900 font-semibold'
                : 'text-gray-500 hover:bg-gray-100 hover:text-gray-900',
            ]"
          >
            <svg class="w-4 h-4 shrink-0" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
              <path stroke-linecap="round" stroke-linejoin="round" :d="item.icon" />
            </svg>
            <span v-if="!compact">{{ item.label }}</span>
          </router-link>
        </template>
      </div>

      <!-- Operation credits -->
      <div
        v-if="user && !compact"
        class="mb-3 rounded-xl border border-gray-200 bg-white p-3"
        data-test="sidebar-credits"
      >
        <div class="flex items-baseline justify-between">
          <span class="text-[10px] font-black uppercase tracking-widest text-gray-400">Operations</span>
          <span class="text-xs font-bold tabular-nums text-gray-900">
            {{ user.credits }}<span class="text-gray-400 font-medium"> / {{ user.creditsLimit }}</span>
          </span>
        </div>
        <div class="mt-2 h-1 rounded-full bg-gray-100 overflow-hidden">
          <div class="h-full bg-gray-900 rounded-full" :style="{ width: `${percent}%` }" data-test="credits-bar"></div>
        </div>
        <p v-if="renewal" class="mt-1.5 text-[10px] text-gray-400">{{ renewal }}</p>
      </div>

      <!-- Account -->
      <div v-click-outside="closeUserMenu" class="relative pt-3 border-t border-gray-200">
        <div
          v-if="isUserMenuOpen"
          :class="[
            'absolute z-[110] bg-white border border-gray-200 rounded-xl p-1.5 shadow-sm',
            compact ? 'left-full bottom-0 ml-2 min-w-[200px]' : 'bottom-full left-0 right-0 mb-2',
          ]"
          data-test="user-menu"
        >
          <div class="px-2.5 py-2 border-b border-gray-100 mb-1">
            <p class="text-[10px] font-black uppercase tracking-widest text-gray-400">Account</p>
            <p class="mt-0.5 text-xs font-bold text-gray-900 truncate">{{ displayName }}</p>
            <p v-if="user?.name && user?.email" class="text-[10px] text-gray-400 truncate">{{ user.email }}</p>
          </div>
          <button
            type="button"
            class="w-full flex items-center gap-2.5 px-2.5 py-2 rounded-lg text-xs font-bold text-rose-500 hover:bg-rose-50 transition-colors"
            data-test="logout"
            @click="logout"
          >
            <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
              <path stroke-linecap="round" stroke-linejoin="round" d="M17 16l4-4m0 0l-4-4m4 4H7m6 4v1a3 3 0 01-3 3H6a3 3 0 01-3-3V7a3 3 0 013-3h4a3 3 0 013 3v1" />
            </svg>
            Log out
          </button>
        </div>
        <button
          type="button"
          :class="['flex items-center gap-2.5 w-full py-1.5 rounded-lg transition-colors hover:bg-gray-100', compact ? 'justify-center' : 'px-2']"
          data-test="user-menu-toggle"
          @click="isUserMenuOpen = !isUserMenuOpen"
        >
          <span class="w-8 h-8 shrink-0 rounded-full border border-gray-200 bg-white overflow-hidden flex items-center justify-center text-xs font-bold text-gray-700">
            <img v-if="user?.picture" :src="user.picture" alt="" class="w-full h-full object-cover" referrerpolicy="no-referrer" />
            <span v-else>{{ initial }}</span>
          </span>
          <span v-if="!compact" class="text-sm font-semibold text-gray-700 truncate text-left">{{ displayName }}</span>
        </button>
      </div>
    </div>
  </nav>
</template>
