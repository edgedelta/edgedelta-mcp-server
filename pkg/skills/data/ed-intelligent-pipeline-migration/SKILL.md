---
name: ed-intelligent-pipeline-migration
description: Use when moving an Edge Delta pipeline or fleet (including cloud/hosted fleets) from the Go agent to the Rust (RED) agent, when agents crash-loop or stop reporting after switching agent builds, or when someone asks whether a pipeline "behaves the same" on the new agent.
metadata:
  version: "0.1.0"
  author: edgedelta
  repository: https://github.com/edgedelta/agent-skills
  tags: edgedelta,red,rust,migration,parity,pipelines,hosted-agents
  globs: "**/edgedelta*.yaml,**/pipeline*.yaml"
  alwaysApply: "false"
---

# Migrating pipelines from the Go agent to RED

**A RED agent that starts is not a migrated pipeline.** Prove it by sending the
same bytes through both agents and diffing the outputs. There are three stages.
Run them in order, and don't move on while the current one is failing.

| Stage | Proves | Tool |
| --- | --- | --- |
| 1. Preflight | RED can build every node in the config | the RED binary, offline |
| 2. Side by side | Same input → same log and metric records on every output | `scripts/sidebyside.py` |
| 3. Shadow fleet | Real traffic, traces, heartbeats | second fleet + `edx` per tag |

## Prerequisites

- The pipeline YAML: `edx pipelines get <pipeline-id> | jq -r .content > pipeline.yaml`
  (see **ed-pipelines**).
- The Go agent and RED binaries for the versions you are comparing.
- Python 3 with PyYAML.

## 1. Preflight: let RED reject the config

```bash
ED_BACKEND_DISABLED=1 red --config pipeline.yaml   # Ctrl-C after "pipeline running"
```

A startup error is a blocker; record it verbatim. So is a warning starting
with `node type not implemented`. RED starts when it logs this, but that node
drops everything sent to it.

Two results are caused by running offline, not by RED:
- With no `ED_API_KEY`, RED logs `ed_output disabled: ED_API_KEY is not set`,
  and then the `node type not implemented` warning for the `ed_output` node.
  Ignore both for `ed_output`.
- A `lookup` with an `ed://` location fails with "needs the Edge Delta
  backend". To check it, download the table (`edx lookup download <name> --out
  <file>`), point `location_path` at `file://<file>` in both agents' configs,
  and include the lookup in stage 2.

The Go agent accepts these config shapes, and RED preflight has rejected them
before:

- a `sequence` or `compound` inside another sequence's `processors:` list.
  Top-level `sequence` and `compound` nodes are a different case.
- `telemetrygen_input` with `template_type: metric` or `trace`
- `ed_debug_output`, which RED starts with but warns about as above

Preflight can pass and the data can still differ. A sequence processor with
`final: true` started cleanly on RED, but RED ran the processors after it and
the Go agent did not. Give every `final: true` processor a fixture in stage 2.

Timestamps from parsed JSON need `Int()` on the Go agent. There, `ParseJSON`
returns numbers as float64, and `set(timestamp, attributes["timestamp"])`
stores the float. The Go agent reads the timestamp as an integer when it ships
the item, so it sends 0 and the backend drops the log. RED is not affected.
Convert first:
`set(attributes["timestamp"], Int(attributes["timestamp"])) where attributes["timestamp"] != nil`.
Stage 2 reports this as `bad_timestamp`.

## Translating legacy Go nodes

RED runs OTTL nodes only. The control plane still accepts legacy Go nodes on a
RED pipeline, and RED drops their data or refuses to start. Run the translator
when stage 1 reports `node type not implemented` for a legacy node, or rejects
a CEL `route`:

```bash
python3 scripts/translate.py pipeline.yaml migrated.yaml
```

It rewrites `log_transform`, `generic_transform`, `resource_transform`,
`regex_filter`, `log_level_detector`, `parse_json_attributes`, CEL `route`,
`log_to_pattern`, `log_to_metric` and `output_transform` into OTTL nodes, and
prints a JSON summary of what it changed. Run stage 1 again on `migrated.yaml`,
then pass it to stage 2 as `--red-config`. A translated pipeline counts as
verified only after stage 2 passes on it with a negative control.

The script never guesses. Any form outside its rules stops it with
`<node>: unsupported <what>: <expression>`, and it writes nothing. Rewrite that
node by hand in OTTL with **ed-pipeline-tuning**, then run the script again.
Rejected forms include `convert_timestamp`, CEL macros, `ec2_metadata`,
`from_k8s`, field paths outside `item["attributes"]` and `item["resource"]`,
`log_to_metric` with `dimension_groups`, and `regex_filter` with `field_path`.

`log_to_metric` becomes a filter, `extract_metric` and `aggregate_metric`
writing `<name>.count`. It loses:
- `<name>.anomaly1`, and any monitor or dashboard built on it
- zero-count intervals: the Go agent reports 0 for a quiet interval, RED
  emits nothing
- wall-clock buckets: RED buckets by item timestamp
- first-log labels: RED keeps only attributes identical across the bucket
- the `_stat_type` and `__metric_category` metadata

Before migrating a pipeline with `log_to_metric`, search the output of
`edx monitors list --all` and of `edx dashboards get <id>` for each dashboard
for `.anomaly1`. If a monitor or dashboard uses it, stop and agree on a
replacement first.

Add these fixtures to stage 2 for translated nodes:
- a log missing each field a transform or route reads
- a log with that field set to an explicit null: CEL `has()` counts it, OTTL
  `!= nil` does not
