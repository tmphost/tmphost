// Copyright 2026 Contextual, Inc. https://tmphost.io
// This notice may not be modified or removed.
// SPDX-License-Identifier: AGPL-3.0-only

/**
 * The rules a Codex / ChatGPT plugin package must pass before it can be
 * submitted to OpenAI's plugin directory, from
 * https://developers.openai.com/plugins/deploy/submission and the error codes
 * of https://developers.openai.com/plugins/deploy/submission-errors (findings
 * carry those codes). The final-submission limits apply, not the looser upload
 * ones, so a green check means the ZIP is ready to submit.
 *
 * Everything here works on an in-memory snapshot of the package, so it is
 * tested without a disk; `validate-codex-plugin.mjs` reads the real one.
 *
 * @typedef {{ files: Record<string, Buffer>, links?: string[] }} Package
 *   Every regular file by its `/`-separated path inside the plugin, and the
 *   paths of any symbolic links.
 * @typedef {{ code: string, message: string }} Finding
 */

export const PLUGIN_SCHEMA = 'https://agent-plugins.org/schemas/1.0.0/plugin.schema.json'
export const MCP_SCHEMA = 'https://agent-plugins.org/schemas/1.0.0/mcp.schema.json'

export const CATEGORIES = [
  'Productivity', 'Creativity', 'Developer Tools', 'Business & Operations', 'Data & Analytics',
  'Communication', 'Education & Research', 'Security', 'Finance', 'Healthcare', 'Travel',
  'Entertainment', 'Other',
]

const PLUGIN_KEYS = ['$schema', 'name', 'version', 'description', 'author', 'homepage', 'repository', 'license', 'keywords', 'extensions']
const OPENAI_KEYS = ['interface', 'onboardingSkill', 'review', 'publication', 'id']
const INTERFACE_KEYS = [
  'displayName', 'shortDescription', 'longDescription', 'developerName', 'category', 'capabilities',
  'websiteURL', 'supportURL', 'privacyPolicyURL', 'termsOfServiceURL', 'defaultPrompt', 'brandColor',
  'brandColorDark', 'composerIcon', 'composerIconDark', 'logo', 'logoDark', 'screenshots',
]
const REVIEW_KEYS = ['test_cases', 'demo_recording_url', 'commerce', 'commerce_description']
const CASE_KEYS = ['description', 'prompt', 'tools_triggered', 'expected_behavior', 'file_attachment_urls', 'expected_output_url']
const PUBLICATION_KEYS = ['countries', 'release_notes', 'translations']
const SKILL_KEYS = ['name', 'description', 'license']
const LISTING_URLS = ['websiteURL', 'supportURL', 'privacyPolicyURL', 'termsOfServiceURL']
const ICONS = ['composerIcon', 'composerIconDark', 'logo', 'logoDark']
/** Keys the portal rejects anywhere: credentials never go in the package. */
const SECRET_KEYS = ['test_credentials', 'reviewer_instructions', 'clientSecret']

// semver.org's own pattern.
const SEMVER = /^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$/
// Lowercase letters, digits and single hyphens: what the docs ask of names
// "for packages you plan to submit", within both the schema's and the
// portal's patterns.
const NAME = /^[a-z0-9]+(?:-[a-z0-9]+)*$/
// Control characters, Unicode line and paragraph separators and invisible
// formatting characters; line breaks are checked separately.
const UNSUPPORTED = /[\u0000-\u0009\u000B-\u001F\u007F-\u009F\u00AD\u200B-\u200F\u2028-\u202E\u2060-\u206F\uFEFF]/
const HEX = /^#[0-9A-Fa-f]{6}$/
const MiB = 1024 * 1024

const isObject = (v) => typeof v === 'object' && v !== null && !Array.isArray(v)

/** Collects findings, so each rule reads as one line. */
class Findings {
  errors = []
  warnings = []
  error(code, message) { this.errors.push({ code, message }) }
  warn(code, message) { this.warnings.push({ code, message }) }
  /** Fails `code` unless `ok`, and returns `ok`. */
  check(ok, code, message) {
    if (!ok) this.error(code, message)
    return ok
  }
  /** Names every key of `obj` outside `allowed`. */
  keys(obj, allowed, where) {
    for (const key of Object.keys(obj)) this.check(allowed.includes(key), 'unknown_field', `${where}.${key} is not a field OpenAI or Agent Plugins defines`)
  }
}

