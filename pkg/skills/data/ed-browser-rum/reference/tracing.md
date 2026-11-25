# Joining browser traces to backend traces

With `requestSpans: true` the SDK sends a `traceparent` header, so the span your
server records becomes a child of the request span and one trace covers the click and
the query behind it. Your backend needs no new code: reading `traceparent` is the
default in every OpenTelemetry SDK and in the Datadog, Elastic and Dynatrace agents.

| Target | What it takes |
|---|---|
| Same-origin API | nothing — no CORS is involved, so nothing can break |
| Cross-origin API | `traceparent` in that server's `Access-Control-Allow-Headers`, **then** the host in `propagateTo` |
| The page load itself | a `Server-Timing` header on the document response |

## Cross-origin

Deploy the CORS change **before** listing the host. `traceparent` is not a
CORS-safelisted header, so adding it makes the request preflighted — and a server that
does not allow it rejects the preflight, which means the request never leaves the
browser. This fails hard, not quietly.

A string entry matches the whole host; there are no wildcards, since an entry matching
more than it names hands the app's trace topology to a third party. For a family of
hosts, or a rule the app already holds, pass a `RegExp` or a predicate:

```ts
propagateTo: [/\.acme\.com$/, host => ourServices.has(host)];
```

**Anchor the pattern.** Unanchored, `/acme\.com/` also matches `evil-acme.com`. The
same three forms work in `ignoreErrors`, where a plain string matches a substring
instead. `propagateTo: false` disables propagation entirely.

## The page load, which cannot propagate

No script is running when the browser asks for the HTML, so there is nothing to
propagate from. Have that response return its own trace instead, and the
document-load span will link to it:

```
Server-Timing: traceparent;desc="00-<trace-id>-<span-id>-01"
```

Both `traceparent` and `trace` are read as metric names. A cross-origin response also
needs `Timing-Allow-Origin` for the browser to expose the value at all.
