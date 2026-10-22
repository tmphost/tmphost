// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

// The Go server injects the base path the app is served under.
export const BASE_PATH = (window.__TMPHOST_BASE_PATH__ || '').replace(/\/$/, '')

export const API_BASE_URL = `${BASE_PATH}/api/v1`

// ADMIN_BASE_URL is the root-only admin API, apart from the public one.
export const ADMIN_BASE_URL = `${BASE_PATH}/admin/v1`

let userCache = null
let userPromise = null

// fetchUser returns the signed-in user, or null when there is no session.
// The result is cached until clearUserCache() so route changes don't refetch.
export function fetchUser() {
  if (userCache) return Promise.resolve(userCache)
  if (userPromise) return userPromise
  userPromise = (async () => {
    try {
      const res = await fetch(`${API_BASE_URL}/users/me`, { credentials: 'same-origin' })
      if (res.status === 401) return null
      if (!res.ok) throw new Error(`Failed to load user (${res.status})`)
      const body = await res.json()
      userCache = body.user
      return userCache
    } finally {
      userPromise = null
    }
  })()
  return userPromise
}

export function clearUserCache() {
  userCache = null
}

// fetchAuthConfig lists the sign-in providers the server has enabled.
export async function fetchAuthConfig() {
  const res = await fetch(`${BASE_PATH}/auth/config`)
  if (!res.ok) throw new Error(`Failed to load sign-in options (${res.status})`)
  const body = await res.json()
  return body.providers
}

// fetchMcpUrl returns the MCP endpoint's URL when the server serves it on an
// MCP domain of its own, or '' when it lives below the app URL.
export async function fetchMcpUrl() {
  const res = await fetch(`${BASE_PATH}/auth/config`)
  if (!res.ok) throw new Error(`Failed to load the server config (${res.status})`)
  const body = await res.json()
  return body.mcpUrl || ''
}

// providerLoginUrl is the server route that starts an OAuth round trip.
// redirect is an in-app path to land on afterwards.
export function providerLoginUrl(provider, redirect) {
  const url = `${BASE_PATH}/auth/${encodeURIComponent(provider)}/login`
  return redirect ? `${url}?redirect_url=${encodeURIComponent(redirect)}` : url
}

export async function logout() {
  try {
    await fetch(`${BASE_PATH}/auth/logout`, { method: 'POST', credentials: 'same-origin' })
  } finally {
    clearUserCache()
  }
}

// httpError builds an Error from an API error body, keeping the status so
// callers can tell an expired session (401) from other failures.
async function httpError(res, fallback) {
  let message = fallback
  try {
    const body = await res.json()
    if (body && body.error) message = body.error
  } catch {
    // Not JSON; use the fallback.
  }
  const err = new Error(message)
  err.status = res.status
  return err
}

// setupLogin exchanges the root access token for a root session cookie.
export async function setupLogin(token) {
  const res = await fetch(`${API_BASE_URL}/setup/login`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ token }),
  })
  if (!res.ok) throw await httpError(res, `Sign-in failed (${res.status})`)
}

export async function setupLogout() {
  await fetch(`${API_BASE_URL}/setup/logout`, { method: 'POST', credentials: 'same-origin' })
}

// fetchSettings returns { settings, secretsSet, restartRequired }, or null
// without a root session.
export async function fetchSettings() {
  const res = await fetch(`${API_BASE_URL}/setup/settings`, { credentials: 'same-origin' })
  if (res.status === 401) return null
  if (!res.ok) throw await httpError(res, `Failed to load settings (${res.status})`)
  return res.json()
}

// saveSettings replaces the settings. Blank secrets keep their stored value.
export async function saveSettings(settings) {
  const res = await fetch(`${API_BASE_URL}/setup/settings`, {
    method: 'PUT',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ settings }),
  })
  if (!res.ok) throw await httpError(res, `Failed to save settings (${res.status})`)
  return res.json()
}

// grantCredits gives credits that expire to one user (by email) or to
// every user (all), as root. It returns { users, expiresAt }.
export async function grantCredits({ credits, email, all }) {
  const res = await fetch(`${ADMIN_BASE_URL}/credits/grants`, {
    method: 'POST',
    credentials: 'same-origin',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(all ? { credits, all: true } : { credits, email }),
  })
  if (!res.ok) throw await httpError(res, `Failed to grant credits (${res.status})`)
  return res.json()
}

// request sends a JSON request for the signed-in user (or root, with base
// ADMIN_BASE_URL) and returns the parsed body, or null for 204. Failures
// throw an Error carrying the HTTP status.
async function request(method, path, { body, fallback, base = API_BASE_URL } = {}) {
  const init = { method, credentials: 'same-origin' }
  if (body !== undefined) {
    init.headers = { 'Content-Type': 'application/json' }
    init.body = JSON.stringify(body)
  }
  const res = await fetch(`${base}${path}`, init)
  if (!res.ok) throw await httpError(res, `${fallback} (${res.status})`)
  return res.status === 204 ? null : res.json()
}

// filePath encodes each segment of a folder-relative path, keeping the slashes.
export function filePath(path) {
  return path.split('/').map(encodeURIComponent).join('/')
}

// listCreditActivities returns a page of the user's credit history, newest
// first, as { activities, next }; pass next back as before for the next page.
export function listCreditActivities(before = '') {
  const query = new URLSearchParams({ limit: String(CREDIT_PAGE_SIZE) })
  if (before) query.set('before', before)
  return request('GET', `/users/me/credits/activities?${query}`, { fallback: 'Failed to load credit history' })
}

// CREDIT_PAGE_SIZE is the largest page the server returns.
export const CREDIT_PAGE_SIZE = 200

