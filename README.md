# tmphost.io

Go (Fiber, GORM) backend and Vue 3 (Vite, Tailwind) frontend.

## Sign-in and credits

- Sign in with Google or GitHub (OAuth). Accounts are keyed by verified email,
  so both providers reach the same account.
- Every account gets **250 operations** per period. A cron job running daily at
  00:00 UTC resets due accounts to 250 (unused credits do not carry over) and
  moves their renewal 30 days forward.

## Development

```sh
cp backend/cmd/server/.env.example backend/cmd/server/.env   # fill in OAuth apps, JWT secret, master key
set -a; . backend/cmd/server/.env; set +a
make dev-backend    # API on :2027
make dev-frontend   # app on http://localhost:5173 (proxies /api)
```

OAuth callback URLs to register: `<app URL>/api/v1/auth/<provider>/callback`
(`google`, `github`, or a custom provider's ID; the setup page shows each one).

Database: SQLite by default (`TMPHOST_SQLITE_DSN`); set
`TMPHOST_POSTGRES_ENABLED=true` and `TMPHOST_POSTGRES_DSN`
(`postgres://user:password@host:5432/tmphost?sslmode=disable`) for PostgreSQL.

## Build and test

```sh
make build   # bin/tmphost with the frontend embedded
make test    # 100% coverage is enforced on both sides
```

## Setup page

`/setup` opens only with the root access token (`TMPHOST_AUTH_ROOT_ACCESS_TOKEN`;
when it is empty, a random one is generated and printed at startup). There you
can change the monthly credit grant (applies immediately), and these, which
apply after a restart:

- **Domains**: the app URL (default `http://localhost:2027`; e.g.
  `https://app.tmphost.io`, any domain or subdomain) and the sites domain
  (e.g. `tmphost.io`) whose dynamic subdomains `f<11 characters>.<sites domain>`
  the server answers. Other hosts get a 404; localhost always reaches the app.
- **TLS**: serve HTTPS on its own port (default 443) and redirect the app port
  to it. Certificates come from Let's Encrypt automatically (lego, TLS-ALPN-01
  challenge, so Let's Encrypt must reach the server on port 443), are renewed
  when under 30 days and cached encrypted next to the settings file.
  - App host: always gets its own certificate.
  - Dynamic subdomains (switchable): each gets its own certificate on its first
    visit (Let's Encrypt allows about 50 new ones per week per registered
    domain), or, with an optional Cloudflare API token, one `*.<sites domain>`
    wildcard over DNS-01 with no such limit.
- **Database** (SQLite or PostgreSQL) and **S3 storage**.
- **Sign-in providers**: Google, GitHub, and any number of custom OpenID
  Connect providers (name, SVG icon, client ID, client secret, auth URL = the
  issuer; endpoints come from its `/.well-known/openid-configuration`).

Settings are saved to `TMPHOST_SETTINGS_FILE` (default
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
  -e TMPHOSTIO_MASTER_KEY=... -e TMPHOST_AUTH_JWT_SECRET=... -e TMPHOST_AUTH_ROOT_ACCESS_TOKEN=... \
  -e TMPHOST_BASE_URL=https://app.tmphost.io tmphost
```

Settings and the SQLite database live in the `/data` volume.
