---
name: ed-monitors
description: Monitors - create, manage, snooze and resolve Edge Delta monitors and alerts.
metadata:
  version: "1.0.0"
  author: edgedelta
  repository: https://github.com/edgedelta/agent-skills
  tags: edgedelta,monitors,alerts,alerting
  alwaysApply: "false"
---

# Edge Delta Monitors

Create, inspect, update and delete monitors; view triggered/resolved states.

## Prerequisites

The `edx` CLI must be installed and authenticated. See the **ed-edx** skill.

## Inspect

```bash
edx monitors list --all --output table # ALL monitors (plain list caps at the server's 50)
edx monitors get <monitor-id>          # full definition: query, thresholds, notifications
edx monitors states --all              # triggered/resolved states, complete
edx monitors states -q 'monitor.status:"alert"'
```

`monitors list` and `monitors states` are cursor-paginated like every list
command (**ed-edx** > Pagination): without `--all`, the bare list returns one
page of the server default 50, so a larger estate silently undercounts.

## Create / Update

Monitor definitions are JSON. The reliable workflow is **clone-and-edit**:

```bash
# 1. Fetch an existing monitor of the same type as a template
edx monitors get <id> > monitor.json

# 2. Edit name, query, thresholds, notification targets

# 3. Create (or update in place)
edx monitors create --file monitor.json
edx monitors update <id> --file monitor.json
```

Monitor queries use the same CQL as search commands - validate the query
first by running it:

```bash
edx logs graph -q 'severity_text:"ERROR" AND service.name:"api"' --lookback 1h
```

If the graph returns sensible numbers, the monitor query will too.

## Metric Threshold Monitors

A `metric_threshold` monitor carries its query in `formula_query` (scope
`metric`) plus a window and thresholds:

```json
{
  "name": "API Errors",
  "type": "metric_threshold",
  "evaluation_type": "sliding_window",
  "evaluation_function": "sum",
  "evaluation_window": 900,
  "threshold_type": "above",
  "warning_threshold": 3,
  "alert_threshold": 10,
  "no_data_behavior": "show_no_data",
  "formula_query": {
    "formula": "A",
    "queries": {"A": {"scope": "metric",
                      "query": "sum:api.error.count{service.name:\"api\"}.rollup(60)"}}
  }
}
```

**Evaluation is not real-time.** A freshly created monitor showing "No Data" is
normal until its next scheduled evaluation. To confirm the query resolves and
would (or wouldn't) fire right now, dry-run it (requires `edx` >= 0.10.0):

```bash
edx monitors evaluate <monitor-id>   # prints value vs thresholds + ALERT/WARNING/OK
```

## Delete

```bash
edx monitors delete <monitor-id> --yes
```

## Alert Triage

When an alert fires:

1. `edx monitors get <id>` - what condition fired? what query?
2. `edx events search -q 'event.domain:("Monitor" OR "Monitor Alerts")' --lookback 2h` -
   correlated alerts around the same time?
3. Run the monitor's underlying query yourself with a wider window to see
   the trend (`edx logs graph` / `edx metrics query`).
4. Pivot to the **ed-investigate** skill for full root-cause analysis.

To see which monitors fire most, sweep monitor events over a wide window
(**ed-events**) and count per monitor. Monitor identity in event records is
under `resource`; `attributes` carries the evaluation detail
(`ed.monitor.evaluated.value`, `ed.monitor.query`, ...):

```bash
edx events search -q 'event.domain:("Monitor" OR "Monitor Alerts")' \
  --lookback 720h --all --output-file alerts-30d.json
jq -r '.items[].resource | .["ed.monitor.id"] + "  " + .["event.name"]' \
  alerts-30d.json | sort | uniq -c | sort -rn | head -20
```

## Good Practices

- Alert on symptoms (error rate, latency) not causes (CPU).
- Scope queries with `service.name`/`ed.tag` to keep alerts actionable.
- Bounded group-by keys only - high-cardinality grouping creates alert storms.
