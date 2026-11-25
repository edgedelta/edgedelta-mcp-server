---
name: ed-metrics
description: Metrics - discover metric names and run aggregation queries (timeseries and tables).
metadata:
  version: "1.0.0"
  author: edgedelta
  repository: https://github.com/edgedelta/agent-skills
  tags: edgedelta,metrics,timeseries,aggregation
  alwaysApply: "false"
---

# Edge Delta Metrics

Discover and aggregate metrics.

## Prerequisites

The `edx` CLI must be installed and authenticated. See the **ed-edx** skill.

## Rules That Prevent Failed Queries

1. **Metric names must be EXACT** - no wildcards, no regex, case-sensitive.
   Always discover the name first.
2. **No full-text search** in metric filters - `field:"value"` syntax only.
3. Group-by keys must be real fields - check with
   `edx facets keys --scope metric`.

## Discover Metric Names

```bash
edx metrics list                    # all metric names
edx metrics list --keyword cpu      # fuzzy filter
```

## Query a Metric

```bash
# Average request duration per service, last hour
edx metrics query --name http.request.duration --agg avg --group-by service.name

# Max CPU on one host over 24h with 5-minute rollups
edx metrics query --name system.cpu.usage --agg max \
  --filter 'host.name:"web-1"' --rollup 300 --lookback 24h

# Table instead of timeseries (current values, good for top-N)
edx metrics query --name http.requests --agg sum \
  --group-by service.name --graph-type table
```

Aggregations: `sum`, `avg`, `min`, `max`, `count`, `median`.
Filter: CQL field syntax, `"*"` for none.

Under the hood this builds the CQL `agg:name{filter} by {keys}.rollup(secs)`
and returns records keyed by formula, e.g. `{"A": {"records": [...]}}`.

## Groupable Dimensions (Important)

Metric dimensions are a **fixed indexed allowlist** (`service.name`, `host.ip`,
`ed.*`, `k8s.*`, ...) - discover it with `edx facets keys --scope metric`.
Grouping by anything else (an OTLP datapoint attribute like `model`/`type`, or a
custom `log_to_metric` field dimension) **silently returns one empty group**
rather than an error; `edx metrics query` warns when a `--group-by` key is not
indexed. For attribute-level breakdowns, query the underlying **logs** (which
carry every attribute) or emit a dedicated `log_to_metric` per breakdown.

## Common Investigations

| Question | Command |
|----------|---------|
| Is CPU/memory elevated? | `edx metrics query --name system.cpu.usage --agg avg --group-by host.name` |
| Which service has the most errors? | `edx metrics query --name <error-count-metric> --agg sum --group-by service.name --graph-type table` |
| Did latency regress after deploy? | `edx metrics query --name http.request.duration --agg avg --from <deploy-time> --to <now>` |

## Empty Results Checklist

1. Verify the metric name exactly: `edx metrics list --keyword <part>`.
2. Verify filter values: `edx facets options --scope metric --facet service.name`.
3. Widen `--lookback`.
4. Remove `--filter` (use `"*"`), then add filters back one at a time.
