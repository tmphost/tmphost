---
name: tmphost
description: Share files at temporary public URLs with tmphost. Use when the user wants to host, share, publish or send a link to a file, a generated report, an image or a static HTML site, or to fetch a file back from a tmphost folder.
---

# tmphost

tmphost hosts files in folders that expire after a TTL. Every file gets a
public URL until its folder expires. Use the `file` tool of the `tmphost` MCP
server; the first call opens the browser so the user can sign in (OAuth).

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

## Credits

Each file costs 1 credit per started MB of its size per started 30 minutes of
the folder's remaining lifetime, so pick the shortest TTL that fits the
request and say how long the link will work. Uploading into a folder that
expires sooner costs less. If an upload fails for lack of credits, tell the
user rather than retrying.