/**
 * Checks a text field: a non-empty string of supported characters within
 * `max`, on one line unless `multiline`. Returns whether it passed.
 */
function text(f, value, field, { max, multiline = false, required = true }) {
  if (value === undefined && !required) return true
  if (!f.check(typeof value === 'string', `${field}_wrong_type`, `${field} must be a string`)) return false
  if (!f.check(value.trim() !== '', `${field}_empty`, `${field} must not be empty`)) return false
  if (!f.check([...value].length <= max, `${field}_too_long`, `${field} must be ${max} characters or fewer`)) return false
  const breaks = multiline ? /\r/ : /[\r\n]/
  return f.check(!UNSUPPORTED.test(value) && !breaks.test(value), `${field}_character_unsupported`, `${field} must use supported text${multiline ? '' : ' on one line'}`)
}

/** Checks an HTTPS URL with a host and no credentials, within `max`. */
function https(f, value, field, max) {
  if (!text(f, value, field, { max })) return
  let url
  try {
    url = new URL(value)
  } catch {
    url = null
  }
  f.check(url?.protocol === 'https:' && url.hostname !== '', `${field}_not_https`, `${field} must be an HTTPS URL`)
  f.check(!url?.username && !url?.password, `${field}_has_credentials`, `${field} must not contain credentials`)
}

/** WCAG contrast ratio between two #RRGGBB colours. */
export function contrast(a, b) {
  const luminance = (hex) => {
    const [r, g, bl] = [1, 3, 5].map((i) => {
      const c = parseInt(hex.slice(i, i + 2), 16) / 255
      return c <= 0.03928 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4
    })
    return 0.2126 * r + 0.7152 * g + 0.0722 * bl
  }
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x)
  return (hi + 0.05) / (lo + 0.05)
}

/**
 * The pixel size of a PNG, JPEG or WebP image, or null when it doesn't
 * decode as the format its extension names.
 */
export function rasterSize(buf, ext) {
  if (ext === 'png') {
    if (buf.length < 24 || buf.toString('latin1', 0, 8) !== '\x89PNG\r\n\x1a\n') return null
    return { width: buf.readUInt32BE(16), height: buf.readUInt32BE(20) }
  }
  if (ext === 'webp') {
    if (buf.length < 30 || buf.toString('latin1', 0, 4) !== 'RIFF' || buf.toString('latin1', 8, 12) !== 'WEBP') return null
    const chunk = buf.toString('latin1', 12, 16)
    if (chunk === 'VP8X') return { width: buf.readUIntLE(24, 3) + 1, height: buf.readUIntLE(27, 3) + 1 }
    if (chunk === 'VP8L') {
      const bits = buf.readUInt32LE(21)
      return { width: (bits & 0x3fff) + 1, height: ((bits >> 14) & 0x3fff) + 1 }
    }
    if (chunk === 'VP8 ') return { width: buf.readUInt16LE(26) & 0x3fff, height: buf.readUInt16LE(28) & 0x3fff }
    return null
  }
  // JPEG: walk the segments to the first start-of-frame.
  if (buf.length < 4 || buf[0] !== 0xff || buf[1] !== 0xd8) return null
  for (let i = 2; i + 9 <= buf.length;) {
    if (buf[i] !== 0xff) return null
    const marker = buf[i + 1]
    if (marker >= 0xc0 && marker <= 0xcf && ![0xc4, 0xc8, 0xcc].includes(marker)) {
      return { width: buf.readUInt16BE(i + 7), height: buf.readUInt16BE(i + 5) }
    }
    i += 2 + buf.readUInt16BE(i + 2)
  }
  return null
}

