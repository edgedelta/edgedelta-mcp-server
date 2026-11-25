# Troubleshooting

Start with `debug: true`, which logs the whole pipeline to the console. An
`ed_rum_debug` key in `localStorage` switches it on too, so a deployed page can be
diagnosed without a config change.

## No data at all

**First, leave the page.** The page-load span is sent on hide, so a tab you never
switched away from has sent nothing yet. Errors are not held back this way.

Then look in the network tab for a `POST` to `/otel/v1/traces` (page loads) or
`/otel/v1/logs` (errors):

| What you see | Cause |
|---|---|
| `404` | `endpoint` already ends in a path such as `/traces`. The UI lists full URLs for Edge Delta's own formats; this SDK sends OTLP and appends the path itself, so give it scheme and host only — or, if you proxy, the prefix your proxy serves at. |
| `401` | Token is wrong or disabled. |
| `403` | `allowed_origins` does not include this origin. Compare it exactly, including scheme and port. |
| No request at all | `init` never ran, the script was blocked, or an ad blocker ate it. See below. |
| An SRI error in the console | A pinned `integrity` hash does not match the file, so the browser blocked the script before it ran. |

## A Content Security Policy blocks it

`script-src` has to allow `https://js.edgedelta.com` for the bundle, and `connect-src`
your `endpoint` host. A script-tag install's loader file is same-origin, so
`script-src 'self'` already covers it — which is the reason for not inlining it.
Either block shows up as a CSP violation in the console and as nothing in Edge Delta.

## Data from some users but not others

Ad blockers block RUM scripts and ingestion domains by default, which typically costs
10–30% of traffic and looks like a quiet shortfall rather than an error. To recover
it, serve the bundle from your own domain and set `endpoint` to a first-party path
that proxies to Edge Delta.

## Other symptoms

| Symptom | Explanation |
|---|---|
| Error stacks are minified | Expected today — source-map support is not shipped yet. Set `version` now so stacks can be symbolicated retroactively once it lands. |
| Frontend traces aren't linked to backend traces | Needs `traceparent` propagation, which requires your API to accept and expose the header via CORS. Not automatic — see [tracing.md](tracing.md). |
| Vitals missing on some page loads | Normal. CLS and INP are only known once a visitor interacts and the page is hidden, so a bounce can produce a page-load span with no INP. |
| No route-change views in an SPA | The SDK opens a view only on a **path** change. A query-string rewrite is not a page view. |
| Nothing during local development | `http://localhost:3000` is a distinct origin — add it to `allowed_origins`, or use a pipeline node without the restriction. |