- a string body on any route that indexes `body`
- a log on each side of every `regex_filter`

## 2. Side by side: same bytes, diff the outputs

```bash
python3 scripts/sidebyside.py --config pipeline.yaml \
  --go-bin ./edgedelta --red-bin ./red \
  --feed http_in=logs.ndjson --feed otlp_in=signals.otlp.json \
  --out ./parity/run1 [--red-config migrated.yaml] [--settle 75]
```

The script writes `go.yaml` and `red.yaml`. In each one:
- every input named by `--feed` becomes a local receiver: `http_input` for line
  files, `otlp_input` for `*.otlp.json` (one OTLP/HTTP JSON request per line)
- every output becomes an `http_output` to a local capture server
- inputs you don't feed are dropped and listed in `dropped_inputs`

It posts the same bytes to both agents, waits `--settle` seconds, and stops
both with SIGTERM. Then it diffs the records per output. Each agent's own
listener port is replaced with `<port:NAME>`, because inputs stamp it into
resource fields such as `server.port`. Exit 0 = match, 1 = diff,
2 = harness failure.

**Traces are not compared.** `http_output` sends no traces on either agent, so
the script refuses OTLP fixtures with `resourceSpans`. Compare traces in
stage 3.

**Set `--settle` longer than the longest `interval` or `reporting_frequency`**
in the pipeline (`aggregate_metric`, `log_to_pattern_metric`, `dedup`,
`suppress`, `rate_limit`). On a 1-minute `aggregate_metric` with a 5-second
settle, RED emitted the open window at shutdown and the Go agent emitted
nothing. A 70-second settle matched.

Only `timestamp` is ignored by default, because `http_input` stamps each log
with its arrival time. Use `--no-default-ignore` when the fixtures carry their
own timestamps (OTLP). Use `--ignore <field>` only for nondeterminism you can
explain, never to make a diff go away.

Both agents run offline with every inherited `ED_*` variable removed. The Go
agent also needs `ED_COORDINATOR_CLIENT_DISABLED=1`, or it never opens its
inputs; the script sets it. The Go agent's log is `<out>/go/edgedelta.log`.

Before trusting a pass, run a negative control: a `--red-config` with one
processor removed must produce exactly the expected `only_*` records.

Cover every live branch in the fixtures: each route path, input that matches a
filter and input that doesn't, null/missing/wrong-type fields, Unicode, and
every log and metric shape the pipeline carries.

Reading `report.json`:
- `only_go` / `only_red`: records on one side only, with samples.
  `fields_only_*` lists keys that appear on one side only.
- `value_diffs`: pairs `only_go` and `only_red` records on `--pair-by`
  (default `body`) and counts the fields whose values differ. Read it first
  when every record differs; it usually names a single field. For example,
  after `merge_maps` into `attributes` the Go agent sends
  `resource.server.port` as null and RED keeps the port.
- `bad_timestamp`: per agent, records whose timestamp was not an integer when
  it reached the output. Any count fails the run. The script finds them with
  a probe in front of each output, because the captured JSON prints such a
  float the same way as an integer.
- A `status` other than `ok` means nothing was compared. If the Go agent
  captured nothing on any output, it may have replaced an output it could not
  build with a no-op; that is logged as an error in `<out>/go/edgedelta.log`.

## 3. Shadow fleet: real traffic and heartbeats

Stage 2 runs both agents with the backend disabled, so it never exercises the
backend. Before switching a fleet, run RED next to the Go agent on the same
config with a distinct tag, and send it a copy of production traffic. Compare
logs, metrics and traces per tag with **ed-logs**, **ed-metrics** and
**ed-traces** over the same closed window.

Fleet views find agents through the `heartbeat` metric, grouped by
`ed.agent.identifier`. List every pod reporting for the config:

```bash
edx metrics query --name heartbeat --agg max --graph-type table --lookback 15m \
  --filter 'ed.conf.id:<conf-id>' --group-by ed.agent.identifier,ed.agent.type
```

Every RED pod must appear the same way the Go pods do. In Kubernetes the
identifier is the pod name.

Cloud fleets also match heartbeats on the metric attribute `ed.env.id`. Run the
query again with `--filter 'ed.conf.id:<conf-id> @ed.env.id:"<cloud-fleet-id>"'`.
Quote the value: `@ed.env.id:*` is rejected as an invalid query. A RED pod that
appears in the first query but not in this one is missing `ed.env.id`, and that
blocks the switch.

## Report

For each pipeline, record:
- both agents' versions
- the preflight result
- the side-by-side command and the fixture list
- per-output counts and diffs
- the shadow window and the queries used

List what was **not** tested. Traces are never covered by stage 2. Classify
each node as verified, translated but unverified, intentionally changed, or
blocked. For writing OTTL, see **ed-pipeline-tuning**.

## Common mistakes

| Mistake | Instead |
| --- | --- |
| "RED pod is Running, migration done" | Running only clears stage 1. |
| Comparing only through backend queries | Run stage 2 first. It shows the individual records that differ. |
| A settle shorter than a processor interval | Settle past the longest `interval` / `reporting_frequency`. |
| Treating a stage 2 match as covering traces | Compare traces in stage 3. |
| Adding the diffing field to `--ignore` | Find out why it differs, then fix it or record a blocker. |
| Translating legacy nodes by hand | Run `scripts/translate.py`. Hand-write only the nodes it rejects. |
