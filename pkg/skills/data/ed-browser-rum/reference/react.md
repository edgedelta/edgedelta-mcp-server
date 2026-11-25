# React error reporting

`@edgedelta/browser-rum/react` reports React render errors, which no global listener
sees — React catches them itself.

```tsx
import { createRoot } from "react-dom/client";
import { ErrorBoundary, rumRootOptions } from "@edgedelta/browser-rum/react";

createRoot(node, rumRootOptions()).render(
  <ErrorBoundary fallback={({ eventId, resetError }) => <Oops id={eventId} onRetry={resetError} />}>
    <App />
  </ErrorBoundary>
);
```

## Three reporting paths

Told apart by `ed.rum.mechanism`:

| Mechanism | Comes from | Severity |
|---|---|---|
| `react.boundary` | this module's `ErrorBoundary` | `ERROR` — a subtree died |
| `react.root` | `rumRootOptions()`, for errors no boundary caught | `FATAL` — the page died |
| `react.router` | `reportRouteError`, for a router's own per-route catch boundary | `ERROR` |

The third one matters more than it looks. A router that renders its own catch boundary
intercepts route render errors before any boundary you placed above the outlet can see
them, so without it an entire route can crash invisibly. Wire it to whatever hook the
framework offers — TanStack Router's `defaultOnCatch`, for instance.

`rumRootOptions` deliberately does not set React's `onCaughtError`: `ErrorBoundary`
reports those itself, and setting both would report every caught error twice.

## Fallback and event id

`fallback` takes an element or a render function receiving
`{ error, componentStack, eventId, resetError }`. The `eventId` is the
`ed.rum.error_id` on the reported event, so a user can quote it and someone can find
that exact error. It is not a trace id — one view can produce several boundary errors.

## Per-error policy

`classify` returns nothing for the errors that should take the defaults, which is the
common case — a classifier exists to name exceptions.

```tsx
const classify = (error: unknown) =>
  isStaleChunkLoad(error) ? { severityNumber: SEVERITY.WARN } : undefined;
```

It can override `severityNumber` and add `attributes`, and is accepted by
`ErrorBoundary`, `rumRootOptions` and `reportRouteError` alike.
