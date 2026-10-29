---
name: share-site
description: Upload a static HTML site to one tmphost folder and reply with its public URL.
---

# Share a static site

1. Find the site's directory (a build output such as `dist/` or the folder the
   user names) and list its files. It needs an `index.html` at its root.
2. Upload `index.html` with `uploadFile` without `folderId` and note the
   returned `folderId`.
3. Upload every other file with that `folderId`, its directory in `path`
   (`css/site.css` is `fileName: "site.css"`, `path: "css"`). Text files go as
   `textContent`, images and fonts as `base64Content`.
4. Call `getUrl` with just the `folderId` and reply with that URL, when it
   expires and the credits spent.
