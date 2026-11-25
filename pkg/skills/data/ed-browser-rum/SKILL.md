---
name: ed-browser-rum
description: Add Edge Delta Real User Monitoring to a website or web app. Use when the user asks to "add RUM", "add real user monitoring", "monitor frontend performance", "track Core Web Vitals", "capture JavaScript errors from users", "instrument the frontend", or wants browser and page-load telemetry sent to Edge Delta. Covers both the script-tag install and the @edgedelta/browser-rum npm package, including its Vite plugin and React error boundary.
metadata:
  version: "1.0.0"
  author: edgedelta
  repository: https://github.com/edgedelta/agent-skills
  tags: edgedelta,rum,browser,frontend,web-vitals,observability,otlp,errors
  alwaysApply: "false"
---

# Edge Delta browser RUM

Reports page-load performance, Core Web Vitals and JavaScript errors from real
visitors' browsers, as OTLP, into an Edge Delta pipeline.

| Signal | Arrives as | Sent when |
|---|---|---|
| Page loads, Core Web Vitals, request spans | `documentLoad` **trace span** | the visitor leaves or backgrounds the page |
| JavaScript errors | **log records** at severity `ERROR` | within a few seconds |

Both carry `session.id`, `url.*` and the view's trace id, which is how you get from
one error to the page load it happened in. Current version: **v0.6.1**.

## Pick an install path

Both behave identically at runtime. They differ in one thing: **what happens to
errors thrown before RUM is running.**

| | Script tag | Bundler (npm) |
|---|---|---|
| Install | copy [snippet.js](snippet.js) to your static dir | `npm i @edgedelta/browser-rum` |
| Errors before your bundle loads | captured | lost, unless you add the loader |
| Config from env vars | hand-written | resolved at build time by the Vite plugin |
| `RegExp` / callback options | only in a same-origin file | anywhere |
| Guide | [reference/script-tag.md](reference/script-tag.md) | [reference/bundler.md](reference/bundler.md) |

Prefer the script tag for a plain site or any app whose HTML you control. Prefer the
bundler for a Vite app — its plugin emits the same early-error loader, so you give up
nothing.

Either way, `init` needs a `token` and a `service`, and nothing else is required.

## Configuration

| Option | Default | What it does |
|---|---|---|
| `token` | — | Ingestion token from the pipeline's HTTP ingestion node. Public by design. |
| `service` | — | Names this app in queries and dashboards. |
| `version` | — | Your release version. Set it — stacks are minified today and need it to symbolicate later. |
| `environment` | — | `production`, `staging`, … Set it, or prod and staging data mix. |
| `endpoint` | Edge Delta ingest | Scheme and host **only** — the SDK appends `/otel/v1/traces` and `/otel/v1/logs`. |
| `sampleRate` | `1` | Fraction of sessions, `0`–`1`. Decided once per session, so a recorded session is complete. |
| `captureErrors` | `true` | Uncaught errors and unhandled promise rejections. |
| `routeChanges` | `true` | Opens a new view on each SPA path change. Off only if the app calls `startView` itself. |
| `navigationSpans` | `false` | Load phases as child spans, for a waterfall. Up to 8× the trace items per page view. |
| `requestSpans` | `false` | One client span per `fetch`/XHR. The largest volume multiplier — turn it on deliberately. |
| `propagateTo` | `[]` | Cross-origin hosts to send `traceparent` to. **Needs CORS on that server first.** |
| `breadcrumbs` | `20` | Trail attached to every error. `false` turns it off. Click descriptors are structural only — never text, input values or `aria-label`. |
| `ignoreErrors` | `[]` | Drop matching errors: substring, `RegExp`, or predicate. Matched on message and exception type, never the stack. |
| `beforeSend` | — | `(event) => event \| null`. Last chance to redact or drop. |
| `debug` | `false` | Logs the whole pipeline to the console. Also switched on by an `ed_rum_debug` key in `localStorage`, so a deployed page can be diagnosed without a config change. |
| `useBeacon` | `false` | Leave off unless advised — it puts the token in a URL, which lands in proxy and CDN access logs. |

## API

Script-tag installs call these on `EdgeDelta`; bundler installs import them by name.

| Call | Use it for |
|---|---|
| `setUser({ id, email, name })` | Attach a user to everything sent afterwards. `setUser({})` clears on logout. |
| `setContext({ attributes })` | Merge custom attributes. An `undefined` value removes a key. |
| `addError(err, options?)` | An error you already caught. Uncaught ones need no call. |
| `addMessage(text, options?)` | A log record with no exception attached. |
| `addBreadcrumb({ type, message })` | Add to the trail attached to later errors. |
| `startView(route?)` / `endView(route?)` | Drive or name views yourself. |
| `use(...middleware)` | Rewrite or drop events and HTTP requests. |
| `flush()` | Send buffered errors now. |

Only pass fields you are willing to store — they land in queryable telemetry. When one
broken endpoint produces a different message every time, pass a `fingerprint` in
`addError`'s options to group them, rather than letting each become its own issue.

Details, plus views, breadcrumbs and middleware: [reference/api.md](reference/api.md).

## Lock the token to your origins

The token ships in your page source and anyone can read it. That is expected — it
grants nothing but the ability to send data. Constrain it on the ingestion node
rather than trying to hide it, and never put an Edge Delta **API** token here.

```yaml
- name: rum_ingest
  type: http_ingestion_input
  allowed_origins: [https://acme.com, "https://*.acme.com"]
```

A wildcard covers one whole leading label, so `*.acme.com` allows `a.b.acme.com` but
not `acme.com` itself — list the apex separately. Ports and schemes must match
exactly. Empty means any origin.

Setting this makes the token **browser-only**: requests with no `Origin` header are
refused, since that is what a stolen token looks like. Use a separate ingestion node
if the same pipeline also takes server-side traffic.

## Reference

| File | Covers |
|---|---|
| [reference/script-tag.md](reference/script-tag.md) | Script-tag install, placement rules, Subresource Integrity, CSP |
| [reference/bundler.md](reference/bundler.md) | npm install, the Vite plugin, env vars, loader modes |
| [reference/react.md](reference/react.md) | React render errors — boundary, root and router paths |
| [reference/testing.md](reference/testing.md) | Asserting on what your app reported |
| [reference/api.md](reference/api.md) | Views, breadcrumbs, middleware, flushing, ignoring noise |
| [reference/tracing.md](reference/tracing.md) | Joining browser traces to backend traces |
| [reference/querying.md](reference/querying.md) | Attributes to query, and what to alert on |
| [reference/troubleshooting.md](reference/troubleshooting.md) | Symptom → cause, starting with "no data at all" |

**When testing, leave the page.** The `documentLoad` span is sent on hide, so a tab
you never switched away from has sent nothing yet. Errors are not held back this way.
