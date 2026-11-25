# Script-tag install

## 1. Serve the loader from your own domain

Copy [`../snippet.js`](../snippet.js) to wherever the site serves static assets —
`public/rum.js`, `static/rum.js` — and replace the two placeholders. The `token` comes
from the HTTP ingestion node that will receive this traffic.

## 2. Load it first in `<head>`

```html
<!-- Edge Delta RUM -->
<script src="/rum.js"></script>
```

Three rules, each of which costs you the early-error window if broken:

**No `async`, no `defer`, nothing above it.** The file installs error handlers and
starts buffering synchronously, then loads the bundle asynchronously so it never
blocks rendering. Everything thrown before the bundle arrives is still captured —
which is where the most interesting failures live.

**Do not paste it inline into the HTML.** An inline script needs `'unsafe-inline'` or
its own hash in a `script-src` CSP, and HTML formatters — prettier and biome both —
reformat embedded scripts, which silently invalidates that hash and takes RUM down
with no error anywhere. A same-origin file needs neither, and `init` can use `RegExp`
and callback options there.

**Keep `init` out of application code.** For a framework app the tag belongs in the
HTML shell — `index.html`, `app.html`, the document template — not in a root
component. In Next.js, `app/layout.tsx` with
`<Script src="/rum.js" strategy="beforeInteractive">`.

## Subresource Integrity

Optional, worth doing for production. It tells the browser to refuse the bundle if its
bytes ever change, so a compromised CDN cannot run new code on your pages.

```bash
set -o pipefail; curl --fail -sS https://js.edgedelta.com/rum/v0.6.1/rum.min.js | openssl dgst -sha384 -binary | openssl base64 -A
```

In the loader file, change `a.crossOrigin="anonymous"` to
`a.crossOrigin="anonymous",a.integrity="sha384-PASTE_HASH_HERE"`. Keep whatever prefix
the snippet already uses for the script element rather than the letter shown here.

**Only for a fully pinned URL** — one ending in a complete version, like `/rum/v0.6.1/`.
A rolling URL such as `/rum/v1/` deliberately serves a new build on every patch
release, so a pinned hash would begin rejecting it and RUM would go silent with no
warning. Published versions are never overwritten, so a hash stays valid for the life
of the version you pinned.

Check it before shipping: load the page and confirm `rum.min.js` fetched with status
200. A wrong hash makes the browser block the script entirely, and nothing in Edge
Delta will indicate why.

## Content Security Policy

| Directive | Needs |
|---|---|
| `script-src` | `https://js.edgedelta.com` for the bundle. The loader file is same-origin, so `'self'` already covers it — which is the reason for not inlining it. |
| `connect-src` | your `endpoint` host |

Either block shows up as a CSP violation in the console and as nothing at all in Edge
Delta.

## TypeScript

Types are served beside the bundle at `https://js.edgedelta.com/rum/v0.6.1/rum.d.ts`.
Save it into the project and reference it once to type both `EdgeDelta` and
`window.EdgeDelta`:

```ts
/// <reference path="./rum.d.ts" />
```
