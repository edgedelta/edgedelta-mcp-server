# API in depth

Script-tag installs call these on `EdgeDelta`; bundler installs import them by name.
Both forms queue safely before `init`.

## Context

`setContext` merges, so a page can enrich in stages — user at login, org at org switch
— without re-sending what it already set. A key passed as `undefined` is removed.
`setUser` is a wrapper for the `user.*` conventions; `setUser({})` clears it.

```ts
setContext({ attributes: { "org.id": orgId } });
setContext({ resource: { "service.namespace": "web" } });
```

## Views

A view is one trace: the document load, then one per route change. Each carries its
own span, its own CLS and its own INP, and errors reported while it is open name its
trace. Views are joined to each other by `session.id`, not by trace id — a 40-minute
single-page session is not one trace.

Detection needs no framework code and no monkeypatching: the SDK listens on the
Navigation API's `currententrychange`, which fires for `history.pushState` — note that
`navigate`, the event most examples reach for, does not. Browsers without the
Navigation API fall back to wrapping `history`. Only a change of **path** opens a
view, so a router rewriting the query string on every filter change costs nothing.

What detection cannot know is the *route pattern* and when the transition actually
finished. Both default to something reasonable — the pathname, and the first paint
after the URL commits — and `endView` replaces both when the router knows better:

```ts
router.subscribe("onResolved", ({ toLocation }) => endView(toLocation.routeId));
```

That reports `ed.rum.route` as `/orgs/$orgId/logs` rather than `/orgs/42/logs`, so the
spans group, and measures the transition to the point the data resolved instead of to
the first frame that happened to paint. Span names stay `documentLoad` and
`routeChange`, so a span-name index never has to hold every URL the app can make.

To drive views entirely by hand, set `routeChanges: false` and call
`startView("/orgs/$orgId/logs")`, which ends the open view and opens a named one.

A route change's span covers the transition; its vitals cover the whole time the view
was open, which is longer — a layout shift ten seconds in still belongs to the route
the user was looking at.

## Breadcrumbs

Errors and messages carry `ed.rum.breadcrumbs`: a JSON array of the last 20 things
that happened, newest last. Clicks, `fetch` calls and `console.error` are recorded
automatically; add your own with `addBreadcrumb({ type, message, data })`.

Click descriptors are structural only — `button#save.btn[data-test-id=save-button]`.
Text content, input values and `aria-label` are never read, because this string leaves
the browser attached to every error the page reports afterwards.

The trail is a ring buffer per document, so it costs nothing on page views that never
fail. It is stamped on before `beforeSend` runs, so middleware can redact or drop it
like any other attribute. `XMLHttpRequest` is not wrapped, so a page whose traffic is
XHR sees no `http` crumbs.

## Middleware

```ts
use({
  // Drop or redact an event on its way into the queue. `beforeSend` is one of these.
  event: (event, next) => { if (isNoise(event)) return; next(event); },
  // Wrap the HTTP request: add headers, route through a first-party proxy, drop it.
  send: (request, next) => next({ ...request, url: toFirstParty(request.url) }),
});
```

Handlers run in registration order. Call `next` to continue the chain with a value —
possibly a changed one — or return without calling it to drop the event or abandon the
request. Everything is synchronous on purpose: the last chance to touch a payload
happens while the page is unloading, where there is nothing to await with.

A `send` handler sees the final `url`, `headers` and `body`, plus the `events` the
body was built from and whether this is the page-end flush. Batch splitting happens
above the chain, so a handler sees each request as it will actually go out.

## Ignoring known-noisy errors

`ignoreErrors` takes substrings, `RegExp`s and predicates. Matched against the log
record's body and, for errors, `exception.type` — **not** the stack: nearly every
stack in a bundled app names the framework, so matching it turns one common word into
a silent outage. Spans are never matched, so a pattern meant for an error message
cannot cost you a page view.

Strings survive JSON, which is the point — a script-tag install can pass a list
through its config instead of needing a `beforeSend` closure in the page. It runs
ahead of `beforeSend`, so that escape hatch only sees what survived.

## Flushing

`flush()` sends queued errors and messages. `flush(true)` adds the open view's span,
held back for final vitals — that is what `visibilitychange` and `pagehide` call
internally. Reach for it directly on a page that never unloads: a test harness, or a
single-page app tearing down a view it will not return to.

## Fingerprinting

When one broken endpoint produces a different message every time, group them instead
of letting each become its own issue:

```ts
addError(err, { fingerprint: `HTTP ${status} ${route}` });
```
