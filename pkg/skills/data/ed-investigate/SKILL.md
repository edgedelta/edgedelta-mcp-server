---
name: ed-investigate
description: Cross-signal incident investigation workflow - from alert to root cause using events, patterns, logs, metrics, traces and pipeline history.
metadata:
  version: "1.0.0"
  author: edgedelta
  repository: https://github.com/edgedelta/agent-skills
  tags: edgedelta,incident,investigation,rca,oncall,debugging
  alwaysApply: "false"
---

# Edge Delta Incident Investigation

A structured workflow for investigating production incidents with `edx`.
Work top-down: events → patterns → logs/metrics/traces → change correlation.

## Prerequisites

The `edx` CLI must be installed and authenticated. See the **ed-edx** skill.

## 0. Establish the Window

- Paged incidents have exact start/end timestamps - use `--from/--to`
  (ISO 8601: `2026-06-12T10:00:00.000Z`).
- Verbal reports ("since this morning") - use a generous `--lookback` and
  narrow once the onset is visible.

## 1. What Fired? (Events)

```bash
edx events search --from <start> --to <end> --output table
edx events search -q 'event.domain:("Monitor" OR "Monitor Alerts")' --lookback 2h
```

Note affected services and event types. Monitor alerts carry the monitor ID:
`edx monitors get <id>` shows the exact condition.

## 2. What Changed in the Logs? (Patterns)

```bash
edx patterns list --summary --from <start> --to <end>
edx patterns list --negative -q 'service.name:"<svc>"' --offset 24h
```

New or surging negative patterns are your primary suspects. Pull raw lines:

```bash
edx logs search -q '"<distinctive token from pattern>"' --from <start> --to <end>
```

## 3. Quantify (Metrics & Log Graphs)

```bash
edx logs graph -q 'severity_text:"ERROR"' --group-by service.name --lookback 6h
edx metrics query --name <latency/error metric> --agg avg --group-by service.name
```

Identify exactly when the regression started - this timestamp drives step 5.

## 4. Localize (Traces & Service Map)

```bash
edx service-map --lookback 1h
edx traces search -q 'status.code:"500" AND service.name:"<svc>"' --include-children
```

Find the deepest failing span: that service/dependency is the likely root
cause; everything upstream is blast radius.

## 5. Correlate With Changes (Pipeline & Deploys)

Root causes are usually changes. Check what changed right before onset:

```bash
edx pipelines history <pipeline-id> --output table --columns timestamp,author,status,description
edx fleet deployments
```

Compare change timestamps with the regression onset from step 3. Match the
change content to the failure (e.g. live-capture errors → capture-related
changes). Name the top offending change (top two if unsure) and say why.

## 6. Verify / Mitigate

- Pipeline config suspected: roll back -
  `edx pipelines deploy <pipeline-id> <last-good-version> --yes`, then re-run the
  step 3 graph to confirm recovery.
- Processor behavior suspected: live-capture it -
  `edx capture start <pipeline-id> --nodes <node> --duration 2m`.

## Reporting

Summarize: window, affected services, the evidence chain (event → pattern →
metric onset → failing span), the correlated change with author and timestamp,
and the mitigation taken or proposed.
