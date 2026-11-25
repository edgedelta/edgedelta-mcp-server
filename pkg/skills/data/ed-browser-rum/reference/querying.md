# Querying and alerting

## Two signals, two places

Errors are logs rather than spans because a browser error has no span to attach an
exception to, and as logs they get pattern grouping and full-text search over messages
and stacks.

To get from an error to its page load, use the trace context. Every error log record
carries the `trace_id` of the view it happened in, so the log drawer shows a
**Search in Traces** link that lands on that `documentLoad` span. Errors thrown before
the page finished loading also carry its `span_id`. `session.id` and `url.*` are on
both signals, which is how you widen from one error to everything else that visitor
did.

## Attributes

| Attribute | Notes |
|---|---|
| `service.name` | Your `service` value. |
| `session.id` | Groups a visitor's page loads and errors. |
| `trace_id` (logs) / `trace.id` (spans) | Shared by everything one view emits — the join between an error and its page load. |
| `url.path`, `url.domain` | Query strings and fragments are stripped, since they often carry tokens and PII. |
| `ed.rum.route` | The route pattern, when the router supplied one via `startView` / `endView`. |
| `ed.rum.lcp`, `.cls`, `.inp`, `.fcp`, `.ttfb` | Core Web Vitals. Each has a matching `.rating` of `good`, `needs-improvement` or `poor`. |
| `ed.rum.dns_ms`, `.tcp_ms`, `.tls_ms`, `.request_ms`, `.response_ms` | Network phases of the page load. |
| `ed.rum.dom_interactive_ms`, `.dom_content_loaded_ms` | Rendering milestones, relative to navigation start. |
| `ed.rum.navigation_type`, `ed.rum.transfer_size` | e.g. `navigate`, `reload`, `back_forward`; bytes over the wire. |
| `ed.rum.mechanism` | Which React path reported an error — see [react.md](react.md). |
| `ed.rum.breadcrumbs` | JSON array of the last 20 events before the error. |
| `browser.*`, `user_agent.original` | Browser, platform and mobile flag. |
| `user.id` | Present once `setUser` is called. |

## Spans are flat by default

The load phases are attributes on the `documentLoad` span rather than nested child
spans, so query them as fields instead of expecting a waterfall. `navigationSpans:
true` adds the children — `dnsLookup`, `connect`, `tls`, `request`, `response`,
`domProcessing`, `loadEvent` — for anyone who wants one, but the attributes stay
either way and remain what you query. The children carry none.

## Alerting

Build monitors over the **trace** data for performance — LCP p75 by `url.path`, say.
Error-rate monitors go over the **log** data instead, filtered to `exception.type`
being present.