/** The size an SVG declares: its viewBox, else unitless width and height. */
export function svgSize(source) {
  const root = /<svg\b([^>]*)>/.exec(source)
  if (!root) return { error: 'svg_root_element_invalid' }
  const attr = (name) => new RegExp(`\\s${name}\\s*=\\s*["']([^"']*)["']`).exec(root[1])?.[1]
  const viewBox = attr('viewBox')
  const values = viewBox !== undefined ? viewBox.trim().split(/[\s,]+/).slice(2) : [attr('width'), attr('height')]
  if (values.length !== 2 || values.includes(undefined)) return { error: 'svg_dimensions_missing' }
  if (!values.every((v) => /^\d+(\.\d+)?$/.test(v))) return { error: 'svg_dimensions_not_numeric' }
  const [width, height] = values.map(Number)
  if (width <= 0 || height <= 0) return { error: 'svg_dimensions_not_positive' }
  return { width, height }
}

/** Checks one image field: path rules, then format, size and squareness. */
function image(f, pkg, value, field) {
  if (!text(f, value, field, { max: 1024 })) return
  if (!f.check(value.startsWith('./'), 'branding_asset_path_missing_root_prefix', `${field} must start with ./`)) return
  const path = value.slice(2)
  if (!f.check(!path.split('/').includes('..') && !path.startsWith('/'), 'declared_asset_path_unsafe', `${field} must stay inside the plugin`)) return
  const buf = pkg.files[path]
  if (!f.check(buf !== undefined, 'declared_asset_file_missing', `${field} names ${value}, which is not a file in the plugin`)) return
  f.check(buf.length <= 5 * MiB, 'image_file_too_large', `${field} must be 5 MiB or smaller`)
  const ext = path.slice(path.lastIndexOf('.') + 1).toLowerCase()
  if (!f.check(['png', 'jpg', 'jpeg', 'webp', 'svg'].includes(ext), 'image_file_format_unsupported', `${field} must be a .png, .jpg, .jpeg, .webp or .svg file`)) return
  let size
  if (ext === 'svg') {
    size = svgSize(buf.toString('utf8'))
    if (!f.check(!size.error, size.error, `${field} must declare a numeric, positive viewBox or width and height`)) return
  } else {
    size = rasterSize(buf, ext === 'jpg' ? 'jpeg' : ext)
    if (!f.check(size !== null, 'raster_image_decode_failed', `${field} must decode as the format its extension names`)) return
    f.check(size.width <= 4096 && size.height <= 4096, 'raster_image_dimensions_too_large', `${field} must be at most 4096x4096 px, is ${size.width}x${size.height}`)
  }
  f.check(size.width === size.height, 'image_not_square', `${field} must be square, is ${size.width}x${size.height}`)
  f.check(size.width >= 48, 'image_dimensions_too_small', `${field} must be at least 48x48 px, is ${size.width}x${size.height}`)
}

