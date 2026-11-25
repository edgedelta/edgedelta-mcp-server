# Verification

Scope evidence to the onboarded resource and a known recent time window. Verify actual
source activity; synthetic receiver traffic alone cannot establish source coverage.
For an authorized fixture, use a unique marker/service identity and known expected data.
For customer workloads, use their existing identities and observable activity instead.

Build source-specific queries from discovered fields. For example, define SOURCE_QUERY
and METRIC_FILTER as appropriate CQL filters and METRIC_NAME as a discovered metric:

```bash
edx --profile "$ED_PROFILE" logs search -q "$SOURCE_QUERY" --lookback 30m --limit 10
edx --profile "$ED_PROFILE" metrics query --name "$METRIC_NAME" \
  --agg max --filter "$METRIC_FILTER" --lookback 30m
edx --profile "$ED_PROFILE" traces search -q "$SOURCE_QUERY" --lookback 30m --limit 10
```

Discover indexed fields with `edx facets keys` when adapting queries. Metrics must have
non-null timeseries datapoints, not just a discovered name or a default zero aggregate.
Trace queries require field syntax.
Check source identity, timestamps and expected values; unrelated preexisting data is
not evidence. Bound polling (e.g. 10 minutes), retain failures, and distinguish ingestion
latency from a failed exporter. Consult agent health/live capture if data is missing.

Record each resource/signal as passed, failed, blocked, unsupported or not requested,
with the query, time window and evidence. A partial pass is not complete onboarding.
Honor retention instructions even when verification fails. Clean up only owned resources
when temporary cleanup is within scope; keep a durable inventory for interrupted runs.

## Locate the failing stage

- Deployment: verify the actual agent revision, IAM/RBAC, network access and source
  selection. Empty Kubernetes include lists can pass validation while collecting nothing.
- Source: use native capture/source counters to distinguish no source activity from an
  agent failing to receive it. Transport success alone is not an indexed-data check.
- Transformation: inspect after-processing records for source identity, timestamp,
  metric name/value/unit and unwanted duplicates. Use representative inputs with expected outputs before deployment.
- Indexing: query recent source-specific logs and actual metric timeseries. A real zero
  sample is valid; a zero aggregate without samples is not. Inspect child spans when
  verifying database-client instrumentation, separately from database engine telemetry.

Allow source/export buffering, agent flush intervals and indexing delay. Set a bounded
polling deadline appropriate to the path (for example ten minutes), retain evidence,
and diagnose the missing stage when it expires rather than recreating working resources.
Do not shrink result limits to work around truncated output: use edx --output-file.

If dry-run returns an API error, record it as an unavailable test, not a passing one.
Use native live capture within an authorized isolated test to validate behavior; if
neither works, report the transform as unverified. A capture 404/empty result can mean
no samples yet: inspect the capture task, expiry, agent check-in and source activity.

Verify steady-state ingestion while agents remain running. Do not stop or restart an
agent just to force a final flush, especially when the user requested retention. Earlier
shutdown-assisted smoke tests do not establish steady-state ingestion latency.

If validation creates incorrect test series, record their names and affected interval.
Use corrected, distinct test names or a clearly bounded clean interval for evidence;
do not present earlier invalid samples as passing data or silently erase that history.

## Transformation checks

Use existing edx processor previews with representative source records. Include both
accepted and rejected records; compare exact signal names, values/units, source identity,
original timestamps and output counts. Do not merely assert that some output exists.
If a runtime cannot preview the processor, verify native capture in an authorized test
and label the remaining uncertainty rather than inventing a local interpreter.

The Go agent runs exposed several pitfalls worth checking when applicable:
- Separate extract_metric condition entries are alternatives; combine required checks
  into one conjunction. For gauges, preserve measurement semantics rather than summing
  samples or treating a rate as a cumulative counter.
- Setting an arbitrary field can validate without changing the event's timestamp. Verify
  the actual timestamp, using the deployed runtime's supported field and units.
- A sequence may pass unmatched records through as logs despite `keep_item: false` on
  extraction. Inspect actual outputs and add an explicit data-type filter when the
  destination should receive only extracted metrics. Test unknown names and zero counts.
- Valid zero-valued measurements must survive filtering; missing measurements must not
  be fabricated as zeros. A batched source must preserve each event and its identity.

These are checks to adapt to the source/runtime, not mandatory processors for every pipeline.
