# Bundler install

```ts
import { init } from "@edgedelta/browser-rum";

init({ token: "pub_...", service: "acme-web", environment: "production" });
```

Call it from the first module your entry imports. Everything after that point is
covered; the gap is what the browser threw while your bundle was still being fetched
and parsed, which no code inside that bundle can watch for itself. The Vite plugin
below closes it, and `init` picks up whatever it buffered.

Subpath exports: `/react`, `/vite`, `/testing`. `react` and `vite` are optional peer
dependencies, so installing the core alone pulls in nothing extra.

## The Vite plugin

`@edgedelta/browser-rum/vite` does the two things that have to happen at build time.

```ts
// vite.config.ts
import { edgeDeltaRum } from "@edgedelta/browser-rum/vite";

export default defineConfig({
  plugins: [edgeDeltaRum({ config: { service: "acme-web" } })],
});
```

**It resolves config from the environment** and serves it as
`virtual:edgedelta-rum`, so no variable names and no environment parsing reach the
browser bundle:

```ts
import { rumConfig } from "virtual:edgedelta-rum";
if (rumConfig) init({ ...rumConfig, ignoreErrors: [/^ResizeObserver/] });
```

`rumConfig` is `null` whenever no token was configured — that is the off switch,
and nothing is injected or reported. For its type, reference the client types once
from any file: `/// <reference types="@edgedelta/browser-rum/vite-client" />`.

**It emits a small blocking script into `<head>`** that buffers the errors thrown
before your bundle runs and hands them to `init`. It replaces an
`<!--%EDGEDELTA_RUM%-->` comment if the document has one and appends to `<head>`
otherwise; pass `placeholder` to look for a different comment.

## Environment variables

| Variable | Config field |
|---|---|
| `VITE_RUM_TOKEN` | `token` — its absence turns everything off |
| `VITE_RUM_SERVICE` | `service` |
| `VITE_RUM_ENDPOINT` | `endpoint` |
| `VITE_RUM_ENVIRONMENT`, else `VITE_STAGE` | `environment` |
| `VITE_APP_VERSION` | `version` |
| `VITE_RUM_SAMPLE_RATE` | `sampleRate` |
| `VITE_RUM_REQUEST_SPANS` | `requestSpans` |
| `VITE_RUM_NAVIGATION_SPANS` | `navigationSpans` |
| `VITE_RUM_USE_BEACON` | `useBeacon` |
| `VITE_RUM_DEBUG` | `debug` |
| `VITE_RUM_PROPAGATE_TO`, else the host of `VITE_API_URL` | `propagateTo` |

Anything the environment does not set falls back to `options.config`, then to the
SDK's own default. `false` and `0` read as off for the booleans; a
`VITE_RUM_PROPAGATE_TO` of `false` disables propagation entirely.

## Loader modes

| `loader` | What lands in `<head>` | Cost |
|---|---|---|
| `true` (default) | `<script src>` to a hashed same-origin asset | one extra request, cached forever, allowed by `script-src 'self'` |
| `'inline'` | the source itself | no request; needs `'unsafe-inline'` or a `sha256-` of these bytes in your CSP |
| `false` | nothing | no early-error buffer unless you serve `LOADER_SOURCE` yourself |

The request is a few hundred bytes, same-origin, and reuses the connection that
delivered the document — but `'inline'` removes it if a blocking request in `<head>`
matters more to you than a CSP entry. Compute that entry from `LOADER_SOURCE`:

```bash
node -e 'import("@edgedelta/browser-rum/vite").then(({ LOADER_SOURCE }) =>
  console.log("sha256-" + require("node:crypto").createHash("sha256").update(LOADER_SOURCE).digest("base64")))'
```

If your CSP uses a nonce instead, set Vite's own `html.cspNonce` — Vite appends
`nonce="…"` to every `<script>` after the plugins have run, so the loader tag is
covered in both modes and in dev, with nothing to configure here. Treat that value as
a placeholder your server replaces per response; a nonce baked into a static file is
readable by everyone and protects nothing.

`loader: false` is for an app whose HTML is assembled outside Vite: you still get
`virtual:edgedelta-rum`, and placing the loader is yours to do — serve `LOADER_SOURCE`
at a path built with the exported `contentHash`.