/** Checks `extensions["com.openai"].interface`, the public listing. */
function listing(f, pkg, ui) {
  if (!f.check(isObject(ui), 'plugin_interface_wrong_type', 'extensions["com.openai"].interface must be an object')) return
  f.keys(ui, INTERFACE_KEYS, 'interface')
  text(f, ui.displayName, 'submission_display_name', { max: 30 })
  text(f, ui.shortDescription, 'submission_subtitle', { max: 30 })
  text(f, ui.longDescription, 'submission_description', { max: 4000, multiline: true })
  text(f, ui.developerName, 'submission_developer_name', { max: 80 })
  f.check(CATEGORIES.includes(ui.category), 'plugin_category_unknown', `interface.category must be one of ${CATEGORIES.join(', ')}`)

  if (f.check(Array.isArray(ui.capabilities), 'plugin_capabilities_wrong_type', 'interface.capabilities must be a list of strings ([] if none)')) {
    f.check(ui.capabilities.length <= 20, 'plugin_capabilities_too_many', 'interface.capabilities must have 20 entries or fewer')
    ui.capabilities.forEach((c, i) => text(f, c, `plugin_capability[${i}]`, { max: 120 }))
  }

  // A remote MCP plugin needs all four listing URLs.
  for (const key of LISTING_URLS) https(f, ui[key], `interface.${key}`, 1024)

  if (ui.defaultPrompt !== undefined) {
    const prompts = typeof ui.defaultPrompt === 'string' ? [ui.defaultPrompt] : ui.defaultPrompt
    if (f.check(Array.isArray(prompts), 'plugin_default_prompt_wrong_type', 'interface.defaultPrompt must be a string or a list of strings')) {
      f.check(prompts.length <= 3, 'plugin_default_prompt_too_many', 'interface.defaultPrompt must have at most three prompts')
      const seen = new Set()
      prompts.forEach((p, i) => {
        if (!text(f, p, `plugin_default_prompt[${i}]`, { max: 128 })) return
        f.check(!/@\S/.test(p), 'plugin_default_prompt_mention', `interface.defaultPrompt[${i}] must not @mention a server`)
        const norm = p.normalize('NFKC').replace(/\s+/g, ' ').trim().toLowerCase()
        f.check(!seen.has(norm), 'plugin_default_prompt_duplicate', `interface.defaultPrompt[${i}] repeats an earlier prompt`)
        seen.add(norm)
      })
    }
  }

  for (const [key, against, name] of [['brandColor', '#FFFFFF', 'white'], ['brandColorDark', '#212121', '#212121']]) {
    if (ui[key] === undefined) continue
    if (!f.check(typeof ui[key] === 'string' && HEX.test(ui[key]), `plugin_${key}_format`, `interface.${key} must be a six-digit hex colour such as #1ABCFE`)) continue
    f.check(contrast(ui[key], against) >= 2, `plugin_${key}_contrast`, `interface.${key} must have at least 2:1 contrast against ${name}`)
  }

  f.check(ui.logo !== undefined, 'plugin_logo_path_missing', 'interface.logo is required')
  f.check(ui.composerIcon !== undefined, 'plugin_composer_icon_path_missing', 'interface.composerIcon is required')
  for (const key of ICONS) if (ui[key] !== undefined) image(f, pkg, ui[key], `interface.${key}`)
  // Screenshots are only allowed when the MCP server renders its own UI,
  // which tmphost's doesn't.
  f.check(ui.screenshots === undefined, 'screenshots_not_allowed', 'interface.screenshots are only allowed for MCP servers with a UI template')
}

/** Checks one review test case. */
function testCase(f, c, where, positive) {
  if (!f.check(isObject(c), 'test_case_wrong_type', `${where} must be an object`)) return
  f.keys(c, CASE_KEYS, where)
  text(f, c.description, `${where}.description`, { max: 4000, multiline: true })
  text(f, c.prompt, `${where}.prompt`, { max: 4000, multiline: true })
  if (positive) {
    text(f, c.tools_triggered, `${where}.tools_triggered`, { max: 4000 })
    text(f, c.expected_behavior, `${where}.expected_behavior`, { max: 4000, multiline: true })
  }
  if (c.expected_output_url !== undefined) https(f, c.expected_output_url, `${where}.expected_output_url`, 2048)
  if (c.file_attachment_urls !== undefined && f.check(Array.isArray(c.file_attachment_urls), `${where}.file_attachment_urls_wrong_type`, `${where}.file_attachment_urls must be a list`)) {
    c.file_attachment_urls.forEach((u, i) => https(f, u, `${where}.file_attachment_urls[${i}]`, 2048))
  }
}

/** Checks `review`: what the review team needs to test an MCP plugin. */
function review(f, r) {
  if (!f.check(isObject(r), 'review_missing', 'extensions["com.openai"].review is required for MCP review')) return
  f.keys(r, REVIEW_KEYS, 'review')
  const cases = r.test_cases
  if (f.check(isObject(cases), 'test_cases_missing', 'review.test_cases is required for MCP review')) {
    f.keys(cases, ['positive', 'negative'], 'review.test_cases')
    for (const [kind, count] of [['positive', 5], ['negative', 3]]) {
      const list = cases[kind]
      if (!f.check(Array.isArray(list) && list.length === count, `test_cases_${kind}_count`, `review.test_cases.${kind} must have exactly ${count} cases`)) continue
      list.forEach((c, i) => testCase(f, c, `review.test_cases.${kind}[${i}]`, kind === 'positive'))
    }
  }
  if (r.demo_recording_url === undefined) {
    f.warn('demo_recording_url_missing', 'review.demo_recording_url is required before submitting for MCP review; add the walkthrough video URL')
  } else {
    https(f, r.demo_recording_url, 'review.demo_recording_url', 2048)
  }
  if (r.commerce !== undefined) f.check(typeof r.commerce === 'boolean', 'review_commerce_wrong_type', 'review.commerce must be true or false')
  if (r.commerce_description !== undefined) text(f, r.commerce_description, 'review.commerce_description', { max: 4000, multiline: true })
}

