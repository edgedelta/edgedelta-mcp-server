---
name: ed-events
description: Events - pattern anomalies, monitor alert triggers and Kubernetes events.
metadata:
  version: "1.0.0"
  author: edgedelta
  repository: https://github.com/edgedelta/agent-skills
  tags: edgedelta,events,anomalies,alerts,kubernetes
  alwaysApply: "false"
---

# Edge Delta Events

Events are the "what happened" stream: anomaly detections, monitor alert
triggers and Kubernetes events. Always check events early in an incident -
they often point straight at the cause.

## Prerequisites

The `edx` CLI must be installed and authenticated. See the **ed-edx** skill.

## Event Types

| Query | Meaning |
|-------|---------|
| `event.type:"pattern_anomaly"` | Log anomaly detections |
| `event.type:"metric_threshold"` | Metric alert triggers |
| `event.type:"log_threshold"` | Log alert triggers |
| `event.domain:("Monitor" OR "Monitor Alerts")` | All monitor-triggered events |
| `event.domain:"K8s"` | Kubernetes events (OOMKilled, BackOff, ...) |

Discover the live set: `edx facets options --scope event --facet event.type`
and `--facet event.domain`.

## Search Events

```bash
# All anomalies in the last 6 hours
edx events search -q 'event.type:"pattern_anomaly"' --lookback 6h

# Anomalies for one service
edx events search -q 'service.name:"api" AND event.type:"pattern_anomaly"'

# Everything monitors fired recently, as a table
edx events search -q 'event.domain:("Monitor" OR "Monitor Alerts")' --output table

# Kubernetes trouble
edx events search -q 'event.domain:"K8s" AND OOMKilled' --lookback 24h
```

Full-text search is supported in the event scope (bare words work).

## Counting events over a long window

**A single page is a lower bound, never a total** - the default `--limit` is
20, so an unqualified 30-day search returns 20 events and a `next_cursor`.
The full pagination contract (cursor semantics, `--all`, retries, resume) is
in **ed-edx** > Pagination; the sweep itself:

```bash
# Every monitor alert in the last 30 days, complete (requires edx >= 0.20.0)
edx events search -q 'event.domain:("Monitor" OR "Monitor Alerts")' \
  --lookback 720h --all --output-file alerts-30d.json
# stderr: page 1: 1000 item(s), 1000 total
#         page 2: 1000 item(s), 2000 total
#         ...
jq '.total_items, .pages' alerts-30d.json
```

Monitor events use `event.domain` `"Monitor"` or `"Monitor Alerts"` depending
on the org and data age; the disjunction covers both. Do not guess domain
values - a wrong one returns zero rows, not an error. List the live set first:
`edx facets options --scope event --facet event.domain`.

Paging by hand works too - pass the previous response's `next_cursor` back
with the query and time flags identical:

```bash
edx events search -q 'event.domain:("Monitor" OR "Monitor Alerts")' \
  --lookback 720h --limit 1000
# -> "next_cursor": "PP-FAwEBBmN1cnNvcg..."   (opaque - pass it back verbatim)
edx events search -q 'event.domain:("Monitor" OR "Monitor Alerts")' \
  --lookback 720h --limit 1000 --cursor 'PP-FAwEBBmN1cnNvcg...'
# repeat until "next_cursor": ""
```

There is no server-side cap on the limit and no maximum time window for event
search, so a sweep is bounded only by the result set itself.

## Incident Usage

1. Establish the incident window (from the page/alert).
2. `edx events search --from <start> --to <end>` - what fired in the window?
3. Pattern anomalies name the service and signature - pivot to
   `edx patterns list` / `edx logs search` for detail.
4. Monitor alerts carry the monitor ID - `edx monitors get <id>` for the
   query and thresholds behind it.
