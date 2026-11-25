# Testing an app that uses RUM

`@edgedelta/browser-rum/testing` gives you a recorder to stand in for the SDK, so
tests can assert on what your app reported without a transport making real requests.

```ts
import { mock } from "bun:test";                       // or vi.mock / jest.mock
import { createRumRecorder } from "@edgedelta/browser-rum/testing";

export const rum = createRumRecorder();
mock.module("@edgedelta/browser-rum", () => rum.module);
```

Install it once, from wherever your runner loads setup files, and call `rum.reset()`
from a `beforeEach`. Installing a module replacement is the one thing every runner
does differently, so that call stays yours — the recorder itself is runner-agnostic.

Then read the lists:

| | |
|---|---|
| `rum.errors`, `rum.messages` | `{ subject, options }` per `addError` / `addMessage` |
| `rum.breadcrumbs` | `{ type, message, data }` |
| `rum.contexts`, `rum.users` | each `setContext` / `setUser` payload |
| `rum.startedViews`, `rum.endedViews` | route names |
| `rum.initialized`, `rum.flushes` | config passed to `init`; flush calls |

```ts
expect(rum.errors[0].options?.severityNumber).toBe(SEVERITY.WARN);
```

`rum.module` covers the whole public API, so a module importing something the recorder
does not track still gets a function rather than `undefined`, and it re-exports the
real `SEVERITY` rather than a copy.