/** Checks `publication`: release notes, countries and translations. */
function publication(f, p) {
  if (!f.check(isObject(p), 'publication_missing', 'extensions["com.openai"].publication with release_notes is required for MCP review')) return
  f.keys(p, PUBLICATION_KEYS, 'publication')
  text(f, p.release_notes, 'publication.release_notes', { max: 4000, multiline: true })
  if (p.countries !== undefined) {
    f.check(Array.isArray(p.countries) && p.countries.every((c) => typeof c === 'string' && /^[A-Z]{2}$/.test(c)), 'publication_countries_format', 'publication.countries must be a list of uppercase two-letter country codes')
  }
  if (p.translations === undefined || p.translations === null) return
  if (!f.check(isObject(p.translations), 'publication_translations_wrong_type', 'publication.translations must be an object keyed by locale')) return
  for (const [locale, t] of Object.entries(p.translations)) {
    const where = `publication.translations.${locale}`
    if (!f.check(locale.trim() !== '' && isObject(t), 'publication_translation_wrong_type', `${where} must be an object under a non-empty locale`)) continue
    f.keys(t, ['subtitle', 'description'], where)
    if (t.subtitle !== null) text(f, t.subtitle, `${where}.subtitle`, { max: 30, required: false })
    if (t.description !== null) text(f, t.description, `${where}.description`, { max: 4000, multiline: true, required: false })
  }
}

/** Names every key in `secret` found anywhere inside `value`. */
function secrets(f, value, where) {
  if (Array.isArray(value)) value.forEach((v, i) => secrets(f, v, `${where}[${i}]`))
  else if (isObject(value)) {
    for (const [k, v] of Object.entries(value)) {
      f.check(!SECRET_KEYS.includes(k), 'package_secret_field', `${where}.${k}: credentials and reviewer instructions never go in the package`)
      secrets(f, v, `${where}.${k}`)
    }
  }
}

/** Parses a JSON file of the package, or records why it can't. */
function json(f, pkg, path) {
  const buf = pkg.files[path]
  if (!f.check(buf !== undefined, `${path}_missing`, `${path} is missing from the plugin root`)) return null
  let value
  try {
    value = JSON.parse(buf.toString('utf8'))
  } catch (err) {
    f.error(`${path}_json_malformed`, `${path} is not valid JSON: ${err.message}`)
    return null
  }
  return f.check(isObject(value), `${path}_root_not_object`, `${path} must hold a JSON object`) ? value : null
}

