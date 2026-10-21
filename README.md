# tmphost.io

Go (Fiber v3, GORM) backend and Vue 3 (Vite, Tailwind) frontend.

The HTTP API (`/api/v1`, sign-in, the OAuth 2.1 server, public file URLs and
`/mcp`) is described in [openapi.yaml](openapi.yaml) (OpenAPI 3.1). A backend
test fails when a route is missing from it, so keep it in step with
`backend/internal/handler/api/api.go`.

## Sign-in and credits

- Sign in with Google or GitHub (OAuth). Accounts are keyed by verified email,
  so both providers reach the same account.
- A new account starts with **1000 operations** (the sign-up grant,
  `TMPHOSTIO_CREDITS_SIGNUP_GRANT`). Every 30 days it gets **250 operations**
  (the monthly grant, `TMPHOSTIO_CREDITS_MONTHLY_GRANT`): a cron job running
  daily at 00:00 UTC resets due accounts to 250 (unused credits do not carry
  over) and moves their renewal 30 days forward.
- Every change to a balance is kept in `credit_activities`, written in the
  same transaction as the change: `ops` (stored as a uint8: grant, renewal,
  spend, refund, admin_grant, expiry), the signed `change` (+X or -Y) and the `current` balance
  after it. The Credits page charts daily use and balance over 30 days and
  lists the history (`GET /api/v1/users/me/credits/activities?before=&limit=`,
  newest first).

## File hosting

- Files live in folders that expire after a TTL (default 30 minutes, at most
  `files.maxTtlMinutes`, 7200 hours by default). Each file written costs 1
  credit per started MB (1 MiB) of its size per started 30 minutes of the
  folder's remaining lifetime: a 1.01 MB file kept for an hour costs 2 x 2 = 4.
- Files can be added to an existing folder (web app, MCP `upload` with `id`,
  CLI `--id`). Uploading to a path that is already there replaces the file
  in place, an update: its URL stays the same and the folder keeps its
  expiry. The write is charged like any other.
- A folder can be extended to expire later (web app, MCP `extend`, CLI
  `tmphost file extend`, `POST /api/v1/folders/:id/extend` with
  `{"minutes": N}`), as long as its remaining lifetime stays within the max
  TTL. Each file in it costs the same as an upload, for the added time: 1
  credit per started MB per started 30 minutes. An empty folder is free.
- Without signing in, the web app's Upload page makes folders that live at
  most `files.anonymousMaxTtlMinutes` (`TMPHOSTIO_FILES_ANONYMOUS_MAX_TTL_MINUTES`,
  10 minutes by default; root can change it on /setup).
- Storage is local disk (`TMPHOSTIO_STORAGE_LOCAL_PATH`) or an S3 bucket, under
  `u<userId>/<folderId>/<path>`. Paths are at most 5 directories deep and are
  validated server-side, so a file can never leave its folder. The database
  stores each folder's ID, owner and expiry, and the size, type and update
  time of its files (so listings and redirects need no storage call), never
  their bytes; a job deletes expired folders every minute. Folder IDs are 8 random lowercase base36
  characters, so public URLs can't be guessed.
- Uploads are also kept in memory and served from there, with no time
  limit, up to 256 MB in all (`TMPHOSTIO_STORAGE_CACHE_MAX_MB`, least
  recently used dropped first; 0 turns it off). Deleting a file or folder
  drops it from memory too. With
  S3, an upload is answered once it is in memory and written to the bucket
  in the background (`TMPHOSTIO_STORAGE_CACHE_ASYNC`, on by default):
  writes of a path land in order, are retried 3 times, are listed and
  served from memory until they land, and finish before shutdown. Past
  another 256 MB of writes in flight, uploads wait for the bucket again.
- With S3, an optional warm cache (setup page → Storage, off by default)
  keeps files under 1 MB on the server's disk as well, in `warm/` next to
  the settings file (`/data/warm` in Docker), up to 1024 MB by default
  (least recently used dropped first, no time limit). An upload goes to
  disk first, then to the bucket, retried with exponential backoff (1s,
  2s, 4s, ...) as many times as the setup page says (0 by default), and the
  copy is dropped again if the bucket write still fails; a file read from the bucket is copied to disk on the
  way out. Deletes drop the copies. The directory is emptied on start.
- Folder lookups are served from memory too, up to 64 MB in all
  (`TMPHOSTIO_CACHE_FOLDERS_MAX_MB`, least recently used dropped first; 0
  turns it off). Only writing to or deleting a folder drops it (not its
  expiry), and the next lookup reads it from the database again.
- With S3, downloads of files over 1 MB (`TMPHOSTIO_STORAGE_REDIRECT_OVER_MB`,
  0 turns it off) on public URLs, the API and the CLI answer `302 Found` to
  a URL of the bucket signed for 15 minutes (`TMPHOSTIO_STORAGE_REDIRECT_TTL`),
  so large files don't pass through tmphost. HTML files (`.html`, `.htm`,
  whatever their size, on every route), files still in memory and MCP
  downloads (base64Content/textContent) are served directly.
