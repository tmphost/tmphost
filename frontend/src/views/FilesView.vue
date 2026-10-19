<!--
  Copyright 2026 Contextual, Inc. https://tmphost.io
  This notice may not be modified or removed.
  SPDX-License-Identifier: AGPL-3.0-only
-->

<script setup>
import { computed, inject, onMounted, onUnmounted, ref } from 'vue'
import DeleteModal from '../components/DeleteModal.vue'
import { createFolder, deleteFile, deleteFolder, fetchFolder, fetchLimits, listFolders, uploadFile } from '../api'
import { useClipboard } from '../composables/useClipboard'
import { useToasts } from '../composables/useToasts'
import { expiresIn, formatBytes, formatMinutes, relativePath, ttlOptions, uploadCost } from '../utils/files'

const TICK_MS = 30 * 1000

const PRIMARY =
  'rounded-lg bg-black px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-white hover:bg-gray-800 disabled:opacity-50 transition-colors'
const SECONDARY =
  'rounded-lg border border-gray-200 bg-white px-4 py-2.5 text-[11px] font-black uppercase tracking-widest text-gray-900 hover:border-gray-300 disabled:opacity-50 transition-colors'
const ICON_BUTTON =
  'inline-flex items-center justify-center size-7 shrink-0 rounded-lg text-gray-400 hover:text-gray-900 hover:bg-gray-100 transition-colors'
const ICONS = {
  copy: 'M8 8V5a1 1 0 0 1 1-1h10a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1h-3M5 8h10a1 1 0 0 1 1 1v10a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V9a1 1 0 0 1 1-1z',
  open: 'M14 4h6v6m0-6L10 14M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5',
  trash: 'M4 7h16M10 11v6m4-6v6M6 7l1 12a1 1 0 0 0 1 1h8a1 1 0 0 0 1-1l1-12M9 7V4h6v3',
}

const { notifyError, notifySuccess } = useToasts()
const { copy } = useClipboard()
const currentUser = inject('currentUser', { user: ref(null), refreshUser: async () => {} })

const limits = ref(null)
const folders = ref([])
const loading = ref(true)
const now = ref(Date.now())
let timer = null

// Upload form
const selected = ref([])
const ttl = ref(30)
const dragging = ref(false)
const uploading = ref(false)
const progress = ref({ done: 0, total: 0 })

// Expanded folders: id -> { loading, files }
const expanded = ref({})

// The pending deletion: { folder } or { folder, file }.
const pendingDelete = ref(null)
const deleting = ref(false)

const ttlChoices = computed(() => (limits.value ? ttlOptions(limits.value.maxTtlMinutes) : []))
// The slider steps through ttlChoices by index; ttl keeps the minutes. It
// binds :value rather than v-model so Vue sets max before the value.
const ttlIndex = computed(() => Math.max(0, ttlChoices.value.indexOf(ttl.value)))
const sortedFolders = computed(() =>
  [...folders.value].sort((a, b) => Date.parse(b.createdAt) - Date.parse(a.createdAt)),
)
const isOversized = (file) => Boolean(limits.value) && file.size > limits.value.maxUploadBytes
const oversized = computed(() => selected.value.filter((item) => isOversized(item.file)))
const cost = computed(() =>
  limits.value
    ? uploadCost(
        selected.value.map((item) => item.file.size),
        ttl.value,
        limits.value.billingBlockMinutes,
        limits.value.billingUnitBytes,
      )
    : 0,
)
const credits = computed(() => currentUser.user.value?.credits)
const notEnoughCredits = computed(() => credits.value !== undefined && cost.value > credits.value)
const canUpload = computed(
  () => Boolean(limits.value) && selected.value.length > 0 && oversized.value.length === 0 && !uploading.value,
)

const deleteTitle = computed(() => (pendingDelete.value?.file ? 'Delete file?' : 'Delete folder?'))
const deleteMessage = computed(() => {
  const target = pendingDelete.value
  if (!target) return ''
  if (target.file) return `${target.file.path} is removed from folder ${target.folder.id}. Its link stops working.`
  return `Folder ${target.folder.id} and all of its files are removed. Their links stop working.`
})

async function loadLimits() {
  try {
    limits.value = await fetchLimits()
    const choices = ttlChoices.value
    ttl.value = choices.includes(limits.value.defaultTtlMinutes) ? limits.value.defaultTtlMinutes : choices[0]
  } catch (err) {
    notifyError(err.message)
  }
}

