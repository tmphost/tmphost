// The Go server injects the base path the app is served under.
const basePath = (window.__TMPHOST_BASE_PATH__ || '').replace(/\/$/, '')

export const API_BASE_URL = `${basePath}/api/v1`

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
  const res = await fetch(`${API_BASE_URL}/auth/config`)
  if (!res.ok) throw new Error(`Failed to load sign-in options (${res.status})`)
  const body = await res.json()
  return body.providers
}

// providerLoginUrl is the server route that starts an OAuth round trip.
// redirect is an in-app path to land on afterwards.
export function providerLoginUrl(provider, redirect) {
  const url = `${API_BASE_URL}/auth/${encodeURIComponent(provider)}/login`
  return redirect ? `${url}?redirect_url=${encodeURIComponent(redirect)}` : url
}

export async function logout() {
  try {
    await fetch(`${API_BASE_URL}/auth/logout`, { method: 'POST', credentials: 'same-origin' })
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
