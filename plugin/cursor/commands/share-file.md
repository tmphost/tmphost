---
name: share-file
description: Upload a file to tmphost and reply with a temporary public link.
---

# Share a file

1. Find the file the user means: a path they gave, the open file, or content
   you just generated. If it is unclear, ask.
2. Refuse secrets, credentials, `.env` files and private keys: anyone with the
   link can open the file.
3. Call `uploadFile` without `folderId`. Text files (HTML, CSS, JS, JSON,
   Markdown, CSV, SVG) go as `format: "textContent"`; anything else as
   `base64Content`, encoded with `base64 -w0 <file>`. Use the TTL the user
   asked for, else the default.
4. Reply with the `url`, when it expires and the credits spent. For a `.md`
   file, also give the `url` with `?format=html` for a reading page.