async function loadFolders() {
  try {
    folders.value = await listFolders()
  } catch (err) {
    notifyError(err.message)
  } finally {
    loading.value = false
  }
}

function addFiles(fileList) {
  const byPath = new Map(selected.value.map((item) => [item.path, item]))
  for (const file of fileList) {
    const path = relativePath(file)
    byPath.set(path, { path, file })
  }
  selected.value = [...byPath.values()]
}

function onPick(event) {
  addFiles(event.target.files)
  // Allow picking the same files again after removing them.
  event.target.value = ''
}

function onDrop(event) {
  dragging.value = false
  addFiles(event.dataTransfer.files)
}

function removeSelected(path) {
  selected.value = selected.value.filter((item) => item.path !== path)
}

async function upload() {
  const items = selected.value
  uploading.value = true
  progress.value = { done: 0, total: items.length }
  try {
    const folder = await createFolder(ttl.value)
    for (const item of items) {
      await uploadFile(folder.id, item.path, item.file)
      progress.value = { done: progress.value.done + 1, total: items.length }
    }
    selected.value = []
    notifySuccess(items.length === 1 ? 'Uploaded 1 file' : `Uploaded ${items.length} files`)
  } catch (err) {
    notifyError(err.message)
  } finally {
    uploading.value = false
    // A failure part-way still created a folder and spent credits.
    await Promise.all([loadFolders(), currentUser.refreshUser()])
  }
}

// collapse forgets a folder's expanded state.
function collapse(id) {
  const rest = { ...expanded.value }
  delete rest[id]
  expanded.value = rest
}

async function toggleFolder(folder) {
  if (expanded.value[folder.id]) {
    collapse(folder.id)
    return
  }
  expanded.value = { ...expanded.value, [folder.id]: { loading: true, files: [] } }
  try {
    const { files } = await fetchFolder(folder.id)
    expanded.value = { ...expanded.value, [folder.id]: { loading: false, files } }
  } catch (err) {
    notifyError(err.message)
    collapse(folder.id)
  }
}

async function confirmDelete() {
  const { folder, file } = pendingDelete.value
  deleting.value = true
  try {
    if (file) {
      await deleteFile(folder.id, file.path)
      const entry = expanded.value[folder.id]
      entry.files = entry.files.filter((f) => f.path !== file.path)
      notifySuccess('File deleted')
    } else {
      await deleteFolder(folder.id)
      folders.value = folders.value.filter((f) => f.id !== folder.id)
      notifySuccess('Folder deleted')
    }
  } catch (err) {
    notifyError(err.message)
  } finally {
    deleting.value = false
    pendingDelete.value = null
  }
}

onMounted(() => {
  loadLimits()
  loadFolders()
  timer = setInterval(() => {
    now.value = Date.now()
  }, TICK_MS)
})

onUnmounted(() => clearInterval(timer))
</script>

