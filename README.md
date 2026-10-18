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
make dev-backend    # API on :3000
make dev-frontend   # app on http://localhost:5173 (proxies /api)
```

OAuth callback URLs to register: `<TMPHOST_BASE_URL>/api/v1/auth/google/callback`
and `<TMPHOST_BASE_URL>/api/v1/auth/github/callback`.

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
can change the monthly credit grant (applies immediately), the database
(SQLite or PostgreSQL), S3 storage and the sign-in providers (these apply after
a restart).

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
docker run -p 3000:3000 -v tmphost-data:/data \
  -e TMPHOSTIO_MASTER_KEY=... -e TMPHOST_AUTH_JWT_SECRET=... -e TMPHOST_AUTH_ROOT_ACCESS_TOKEN=... \
  -e TMPHOST_BASE_URL=https://tmphost.example.com tmphost
```

Settings and the SQLite database live in the `/data` volume.
