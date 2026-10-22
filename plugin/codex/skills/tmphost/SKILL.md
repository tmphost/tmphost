---
name: tmphost
description: Share files at temporary public URLs with tmphost. Use when the user asks to host, share, publish or send a link to a file, a generated report, an image or a static HTML site, or to fetch a file back from a tmphost folder.
---

# tmphost

tmphost hosts files in folders that expire after a TTL. Every file gets a
public URL until its folder expires. Use the `file` tool of the `tmphost` MCP
server; the first call asks the user to sign in to tmphost (OAuth).

The user's explicit instructions take precedence over this guidance: if they
ask for a different TTL, folder or file name, use theirs.

## Before uploading

- Anyone with the link can open the file. Do not upload secrets, private keys,
  passwords, tokens or other credentials, even when asked; explain why and
  suggest a private channel instead.
- tmphost only hosts files for a limited time. If the user wants permanent
  hosting, say that tmphost can't do that rather than uploading.
- If it is unclear which file or content to share, ask before uploading.

## Actions

- `upload`: `fileName`, the content, and optionally `path` (a directory inside
  the folder, such as `site/css`). Without `id` it creates a new folder that
  lives for `ttl` (default `30m`, e.g. `2h`); with `id` the file goes into
  that existing folder. Returns the folder `id`, the file `url` and
  `creditsSpent`.
- `download`: `id`, `fileName`, optional `path`. Returns the content.
- `get`: `id`. Lists the folder's files with their URLs.
- `url`: the public URL of a folder (`id`), a directory (`id`, `path`) or a
  file (`id`, `fileName`, optional `path`).
- `extend`: `id` and `ttl` (the time to add, e.g. `1h`). Returns the new
  `expiresAt` and `creditsSpent`.
- `pin`: `id`. Gives the folder a new 6 digit PIN that visitors must enter,
  returned once in `pin`; it replaces the folder's old PIN.

Only use folder ids that a tool call returned or that the user gave you; never
guess one.

## Content

- Text files (HTML, CSS, JS, JSON, Markdown, CSV, SVG): pass
  `format: "textContent"` and the file as is in `textContent`.
- Anything else (images, PDFs, archives): `format: "base64Content"` (the
  default) with the bytes in `base64Content`. Encode local files with a
  command such as `base64 -w0 <file>` rather than by hand.

## Sharing a site or several files

1. Upload the first file without `id` and note the returned `id`.
2. Upload the rest with that `id`, keeping their directories in `path`
   (`index.html` at the root, `css/site.css` as `fileName: "site.css"`,
   `path: "css"`). Paths are at most 5 directories deep, with no `.` or `..`
   segments.
3. Give the user the folder URL (`url` with just `id`); `index.html` is served
   for the folder root.

## Protecting a folder with a PIN

When the user wants the files kept from anyone without a code, pass
`pin: true` on the first upload (or call `pin` with the folder `id`). The
result's `pin` is shown only once: give it to the user with the URL. Visitors
enter it once per browser; a link with `?pin=<pin>` appended opens the folder
without asking. A result with `pinMode` but no `pin` means the user's account
PIN (or authenticator app) already guards every folder of theirs.

## Sharing Markdown as a page

Upload a `.md` file with `format: "textContent"` and give the user its `url`
with `?format=html` appended: it opens as a styled reading page (tables, task
lists, code blocks, light and dark), while the plain `url` serves the raw
Markdown.

## Reply

Give the user the link, say when it expires, and mention the credits spent.

## Credits

Each file costs 1 credit per started MB of its size per started 30 minutes of
the folder's remaining lifetime, so pick the shortest TTL that fits the
request. Uploading into a folder that expires sooner costs less. Extending a
folder costs the same per file, for the added time, so extend only when the
user wants the link to last longer. If a call fails for lack of credits, tell
the user rather than retrying.
