<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<template>
  <div class="flex flex-col">
    <header class="px-4 pb-4 border-b border-gray-100">
      <h1 class="text-lg font-bold text-gray-900 tracking-tight">Home</h1>
      <p class="text-xs text-gray-500 mt-0.5">Temporary hosting for your projects.</p>
    </header>
    <section class="grow flex items-center justify-center px-4 py-16">
      <div class="w-full max-w-sm text-center rounded-xl border border-gray-100 p-8">
        <p class="text-[10px] font-black uppercase tracking-widest text-gray-400">tmphost.io</p>
        <h2 class="mt-2 text-2xl font-bold text-gray-900 tracking-tight">Share files for a while</h2>
        <p class="mt-2 text-sm text-gray-500">Upload files or a folder and get links that expire on their own.</p>
        <router-link
          to="/files"
          class="mt-6 inline-block rounded-lg bg-black px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-white hover:bg-gray-800 transition-colors"
          data-test="home-files"
        >
          Go to Files
        </router-link>
      </div>
    </section>
  </div>
</template>