- Public URLs: `<TMPHOSTIO_BASE_URL>/files/<userId>/<folderId>/<path>` by
  default, or `<folderId>.<publicDomain>/<path>` with subdomains on
  (`TMPHOSTIO_FILES_SUBDOMAINS`, `TMPHOSTIO_FILES_PUBLIC_DOMAIN`, which defaults
  to the sites domain; needs wildcard DNS and a certificate, which the
  built-in TLS obtains when it is the sites domain). Both are served with `Content-Security-Policy:
  sandbox`, so hosted pages never run with the app's origin.
- A Markdown file (`.md`, `.markdown`) is served as is, or with
  `?format=html` as a reading page: GitHub-flavoured Markdown (tables, task
  lists, strikethrough, footnotes) in a wide serif column that follows the
  visitor's light or dark theme, titled after its first `#` heading. Raw
  HTML and `javascript:` links in the source are left out.

## MCP, CLI and OAuth

- `/mcp` is a stateless MCP server with one tool, `file` (actions `upload`,
  `download`, `get`, `url`, `extend`), on the app URL and, when set, on the MCP domain
  (`https://<mcp domain>/mcp`). The `tmphost` CLI in [cli/](cli/) mirrors it.
  File content travels as `base64Content` (the default) or, when the
  `format` argument asks for it, as UTF-8 `textContent`.
- Both sign in with tmphost's own OAuth 2.1 server: dynamic client
  registration (`/oauth2/register`), PKCE S256, a consent page
  (`/oauth2/authorize`) and refresh tokens (`/oauth2/token`), discovered via
  `/.well-known/oauth-authorization-server` and
  `/.well-known/oauth-protected-resource/mcp`. The CLI uses the built-in
  public client ID `00000000001` with a loopback redirect, or, where no
  browser can reach it (SSH, containers), the device flow (RFC 8628):
  `/oauth2/device/code` hands out a code the user enters at `/auth/device`
  while the CLI polls `/oauth2/token`. Access tokens also
  work as Bearer tokens on `/api/v1`. The MCP domain is an issuer of its own
  (`https://<mcp domain>`), with its registration and token endpoints on that
  host; its users sign in and consent on the app URL at
  `/oauth2/mcp/authorize`, which answers with the MCP domain's `iss`.

## Claude Code plugin

[plugin/claude](plugin/claude) is a Claude Code plugin that adds the `/mcp`
server and a `tmphost` skill (when and how to share files with it). This
repository is also its marketplace ([.claude-plugin/marketplace.json](.claude-plugin/marketplace.json)):

```sh
/plugin install tmphost --marketplace tmphost/tmphost
# or, in two steps:
/plugin marketplace add tmphost/tmphost
/plugin install tmphost@tmphost
```