<template>
  <div class="flex flex-col">
    <header class="px-4 pb-4 border-b border-gray-100">
      <h1 class="text-lg font-bold text-gray-900 tracking-tight">Files</h1>
      <p class="text-xs text-gray-500 mt-0.5">Upload files to a temporary folder and share the links until it expires.</p>
    </header>

    <!-- Upload -->
    <section class="px-4 pt-4">
      <div
        :class="[
          'rounded-xl border p-5 transition-colors',
          dragging ? 'border-gray-400 bg-zinc-50' : 'border-gray-100 hover:border-gray-200',
        ]"
        data-test="upload-card"
        @dragover.prevent="dragging = true"
        @dragleave="dragging = false"
        @drop.prevent="onDrop"
      >
        <div class="flex flex-wrap items-end gap-3">
          <div class="grow min-w-0">
            <h2 class="text-sm font-bold text-gray-900">New folder</h2>
            <p class="text-xs text-gray-500">Choose files or a whole folder, or drop files here.</p>
          </div>
          <label :class="[SECONDARY, 'cursor-pointer']">
            Choose files
            <input type="file" multiple class="sr-only" data-test="pick-files" @change="onPick" />
          </label>
          <label :class="[SECONDARY, 'cursor-pointer']">
            Choose folder
            <input type="file" webkitdirectory multiple class="sr-only" data-test="pick-folder" @change="onPick" />
          </label>
          <div class="w-48">
            <div class="flex items-baseline justify-between mb-1">
              <label for="upload-ttl" class="text-[11px] font-bold text-gray-500">Expires after</label>
              <span class="text-xs font-bold tabular-nums text-gray-900" data-test="upload-ttl-value">
                {{ formatMinutes(ttl) }}
              </span>
            </div>
            <input
              id="upload-ttl"
              type="range"
              min="0"
              :max="Math.max(ttlChoices.length - 1, 0)"
              step="1"
              :value="ttlIndex"
              :disabled="!limits"
              :aria-valuetext="formatMinutes(ttl)"
              class="w-full cursor-pointer accent-black focus:outline-none focus-visible:outline-2 focus-visible:outline-offset-4 focus-visible:outline-gray-300 disabled:cursor-not-allowed"
              data-test="upload-ttl"
              @input="ttl = ttlChoices[$event.target.valueAsNumber]"
            />
          </div>
        </div>

        <div v-if="selected.length" class="mt-4 border-t border-gray-100 pt-4" data-test="upload-summary">
          <ul class="max-h-48 overflow-y-auto no-scrollbar divide-y divide-gray-50">
            <li
              v-for="item in selected"
              :key="item.path"
              class="flex items-center gap-3 py-1.5 text-xs"
              :data-test="`selected-${item.path}`"
            >
              <span class="grow min-w-0 truncate font-mono text-gray-900">{{ item.path }}</span>
              <span
                :class="['tabular-nums', isOversized(item.file) ? 'font-bold text-rose-600' : 'text-gray-400']"
              >
                {{ formatBytes(item.file.size) }}
              </span>
              <button
                type="button"
                :class="ICON_BUTTON"
                :title="`Remove ${item.path}`"
                data-test="selected-remove"
                @click="removeSelected(item.path)"
              >
                <svg class="w-3.5 h-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="2">
                  <path stroke-linecap="round" stroke-linejoin="round" d="M6 18 18 6M6 6l12 12" />
                </svg>
              </button>
            </li>
          </ul>

          <p v-if="oversized.length" role="alert" class="mt-3 text-xs font-medium text-rose-600" data-test="upload-oversized">
            {{ oversized.length === 1 ? '1 file is' : `${oversized.length} files are` }} over the
            {{ formatBytes(limits.maxUploadBytes) }} limit. Remove {{ oversized.length === 1 ? 'it' : 'them' }} to upload.
          </p>
          <p v-if="notEnoughCredits" class="mt-3 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2 text-xs font-medium text-amber-800" data-test="upload-credits-warning">
            This upload costs {{ cost }} credits but you have {{ credits }} left.
          </p>

          <div class="mt-4 flex flex-wrap items-center gap-3">
            <p class="grow text-xs text-gray-500" data-test="upload-cost">
              {{ selected.length === 1 ? '1 file' : `${selected.length} files` }} ·
              <span class="font-bold text-gray-900">{{ cost }} credits</span>
            </p>
            <button type="button" :disabled="uploading" :class="SECONDARY" data-test="upload-clear" @click="selected = []">
              Clear
            </button>
            <button type="button" :disabled="!canUpload" :class="PRIMARY" data-test="upload-start" @click="upload">
              {{ uploading ? `Uploading ${progress.done} / ${progress.total}` : 'Upload' }}
            </button>
          </div>
        </div>
      </div>
    </section>

    <!-- Folders -->
    <section class="px-4 pt-6">
      <h2 class="text-[10px] font-black uppercase tracking-widest text-gray-400 mb-2">Folders</h2>

      <div v-if="loading" class="flex justify-center py-12" data-test="folders-loading">
        <svg class="animate-spin h-5 w-5 text-gray-400" fill="none" viewBox="0 0 24 24">
          <circle class="opacity-25" cx="12" cy="12" r="10" stroke="currentColor" stroke-width="4" />
          <path class="opacity-75" fill="currentColor" d="M4 12a8 8 0 018-8V0C5.373 0 0 5.373 0 12h4z" />
        </svg>
      </div>

      <div v-else-if="!folders.length" class="rounded-xl border border-gray-100 p-8 text-center" data-test="folders-empty">
        <p class="text-sm font-bold text-gray-900">No folders yet</p>
        <p class="mt-1 text-xs text-gray-500">Uploaded files show up here until they expire.</p>
      </div>

      <ul v-else class="space-y-2">
        <li
          v-for="folder in sortedFolders"
          :key="folder.id"
          class="rounded-xl border border-gray-100 hover:border-gray-200 transition-colors"
          :data-test="`folder-${folder.id}`"
        >
          <div class="flex items-center gap-2 px-3 py-2">
            <button
              type="button"
              class="flex items-center gap-2 grow min-w-0 text-left"
              :aria-expanded="Boolean(expanded[folder.id])"
              data-test="folder-toggle"
              @click="toggleFolder(folder)"
            >
              <svg
                :class="['w-3.5 h-3.5 shrink-0 text-gray-400 transition-transform', expanded[folder.id] ? 'rotate-90' : '']"
                fill="none"
                viewBox="0 0 24 24"
                stroke="currentColor"
                stroke-width="2"
              >
                <path stroke-linecap="round" stroke-linejoin="round" d="m9 18 6-6-6-6" />
              </svg>
              <span class="truncate font-mono text-xs font-bold text-gray-900">{{ folder.id }}</span>
            </button>
            <span class="shrink-0 text-[11px] tabular-nums text-gray-400" :title="folder.expiresAt" data-test="folder-expires">
              {{ expiresIn(folder.expiresAt, now) }}
            </span>
            <button type="button" :class="ICON_BUTTON" title="Copy folder URL" data-test="folder-copy" @click="copy(folder.url, 'Folder URL copied')">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                <path stroke-linecap="round" stroke-linejoin="round" :d="ICONS.copy" />
              </svg>
            </button>
            <a :href="folder.url" target="_blank" rel="noopener" :class="ICON_BUTTON" title="Open folder" data-test="folder-open">
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                <path stroke-linecap="round" stroke-linejoin="round" :d="ICONS.open" />
              </svg>
            </a>
            <button
              type="button"
              :class="[ICON_BUTTON, 'hover:text-rose-600 hover:bg-rose-50']"
              title="Delete folder"
              data-test="folder-delete"
              @click="pendingDelete = { folder }"
            >
              <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                <path stroke-linecap="round" stroke-linejoin="round" :d="ICONS.trash" />
              </svg>
            </button>
          </div>

          <div v-if="expanded[folder.id]" class="border-t border-gray-100 px-3 py-2" data-test="folder-files">
            <p v-if="expanded[folder.id].loading" class="py-1 text-xs text-gray-400">Loading files…</p>
            <p v-else-if="!expanded[folder.id].files.length" class="py-1 text-xs text-gray-400">This folder is empty.</p>
            <ul v-else class="divide-y divide-gray-50">
              <li
                v-for="file in expanded[folder.id].files"
                :key="file.path"
                class="flex items-center gap-2 py-1 pl-5"
                :data-test="`file-${file.path}`"
              >
                <span class="grow min-w-0 truncate font-mono text-xs text-gray-900">{{ file.path }}</span>
                <span class="shrink-0 text-[11px] tabular-nums text-gray-400">{{ formatBytes(file.size) }}</span>
                <button type="button" :class="ICON_BUTTON" title="Copy file URL" data-test="file-copy" @click="copy(file.url, 'File URL copied')">
                  <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                    <path stroke-linecap="round" stroke-linejoin="round" :d="ICONS.copy" />
                  </svg>
                </button>
                <a :href="file.url" target="_blank" rel="noopener" :class="ICON_BUTTON" title="Open file" data-test="file-open">
                  <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                    <path stroke-linecap="round" stroke-linejoin="round" :d="ICONS.open" />
                  </svg>
                </a>
                <button
                  type="button"
                  :class="[ICON_BUTTON, 'hover:text-rose-600 hover:bg-rose-50']"
                  title="Delete file"
                  data-test="file-delete"
                  @click="pendingDelete = { folder, file }"
                >
                  <svg class="w-4 h-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" stroke-width="1.5">
                    <path stroke-linecap="round" stroke-linejoin="round" :d="ICONS.trash" />
                  </svg>
                </button>
              </li>
            </ul>
          </div>
        </li>
      </ul>
    </section>

    <DeleteModal
      :open="Boolean(pendingDelete)"
      :title="deleteTitle"
      :message="deleteMessage"
      :busy="deleting"
      @confirm="confirmDelete"
      @cancel="pendingDelete = null"
    />
  </div>
</template>
