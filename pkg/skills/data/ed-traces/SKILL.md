---
name: ed-traces
description: Distributed traces - search OTel spans, follow traces, service dependency map.
metadata:
  version: "1.0.0"
  author: edgedelta
  repository: https://github.com/edgedelta/agent-skills
  tags: edgedelta,traces,apm,spans,otel,service-map
  alwaysApply: "false"
---

# Edge Delta Traces

Search distributed traces (OpenTelemetry spans) and the service map.

## Prerequisites

The `edx` CLI must be installed and authenticated. See the **ed-edx** skill.

## Rules

- Trace queries require `field:"value"` CQL syntax. **Full-text search is NOT
  supported** and will error.
- Common fields: `service.name`, `status.code`, `span.kind`, `trace_id`,
  `ed.tag`. Discover more: `edx facets keys --scope trace`.
- `status.code` holds the HTTP status code (`200`, `404`, `500`, ...), not a
  span-status word. Query server errors as `status.code:"500"` (or another
  5xx); `status.code:"ERROR"` matches nothing. Enumerate live values with
  `edx facets options --scope trace --facet status.code`.

## Search Spans

```bash
# Server-error (5xx) spans, last hour
edx traces search -q 'status.code:"500"' --lookback 1h

# Server-side spans of one service with full trace context
edx traces search -q 'service.name:"checkout" AND span.kind:"server"' --include-children

# Everything in one trace
edx traces search -q 'trace_id:"<id>"' --include-children
```

`--include-children` pulls child spans of matches so you can see where time
went inside the request.

## Service Map

The service dependency graph built from traces - which services call which,
with health/latency context:

```bash
edx service-map --lookback 1h
```

Use it early in an investigation to find blast radius: an erroring service's
upstream callers are affected, its downstream dependencies are suspects.

## Typical RCA Flow

1. `edx traces search -q 'status.code:"500"'` - find failing spans.
2. Pick a `trace_id`, fetch the whole trace with `--include-children`.
3. Identify the deepest failing span - that service is the likely root cause.
4. Cross-check that service's logs:
   `edx logs search -q 'service.name:"<svc>" AND severity_text:"ERROR"'`.

## Empty Results Checklist

1. Remove full-text terms - traces only accept `field:"value"`.
2. Verify values: `edx facets options --scope trace --facet service.name`.
3. Widen `--lookback`; traces may be sampled.
