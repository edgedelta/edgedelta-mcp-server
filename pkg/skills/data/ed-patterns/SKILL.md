---
name: ed-patterns
description: Log patterns - clustered message signatures with counts, deltas and sentiment for anomaly hunting.
metadata:
  version: "1.0.0"
  author: edgedelta
  repository: https://github.com/edgedelta/agent-skills
  tags: edgedelta,patterns,clustering,anomaly,logs
  alwaysApply: "false"
---

# Edge Delta Log Patterns

Edge Delta clusters similar log messages into **patterns**. Each pattern
carries `count`, `proportion`, `sentiment` (positive/negative/neutral) and
`delta` (change vs an earlier window). Patterns are the fastest way to answer
"what's new or surging in the logs?" without reading raw lines.

## Prerequisites

The `edx` CLI must be installed and authenticated. See the **ed-edx** skill.

## Quick Start

```bash
# ~50 interesting clusters: top anomalies, biggest delta up/down, top counts
edx patterns list --summary --lookback 1h

# Negative-sentiment patterns only (errors, failures, timeouts)
edx patterns list --negative --lookback 1h

# Scoped to one service
edx patterns list -q 'service.name:"api"' --negative --limit 20
```

## Comparing Windows (Delta)

The `delta` stat compares against an earlier window. Default offset equals the
lookback. To compare against the same window yesterday:

```bash
edx patterns list --lookback 1h --offset 24h
```

A large positive delta on a negative-sentiment pattern is a strong incident
signal: a new or surging error signature.

## Getting Raw Samples

Once a suspicious pattern is found, fetch the raw log lines behind it. The most
reliable way is to search logs for a distinctive token from the signature
(patterns render variable parts as `*`, so pick the literal words):

```bash
edx logs search -q '"connection refused"' --lookback 1h
```

`edx patterns samples` returns raw lines too, but it filters only by `-q` and
time - the `--param pattern=` value is currently ignored by the backend (the
same lines come back regardless), so it cannot isolate a single pattern. Use it
to pull recent lines for a service, and use the `logs search` token approach
above to read lines for one specific pattern:

```bash
edx patterns samples -q 'service.name:"api"' --lookback 1h
```

## Interpretation Guide

| Signal | Meaning |
|--------|---------|
| New pattern, high count | New failure mode or new deploy behavior |
| delta >> 0, negative sentiment | Surging error - investigate first |
| delta << 0 on normal traffic patterns | Possible traffic drop / outage |
| High proportion shift | Behavior change even if totals look flat |

## Empty Results Checklist

1. Widen `--lookback`.
2. Verify filters: `edx facets options --scope pattern --facet service.name`.
3. Drop `--negative` (the issue may be neutral-sentiment).

If a service's patterns are a single `*` (or hundreds of near-identical ones),
its logs are structured JSON or `key=value` clustered on the raw body. The fix
is a pipeline change; see "Patterns from structured logs" in **ed-pipeline-tuning**.