/** Checks the root `plugin.json`, the portable Agent Plugins manifest. */
function manifest(f, pkg) {
  const m = json(f, pkg, 'plugin.json')
  if (!m) return null
  f.keys(m, PLUGIN_KEYS, 'plugin.json')
  f.check(m.$schema === PLUGIN_SCHEMA, 'plugin_schema', `plugin.json $schema must be ${PLUGIN_SCHEMA}`)
  if (text(f, m.name, 'plugin_name', { max: 64 })) f.check(NAME.test(m.name), 'plugin_name_format', 'plugin.json name must be lowercase letters, digits and single hyphens')
  if (text(f, m.version, 'plugin_version', { max: 64 })) f.check(SEMVER.test(m.version), 'plugin_version_not_semver', 'plugin.json version must be a semantic version such as 1.0.0')
  text(f, m.description, 'plugin_description', { max: 1024, multiline: true })
  if (f.check(isObject(m.author), 'plugin_developer_missing', 'plugin.json author with a name is required')) {
    f.keys(m.author, ['name', 'email', 'url'], 'author')
    text(f, m.author.name, 'plugin_author_name', { max: 120 })
    text(f, m.author.email, 'plugin_author_email', { max: 320, required: false })
    if (m.author.url !== undefined) https(f, m.author.url, 'plugin_author_url', 2048)
  }
  if (m.homepage !== undefined) https(f, m.homepage, 'plugin_homepage', 2048)
  if (m.repository !== undefined) text(f, m.repository, 'plugin_repository', { max: 2048 })
  if (m.license !== undefined) text(f, m.license, 'plugin_license', { max: 120 })
  if (m.keywords !== undefined) {
    f.check(Array.isArray(m.keywords) && m.keywords.every((k) => typeof k === 'string' && k.trim() !== ''), 'plugin_keywords_wrong_type', 'plugin.json keywords must be a list of non-empty strings')
  }
  secrets(f, m, 'plugin.json')

  const ext = m.extensions
  if (!f.check(isObject(ext) && isObject(ext['com.openai']), 'openai_extension_missing', 'plugin.json needs extensions["com.openai"] with the listing')) return m
  f.keys(ext, ['com.openai'], 'extensions')
  const openai = ext['com.openai']
  f.check(openai.apps === undefined, 'app_configuration_excluded', 'extensions["com.openai"].apps can\'t be submitted; declare the server in mcp.json')
  f.check(openai.hooks === undefined, 'hooks_excluded', 'plugins with lifecycle hooks can\'t be submitted to the directory')
  f.keys(openai, [...OPENAI_KEYS, 'apps', 'hooks'], 'extensions["com.openai"]')
  if (openai.id !== undefined) text(f, openai.id, 'plugin_id', { max: 256 })
  if (openai.onboardingSkill !== undefined && text(f, openai.onboardingSkill, 'onboarding_skill', { max: 1024 })) {
    const path = openai.onboardingSkill.replace(/^\.\//, '')
    f.check(openai.onboardingSkill.startsWith('./') && /^skills\/[^/]+\/SKILL\.md$/.test(path) && pkg.files[path] !== undefined, 'onboarding_skill_missing', 'onboardingSkill must be ./skills/<skill>/SKILL.md of an included skill')
  }
  listing(f, pkg, openai.interface)
  review(f, openai.review)
  publication(f, openai.publication)
  return m
}

/** Checks `mcp.json`: exactly one remote, public HTTPS server. */
function mcp(f, pkg) {
  const m = json(f, pkg, 'mcp.json')
  if (!m) return null
  f.keys(m, ['$schema', 'mcpServers'], 'mcp.json')
  f.check(m.$schema === MCP_SCHEMA, 'mcp_schema', `mcp.json $schema must be ${MCP_SCHEMA}`)
  if (!f.check(isObject(m.mcpServers), 'mcp_servers_wrong_type', 'mcp.json mcpServers must be an object')) return null
  const servers = Object.entries(m.mcpServers)
  if (!f.check(servers.length === 1, 'mcp_server_count', 'a plugin connects exactly one MCP server; declare one')) return null
  const [[name, server]] = servers
  if (!f.check(name.trim() !== '' && isObject(server), 'mcp_server_wrong_type', 'the MCP server must be an object under a non-empty name')) return null
  f.keys(server, ['type', 'url', 'headers'], `mcpServers.${name}`)
  f.check(server.type === 'streamable-http', 'mcp_server_type', `mcpServers.${name}.type must be streamable-http for a remote server`)
  https(f, server.url, `mcpServers.${name}.url`, 2048)
  f.check(!/^https:\/\/(localhost|127\.|\[::1\])/.test(server.url), 'mcp_server_not_public', `mcpServers.${name}.url must be a public endpoint`)
  f.check(server.headers === undefined || (isObject(server.headers) && Object.keys(server.headers).length === 0), 'mcp_server_headers', 'public submission rejects non-empty server headers')
  secrets(f, m, 'mcp.json')
  return server.url
}

/**
 * Parses the YAML front matter of a SKILL.md: `key: value` lines only, the
 * subset skills need, so anything fancier fails loudly instead of being
 * misread.
 */
export function frontMatter(source) {
  const m = /^---\r?\n([\s\S]*?)\r?\n---(?:\r?\n|$)([\s\S]*)$/.exec(source)
  if (!m) return { error: source.startsWith('---') ? 'skill_frontmatter_unclosed' : 'skill_frontmatter_missing' }
  const fields = {}
  for (const line of m[1].split(/\r?\n/)) {
    if (line.trim() === '' || line.trimStart().startsWith('#')) continue
    const kv = /^([A-Za-z][\w-]*):[ \t]+(.+?)\s*$/.exec(line)
    if (!kv || /^[|>[{&*!]/.test(kv[2])) return { error: 'skill_frontmatter_yaml_malformed' }
    const quoted = /^(["'])(.*)\1$/.exec(kv[2])
    fields[kv[1]] = quoted ? quoted[2] : kv[2]
  }
  return { fields, body: m[2] }
}

/** Checks `skills/`: one directory per skill, each with a valid SKILL.md. */
function skills(f, pkg, pluginName) {
  const paths = Object.keys(pkg.files).filter((p) => p.startsWith('skills/'))
  const dirs = new Set()
  for (const p of paths) {
    const segs = p.split('/')
    if (!f.check(segs.length > 2, 'skill_file_ignored', `${p}: files directly under skills/ are not skills`)) continue
    dirs.add(segs[1])
    f.check(!/^agents\//.test(segs.slice(2).join('/')), 'skill_agent_unsupported', `${p}: agents/openai.yaml isn't checked by this validator; extend it before adding one`)
  }
  if (!f.check(dirs.size > 0, 'plugin_runtime_surface_missing', 'the package needs at least one skill in skills/<name>/SKILL.md')) return
  const names = new Set()
  for (const dir of dirs) {
    const where = `skills/${dir}/SKILL.md`
    f.check(!dir.startsWith('.'), 'skill_directory_hidden', `skills/${dir}: skill directories must not start with .`)
    const buf = pkg.files[where]
    if (!f.check(buf !== undefined, 'skill_manifest_missing', `${where} is missing`)) continue
    const source = buf.toString('utf8')
    if (!f.check(Buffer.from(source, 'utf8').equals(buf), 'skill_manifest_invalid_utf8', `${where} must be UTF-8`)) continue
    const fm = frontMatter(source)
    if (!f.check(!fm.error, fm.error, `${where} must start with --- front matter of plain key: value lines, closed by ---`)) continue
    f.keys(fm.fields, SKILL_KEYS, where)
    if (text(f, fm.fields.name, 'skill_name', { max: 64 })) {
      f.check(NAME.test(fm.fields.name) && fm.fields.name === dir, 'skill_name_format', `${where} name must be lowercase letters, digits and single hyphens, and match its directory`)
      f.check(!names.has(fm.fields.name), 'skill_identity_duplicate', `${where} name repeats another skill's`)
      f.check(`${pluginName}:${fm.fields.name}`.length <= 64, 'skill_identity_too_long', `${pluginName}:${fm.fields.name} must be 64 characters or fewer`)
      names.add(fm.fields.name)
    }
    text(f, fm.fields.description, 'skill_description', { max: 1024 })
    f.check(fm.body.trim() !== '', 'skill_body_empty', `${where} needs instructions after the front matter`)
  }
}

/** Checks the package as a ZIP: the archive limits and excluded files. */
function archive(f, pkg) {
  const paths = Object.keys(pkg.files)
  f.check(paths.length <= 5000, 'archive_too_many_entries', 'the package must hold 5000 files or fewer')
  const total = paths.reduce((sum, p) => sum + pkg.files[p].length, 0)
  f.check(total <= 512 * MiB, 'archive_uncompressed_too_large', 'the package must be 512 MiB or smaller')
  const seen = new Set()
  for (const p of paths) {
    const segs = p.split('/')
    f.check(!p.includes('\\'), 'archive_member_path_has_backslash', `${p}: use / in paths`)
    f.check(segs.every((s) => s !== '' && s === s.trim()), 'archive_member_path_has_empty_segment', `${p}: path segments must be non-empty without outer whitespace`)
    f.check(!segs.includes('..'), 'archive_member_path_has_parent_segment', `${p}: no .. segments`)
    f.check(segs.length <= 20, 'archive_member_path_too_deep', `${p}: at most 20 path segments`)
    f.check(pkg.files[p].length <= 100 * MiB, 'archive_member_too_large', `${p}: files must be 100 MiB or smaller`)
    const key = p.normalize('NFC').toLowerCase()
    f.check(!seen.has(key), 'archive_member_path_normalization_collision', `${p}: collides with another path after case and Unicode folding`)
    seen.add(key)
  }
  for (const link of pkg.links ?? []) f.error('archive_member_type_unsupported', `${link}: symbolic links can't be packaged`)
  // A portable package is plugin.json + mcp.json: these would be ignored at
  // best and get the ZIP rejected at worst.
  f.check(!pkg.files['.app.json'], 'app_configuration_excluded', '.app.json can\'t be submitted')
  f.check(!pkg.files['.mcp.json'], 'undeclared_mcp_manifest_ignored', '.mcp.json is the Codex-format file; the portable package uses mcp.json')
  f.check(!paths.some((p) => p.startsWith('hooks/')), 'hooks_excluded', 'plugins with lifecycle hooks can\'t be submitted to the directory')
  f.check(!paths.some((p) => /^\.(codex|claude|agent)-plugin\//.test(p)), 'overlay_manifest_ignored', 'extensions["com.openai"] in plugin.json replaces any overlay manifest; remove it')
}

/**
 * Validates a plugin package. Returns its findings, the manifest and the MCP
 * server URL, for the cross-checks the caller makes.
 *
 * @param {Package} pkg
 */
export function validatePlugin(pkg) {
  const f = new Findings()
  archive(f, pkg)
  const m = manifest(f, pkg)
  const url = mcp(f, pkg)
  skills(f, pkg, typeof m?.name === 'string' ? m.name : '')
  return { errors: f.errors, warnings: f.warnings, manifest: m, url }
}

/**
 * Validates `.agents/plugins/marketplace.json` against the plugin it lists.
 *
 * @param {unknown} market the parsed marketplace file
 * @param {{ name: string, path: string, isPlugin: (path: string) => boolean }} plugin
 *   the plugin's manifest name, its path from the repository root, and a
 *   check that a `./`-path holds a plugin.json
 */
export function validateMarketplace(market, plugin) {
  const f = new Findings()
  if (!f.check(isObject(market), 'marketplace_wrong_type', 'marketplace.json must hold a JSON object')) return f.errors
  f.keys(market, ['name', 'interface', 'plugins'], 'marketplace')
  text(f, market.name, 'marketplace_name', { max: 64 })
  f.check(isObject(market.interface) && typeof market.interface.displayName === 'string' && market.interface.displayName.trim() !== '', 'marketplace_display_name', 'marketplace interface.displayName is required')
  if (!f.check(Array.isArray(market.plugins), 'marketplace_plugins_wrong_type', 'marketplace plugins must be a list')) return f.errors
  const entry = market.plugins.find((p) => p?.name === plugin.name)
  if (!f.check(entry !== undefined, 'marketplace_plugin_missing', `marketplace lists no plugin named ${plugin.name}`)) return f.errors
  const where = `marketplace plugin ${plugin.name}`
  f.keys(entry, ['name', 'source', 'policy', 'category'], where)
  const path = typeof entry.source === 'string' ? entry.source : entry.source?.source === 'local' ? entry.source.path : undefined
  f.check(path === `./${plugin.path}` && plugin.isPlugin(path), 'marketplace_source', `${where} source must be the local path ./${plugin.path}`)
  f.check(['AVAILABLE', 'INSTALLED_BY_DEFAULT', 'NOT_AVAILABLE'].includes(entry.policy?.installation), 'marketplace_policy_installation', `${where} policy.installation must be AVAILABLE, INSTALLED_BY_DEFAULT or NOT_AVAILABLE`)
  f.check(['ON_INSTALL', 'ON_USE'].includes(entry.policy?.authentication), 'marketplace_policy_authentication', `${where} policy.authentication must be ON_INSTALL or ON_USE`)
  f.check(CATEGORIES.includes(entry.category), 'marketplace_category', `${where} category must be one of ${CATEGORIES.join(', ')}`)
  return f.errors
}

/** Formats findings as GitHub Actions annotations. */
export function report({ errors, warnings }, file) {
  return [
    ...errors.map((e) => `::error file=${file}::${e.code}: ${e.message}`),
    ...warnings.map((w) => `::warning file=${file}::${w.code}: ${w.message}`),
  ].join('\n')
}
