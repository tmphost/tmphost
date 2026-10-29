// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The rules a Cursor Plugin must pass before it is submitted to the Cursor
 * Marketplace, from the manifest, component and submission checklist sections
 * of https://cursor.com/docs/reference/plugins. Cursor has no validator to
 * run, so this is it.
 *
 * Everything here works on an in-memory snapshot of the plugin, so it is
 * tested without a disk; `validate-cursor-plugin.mjs` reads the real one.
 *
 * @typedef {{ files: Record<string, Buffer>, links?: string[] }} Package
 *   Every regular file by its `/`-separated path inside the plugin, and the
 *   paths of any symbolic links.
 * @typedef {{ code: string, message: string }} Finding
 */

import { frontMatter } from './codex-plugin.mjs'

export const SERVER = 'tmphost'

const MANIFEST = '.cursor-plugin/plugin.json'
const MANIFEST_KEYS = [
  'name', 'displayName', 'version', 'description', 'author', 'homepage', 'repository', 'license',
  'keywords', 'logo', 'category', 'tags', 'rules', 'agents', 'skills', 'commands', 'hooks', 'mcpServers',
]
/** Manifest fields naming files or directories in the plugin. */
const PATH_KEYS = ['logo', 'rules', 'agents', 'skills', 'commands', 'mcpServers']
/** Components this validator checks; anything else must be added here first. */
const COMPONENTS = {
  rules: { dir: 'rules', exts: ['.md', '.mdc', '.markdown'] },
  skills: { dir: 'skills' },
  commands: { dir: 'commands', exts: ['.md', '.mdc', '.markdown', '.txt'] },
}

// Cursor's plugin identifier: lowercase alphanumerics, hyphens and periods,
// starting and ending with an alphanumeric.
const PLUGIN_NAME = /^[a-z0-9](?:[a-z0-9.-]*[a-z0-9])?$/
// Kebab-case, for skills, commands and the marketplace.
const KEBAB = /^[a-z0-9]+(?:-[a-z0-9]+)*$/
const SEMVER = /^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?$/

const isObject = (v) => typeof v === 'object' && v !== null && !Array.isArray(v)
const nonEmpty = (v) => typeof v === 'string' && v.trim() !== ''

/** Collects findings, so each rule reads as one line. */
class Findings {
  errors = []
  /** Fails `code` unless `ok`, and returns `ok`. */
  check(ok, code, message) {
    if (!ok) this.errors.push({ code, message })
    return ok
  }
}