It connects to `https://mcp.tmphost.io/mcp`. Claude Code signs in through
the browser on the first tool call. For a self-hosted server, add your own
MCP server instead of the plugin:
`claude mcp add --transport http tmphost https://mcp.example.com/mcp`.
Its listing icon is [plugin/claude/.claude-plugin/icon.png](plugin/claude/.claude-plugin/icon.png)
(1024 px, rendered from favicon-light.svg) and its privacy policy is
[https://tmphost.io/privacy](https://tmphost.io/privacy).
CI runs `claude plugin validate --strict` on both manifests and checks the
icon and privacy policy URL.

## Gemini CLI extension

This repository is also a [Gemini CLI](https://github.com/google-gemini/gemini-cli)
extension: [gemini-extension.json](gemini-extension.json) adds the MCP server
at `https://mcp.tmphost.io/mcp` and loads
[plugin/gemini/GEMINI.md](plugin/gemini/GEMINI.md) (when and how to share
files with it) into every session.

```sh
gemini extensions install https://github.com/tmphost/tmphost
```

Gemini CLI signs in through the browser (`/mcp auth tmphost`). For a
self-hosted server, see [plugin/gemini](plugin/gemini/README.md).

## Codex and ChatGPT plugin

[plugin/codex](plugin/codex) is a portable [Agent Plugins](https://agent-plugins.org)
package for Codex and ChatGPT with the MCP server and the `tmphost` skill. This
repository is its marketplace ([.agents/plugins/marketplace.json](.agents/plugins/marketplace.json)):

```sh
codex plugin marketplace add tmphost/tmphost
codex plugin add tmphost@tmphost
```

Codex signs in through the browser on the first tool call. CI checks the
package against OpenAI's directory submission rules and the Agent Plugins
schemas, installs it with the Codex CLI, and uploads the ZIP to submit; see
[plugin/codex](plugin/codex/README.md).

## Development

```sh
cp backend/cmd/server/.env.example backend/cmd/server/.env   # fill in OAuth apps, JWT secret, master key
set -a; . backend/cmd/server/.env; set +a
make dev            # both at once (Ctrl-C stops both), or separately:
make dev-backend    # API on :2027
make dev-frontend   # app on http://localhost:5173 (proxies /api)
```

OAuth callback URLs to register: `<app URL>/auth/<provider>/callback`
(`google`, `github`, or a custom provider's ID; the setup page shows each one).

Database: SQLite by default (`TMPHOSTIO_SQLITE_DSN`); set
`TMPHOSTIO_POSTGRES_ENABLED=true` and `TMPHOSTIO_POSTGRES_DSN`
(`postgres://user:password@host:5432/tmphost?sslmode=disable`) for PostgreSQL.

## Build and test

```sh
make build   # bin/tmphost with the frontend embedded
make test    # 100% coverage is enforced on both sides
```

## Installable app (PWA)

The web app installs as a PWA (`frontend/public/manifest.webmanifest`,
`frontend/src/sw.js`). The service worker precaches only the built app shell;
`/api`, `/auth`, `/oauth2`, `/mcp` and folder files always go to the network.
An open tab checks for a new version on load, every hour and when it comes
back into view, downloads it in the background with a progress bar, then
offers "Update now", which reloads every open tab into the new version. The
service worker is registered only in production builds (`npm run build`), not
under `npm run dev`.

## Admin page

`/admin` opens only with the root access token too (one root sign-in covers
`/setup` and `/admin`). Root can grant credits to one user (by email) or to
every user (`POST /admin/v1/credits/grants` with
`{"credits": 500, "email": "a@b.c"}` or `{"credits": 500, "all": true}`).
Granted credits are kept in `credit_grants` and logged as `admin_grant`. They
expire 30 days later at the next 00:00 UTC, when the daily job takes back
whatever is left (logged as `expiry`). Monthly renewals don't reset them, and
spends take the credits that expire first.

The admin page also shows stats over every account (users, credits held,
credits used and sign-ups per day over the last 30 days, live folders) and a
user list you can search by email, name or user ID. Each user's page shows
their balance, usage, live folders and full credit history. The admin API
lives under `/admin/v1`, apart from the public `/api/v1`, and is not part of
`openapi.yaml`.

## Setup page

`/setup` opens only with the root access token (`TMPHOSTIO_AUTH_ROOT_ACCESS_TOKEN`;
when it is empty, a random one is generated and printed at startup). There you
can change the sign-up and monthly credit grants, the maximum TTL and the public URL mode
(these apply immediately), and these, which apply after a restart:

- **Domains**: the app URL (default `http://localhost:2027`; e.g.
  `https://app.tmphost.io`, any domain or subdomain) and the sites domain
  (e.g. `tmphost.io`) whose dynamic subdomains `<8 characters>.<sites domain>`
  the server answers, and an optional MCP domain (e.g. `mcp.tmphost.io`,
  `TMPHOSTIO_MCP_DOMAIN`) that serves only `/mcp`, its OAuth metadata and
  its OAuth register and token endpoints, so
  MCP clients connect to `https://mcp.tmphost.io/mcp`. Other hosts get a 404;
  localhost always reaches the app.
- **TLS**: serve HTTPS on its own port (default 443) and redirect the app port
  to it. Certificates come from Let's Encrypt automatically (lego, TLS-ALPN-01
  challenge, so Let's Encrypt must reach the server on port 443), are renewed
  when under 30 days and cached encrypted next to the settings file.
  - App host and MCP domain: each always gets its own certificate.
  - Dynamic subdomains (switchable): each gets its own certificate on its first
    visit (Let's Encrypt allows about 50 new ones per week per registered
    domain), or, with an optional Cloudflare API token, one `*.<sites domain>`
    wildcard over DNS-01 with no such limit.
- **Database** (SQLite or PostgreSQL) and **file storage** (local disk or S3).
- **Files**: the maximum TTL and upload size, and whether folders are served
  at paths or on `<folder ID>.<public domain>` subdomains.
- **Sign-in providers**: Google, GitHub, and any number of custom OpenID
  Connect providers (name, SVG icon, client ID, client secret, auth URL = the
  issuer; endpoints come from its `/.well-known/openid-configuration`).

Settings are saved to `TMPHOSTIO_SETTINGS_FILE` (default
`./_storage/settings.json`, mode 0600) and override the config file and
environment. Secrets (including the PostgreSQL DSN) are write-only on the
setup page.

- **Encrypted at rest**: the file is AES-256-GCM encrypted with a key derived
  (HKDF-SHA256) from `TMPHOSTIO_MASTER_KEY`. The master key is read from the
  environment only, never from YAML or the settings file, and must be at least
  32 characters (`openssl rand -hex 32`); the server will not start without it.
  A wrong key or a tampered file stops startup with an error; if the key is
  lost, delete the file and re-enter the settings.
- **Never served**: the server only serves the embedded frontend, and only web
  asset types (`.js`, `.css`, `.svg`, ...) from it.

## Docker

One image, one binary: the frontend and base config are embedded, and the
image (`FROM scratch`, runs as `nobody`) holds just the binary and CA certs.

```sh
make docker
docker run -p 2027:2027 -v tmphost-data:/data \
  -e TMPHOSTIO_MASTER_KEY=... -e TMPHOSTIO_AUTH_JWT_SECRET=... -e TMPHOSTIO_AUTH_ROOT_ACCESS_TOKEN=... \
  -e TMPHOSTIO_BASE_URL=https://app.tmphost.io tmphost
```

Settings and the SQLite database live in the `/data` volume.