// fetchAdminStats returns root's stats over every account: { users, credits,
// used, signups, folders, days: [{ date, used, signups }] }.
export async function fetchAdminStats() {
  const { stats } = await request('GET', '/stats', { fallback: 'Failed to load stats', base: ADMIN_BASE_URL })
  return stats
}

// listAdminUsers returns a page of users, newest first, as { users, next }:
// q searches emails and names, or names one user ID; pass next back as
// before for the next page.
export function listAdminUsers({ q = '', before = '' } = {}) {
  const query = new URLSearchParams({ limit: String(ADMIN_USERS_PAGE_SIZE) })
  if (q) query.set('q', q)
  if (before) query.set('before', before)
  return request('GET', `/users?${query}`, { fallback: 'Failed to load users', base: ADMIN_BASE_URL })
}

// ADMIN_USERS_PAGE_SIZE is how many users a page of the admin list shows.
export const ADMIN_USERS_PAGE_SIZE = 50

// fetchAdminUser returns one user, as root.
export async function fetchAdminUser(id) {
  const { user } = await request('GET', `/users/${encodeURIComponent(id)}`, { fallback: 'Failed to load user', base: ADMIN_BASE_URL })
  return user
}

// listAdminUserCreditActivities returns a page of one user's credit history,
// as root, like listCreditActivities.
export function listAdminUserCreditActivities(id, before = '') {
  const query = new URLSearchParams({ limit: String(CREDIT_PAGE_SIZE) })
  if (before) query.set('before', before)
  return request('GET', `/users/${encodeURIComponent(id)}/credits/activities?${query}`, {
    fallback: 'Failed to load credit history',
    base: ADMIN_BASE_URL,
  })
}

// fetchLimits returns the upload limits and billing block size.
export function fetchLimits() {
  return request('GET', '/limits', { fallback: 'Failed to load limits' })
}

export async function listFolders() {
  const body = await request('GET', '/folders', { fallback: 'Failed to load folders' })
  return body.folders
}

// createFolder charges nothing by itself; credits are spent per uploaded file.
export async function createFolder(ttlMinutes) {
  const body = await request('POST', '/folders', { body: { ttlMinutes }, fallback: 'Failed to create folder' })
  return body.folder
}

// fetchFolder returns { folder, files }.
export function fetchFolder(id) {
  return request('GET', `/folders/${encodeURIComponent(id)}`, { fallback: 'Failed to load folder' })
}

// renameFolder sets a folder's title; an empty title clears it. It returns
// the updated folder.
export async function renameFolder(id, title) {
  const body = await request('PATCH', `/folders/${encodeURIComponent(id)}`, {
    body: { title },
    fallback: 'Failed to rename folder',
  })
  return body.folder
}

// extendFolder makes a folder expire minutes later. It returns
// { folder, creditsSpent }: each file costs as an upload would for the
// added time.
export function extendFolder(id, minutes) {
  return request('POST', `/folders/${encodeURIComponent(id)}/extend`, {
    body: { minutes },
    fallback: 'Failed to extend folder',
  })
}

// createFolderPin gives a folder a new PIN of its own and returns the
// folder, with the PIN in folder.pin: the only time it is shown.
export async function createFolderPin(id) {
  const body = await request('POST', `/folders/${encodeURIComponent(id)}/pin`, { fallback: 'Failed to make a PIN' })
  return body.folder
}

// deleteFolderPin removes a folder's own PIN and returns the folder.
export async function deleteFolderPin(id) {
  const body = await request('DELETE', `/folders/${encodeURIComponent(id)}/pin`, { fallback: 'Failed to remove the PIN' })
  return body.folder
}

// fetchAccountPin returns { mode: 'off' | 'static' | 'totp', agentPins }.
export function fetchAccountPin() {
  return request('GET', '/users/me/pin', { fallback: 'Failed to load PIN settings' })
}

// newTotp returns a new authenticator secret to scan, { secret, url, qrCode };
// saveAccountPin keeps it once it gets the secret back with a code.
export function newTotp() {
  return request('POST', '/users/me/pin/totp', { fallback: 'Failed to set up the authenticator' })
}

// saveAccountPin changes the account PIN: { mode, pin?, secret?, code?,
// agentPins }. It returns the new { mode, agentPins }.
export function saveAccountPin(settings) {
  return request('PUT', '/users/me/pin', { body: settings, fallback: 'Failed to save PIN settings' })
}

export async function deleteFolder(id) {
  await request('DELETE', `/folders/${encodeURIComponent(id)}`, { fallback: 'Failed to delete folder' })
}

// uploadFile stores one file under path and returns { file, creditsSpent }.
// It uses XMLHttpRequest because fetch can't report upload progress:
// onProgress gets the bytes sent so far.
export function uploadFile(folderId, path, file, onProgress = () => {}) {
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest()
    xhr.open('PUT', `${API_BASE_URL}/folders/${encodeURIComponent(folderId)}/files/${filePath(path)}`)
    xhr.upload.onprogress = (e) => onProgress(e.loaded)
    xhr.onload = () => {
      let body = null
      try {
        body = JSON.parse(xhr.responseText)
      } catch {
        // Not JSON; use the fallback.
      }
      if (xhr.status >= 200 && xhr.status < 300) {
        onProgress(file.size)
        resolve(body)
        return
      }
      const err = new Error(body?.error || `Failed to upload ${path} (${xhr.status})`)
      err.status = xhr.status
      reject(err)
    }
    xhr.onerror = () => reject(new Error(`Failed to upload ${path}`))
    xhr.send(file)
  })
}

export async function deleteFile(folderId, path) {
  await request('DELETE', `/folders/${encodeURIComponent(folderId)}/files/${filePath(path)}`, {
    fallback: 'Failed to delete file',
  })
}