/** A relative path inside the plugin, without `./`, or null when it escapes. */
export function inside(value) {
  if (typeof value !== 'string' || value === '' || value.startsWith('/') || /^[a-z]+:/i.test(value)) return null
  const segs = value.replace(/^\.\//, '').split('/').filter((s) => s !== '' && s !== '.')
  return segs.includes('..') ? null : segs.join('/')
}

function isHTTPS(value) {
  try {
    return new URL(value).protocol === 'https:'
  } catch {
    return false
  }
}

/** Checks the manifest's own fields. Returns it, or null when unusable. */
function manifest(f, pkg) {
  const buf = pkg.files[MANIFEST]
  if (!f.check(buf !== undefined, 'manifest_missing', `${MANIFEST} is missing`)) return null
  let m
  try {
    m = JSON.parse(buf.toString('utf8'))
  } catch (err) {
    f.check(false, 'manifest_invalid_json', `${MANIFEST} is not JSON: ${err.message}`)
    return null
  }
  if (!f.check(isObject(m), 'manifest_wrong_type', `${MANIFEST} must hold an object`)) return null

  for (const key of Object.keys(m)) f.check(MANIFEST_KEYS.includes(key), 'manifest_unknown_field', `${key} is not a field Cursor's manifest defines`)
  f.check(typeof m.name === 'string' && PLUGIN_NAME.test(m.name), 'name_format', 'name must be lowercase alphanumerics, hyphens and periods, starting and ending with an alphanumeric')
  f.check(nonEmpty(m.displayName), 'display_name_missing', 'displayName must name the plugin in the marketplace')
  f.check(typeof m.version === 'string' && SEMVER.test(m.version), 'version_format', 'version must be a semantic version such as 1.0.0')
  f.check(nonEmpty(m.description) && m.description.length >= 20, 'description_missing', 'description must explain the plugin\'s purpose')
  f.check(isObject(m.author) && nonEmpty(m.author.name), 'author_missing', 'author.name is required')
  for (const key of ['homepage', 'repository']) f.check(isHTTPS(m[key]), `${key}_not_https`, `${key} must be an HTTPS URL`)
  f.check(nonEmpty(m.license), 'license_missing', 'license must name the license')
  f.check(Array.isArray(m.keywords) && m.keywords.length >= 3 && m.keywords.every(nonEmpty), 'keywords_missing', 'keywords must list at least three search terms')

  for (const key of PATH_KEYS) {
    if (m[key] === undefined) continue
    const path = inside(m[key])
    if (!f.check(path !== null, 'path_unsafe', `${key} must be a relative path inside the plugin, without ..`)) continue
    const exists = path === '' || Object.keys(pkg.files).some((p) => p === path || p.startsWith(`${path}/`))
    f.check(exists, 'path_missing', `${key} names ${m[key]}, which is not in the plugin`)
  }
  f.check(m.logo !== undefined, 'logo_missing', 'logo must name the listing logo, committed to the repository')
  f.check(m.hooks === undefined && m.agents === undefined, 'component_unchecked', 'hooks and agents aren\'t checked by this validator; extend it before adding them')
  return m
}

/**
 * Checks mcp.json: exactly one server, named `tmphost`, reached by its URL
 * alone so Cursor signs in through OAuth discovery. Returns the URL.
 */
function mcp(f, pkg, m) {
  const path = inside(m.mcpServers ?? 'mcp.json')
  const buf = pkg.files[path]
  if (!f.check(buf !== undefined, 'mcp_missing', `${path} is missing`)) return undefined
  let config
  try {
    config = JSON.parse(buf.toString('utf8'))
  } catch (err) {
    f.check(false, 'mcp_invalid_json', `${path} is not JSON: ${err.message}`)
    return undefined
  }
  const servers = isObject(config) && isObject(config.mcpServers) ? config.mcpServers : {}
  if (!f.check(Object.keys(servers).join() === SERVER, 'mcp_servers', `${path} must define exactly one server, ${SERVER}`)) return undefined
  const server = servers[SERVER]
  f.check(isObject(server) && Object.keys(server).join() === 'url', 'mcp_not_oauth', `${path}: the ${SERVER} server must hold only its url, so Cursor signs in with OAuth; no headers, tokens or variables`)
  if (!f.check(isHTTPS(server?.url), 'mcp_url_not_https', `${path}: the ${SERVER} server url must be HTTPS`)) return undefined
  return server.url
}

/** The component files under `dir` with one of `exts`, or SKILL.md files. */
function componentFiles(pkg, { dir, exts }) {
  return Object.keys(pkg.files).filter((p) => {
    if (!p.startsWith(`${dir}/`)) return false
    return exts ? exts.some((e) => p.endsWith(e)) : p.split('/').length === 3 && p.endsWith('/SKILL.md')
  })
}

/** Checks every rule, skill and command, and that their names are unique. */
function components(f, pkg) {
  const names = new Map()
  for (const [kind, where] of Object.entries(COMPONENTS)) {
    const files = componentFiles(pkg, where)
    f.check(files.length > 0, `${kind}_missing`, `${where.dir}/ holds no ${kind}`)
    for (const file of files) {
      const fm = frontMatter(pkg.files[file].toString('utf8'))
      if (!f.check(!fm.error, 'frontmatter_invalid', `${file} must start with --- front matter of plain key: value lines, closed by ---`)) continue
      f.check(nonEmpty(fm.fields.description), 'description_missing', `${file} needs a description`)
      f.check(fm.body.trim() !== '', 'body_empty', `${file} needs instructions after the front matter`)
      if (kind === 'rules') {
        f.check(['true', 'false'].includes(fm.fields.alwaysApply), 'rule_always_apply', `${file}: alwaysApply must be true or false`)
        continue
      }
      // A skill is named after its directory, a command after its file.
      const base = kind === 'skills' ? file.split('/')[1] : file.slice(file.lastIndexOf('/') + 1).replace(/\.[^.]+$/, '')
      if (!f.check(KEBAB.test(fm.fields.name ?? '') && fm.fields.name === base, 'name_mismatch', `${file}: name must be kebab-case and match ${base}`)) continue
      f.check(!names.has(fm.fields.name), 'name_duplicate', `${file}: name ${fm.fields.name} is also ${names.get(fm.fields.name)}`)
      names.set(fm.fields.name, file)
    }
  }
}

/**
 * Checks a Cursor Plugin package. Returns the findings, its manifest and its
 * MCP server's URL when they parse.
 *
 * @param {Package} pkg
 * @returns {{ errors: Finding[], manifest?: object, url?: string }}
 */
export function validatePlugin(pkg) {
  const f = new Findings()
  for (const link of pkg.links ?? []) f.check(false, 'symlink', `${link} is a symbolic link; Cursor skips links that leave its plugin folder`)
  const m = manifest(f, pkg)
  if (!m) return { errors: f.errors }
  const url = mcp(f, pkg, m)
  components(f, pkg)
  return { errors: f.errors, manifest: m, url }
}

/**
 * Checks the repository's .cursor-plugin/marketplace.json: its own fields,
 * unique plugin names, and one entry for `name` whose source is `path` and
 * holds a plugin (`isPlugin(path)`).
 */
export function validateMarketplace(market, { name, path, isPlugin }) {
  const f = new Findings()
  if (!f.check(isObject(market), 'marketplace_wrong_type', 'the marketplace must be an object')) return f.errors
  f.check(typeof market.name === 'string' && KEBAB.test(market.name), 'marketplace_name', 'name must be kebab-case')
  f.check(isObject(market.owner) && nonEmpty(market.owner.name), 'marketplace_owner', 'owner.name is required')
  if (!f.check(Array.isArray(market.plugins), 'marketplace_plugins', 'plugins must be a list')) return f.errors
  const seen = new Set()
  for (const entry of market.plugins) {
    f.check(!seen.has(entry?.name), 'marketplace_duplicate', `plugin ${entry?.name} is listed twice`)
    seen.add(entry?.name)
  }
  const entry = market.plugins.find((p) => p?.name === name)
  if (!f.check(entry !== undefined, 'marketplace_entry_missing', `plugins has no entry named ${name}`)) return f.errors
  const source = inside(isObject(entry.source) ? entry.source.path : entry.source)
  if (f.check(source === path, 'marketplace_source', `${name}'s source must be ./${path}`)) {
    f.check(isPlugin(source), 'marketplace_source_not_plugin', `./${path} has no .cursor-plugin/plugin.json`)
  }
  return f.errors
}

/**
 * Probes the live MCP server the way Cursor connects: an unauthenticated
 * request must get a Bearer challenge naming protected-resource metadata,
 * whose authorization server offers registration and PKCE.
 *
 * @param {string} url the MCP server's URL
 * @param {typeof fetch} get
 * @returns {Promise<Finding[]>}
 */
export async function checkOAuth(url, get) {
  const f = new Findings()
  const res = await get(url, {
    method: 'POST',
    headers: { accept: 'application/json, text/event-stream', 'content-type': 'application/json' },
    body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'initialize', params: { protocolVersion: '2025-11-25', capabilities: {}, clientInfo: { name: 'cursor-plugin-check', version: '1.0.0' } } }),
  })
  if (!f.check(res.status === 401, 'oauth_no_challenge', `an unauthenticated initialize got ${res.status}, not 401`)) return f.errors
  const metadata = /resource_metadata="([^"]+)"/.exec(res.headers.get('www-authenticate') ?? '')?.[1]
  if (!f.check(metadata !== undefined, 'oauth_no_metadata', 'the 401 has no Bearer resource_metadata')) return f.errors
  const resource = await (await get(metadata)).json()
  f.check(resource.resource === url, 'oauth_resource', `the protected resource is ${resource.resource}, not ${url}`)
  const issuer = resource.authorization_servers?.[0]
  if (!f.check(isHTTPS(issuer), 'oauth_no_issuer', 'the protected resource names no authorization server')) return f.errors
  const server = await (await get(`${issuer}/.well-known/oauth-authorization-server`)).json()
  f.check(server.issuer === issuer, 'oauth_issuer', `the authorization server says it is ${server.issuer}, not ${issuer}`)
  f.check(isHTTPS(server.registration_endpoint), 'oauth_no_registration', 'the authorization server offers no Dynamic Client Registration')
  f.check(server.code_challenge_methods_supported?.includes('S256') === true, 'oauth_no_pkce', 'the authorization server does not offer PKCE S256')
  return f.errors
}
