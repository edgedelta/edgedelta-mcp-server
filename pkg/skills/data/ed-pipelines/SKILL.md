---
name: ed-pipelines
description: Pipelines - fleet management, config changes, version history, deployments and live capture.
metadata:
  version: "1.0.0"
  author: edgedelta
  repository: https://github.com/edgedelta/agent-skills
  tags: edgedelta,pipelines,fleet,agents,deploy,live-capture
  globs: "**/edgedelta*.yaml,**/pipeline*.yaml"
  alwaysApply: "false"
---

# Edge Delta Pipelines

Manage fleets of Edge Delta agents: pipeline configurations, version history,
deployments, agent health and live capture.

## Prerequisites

The `edx` CLI must be installed and authenticated. See the **ed-edx** skill.

## Concepts

- A **pipeline** (conf) is an agent configuration with server-side version
  history. A **fleet** is the set of agents running that pipeline.
- Changing a pipeline is two steps: `save` creates a new version, `deploy`
  rolls a version out to the fleet. Deploying an older version is the
  supported rollback.

## First-time onboarding

Use **ed-onboard** to discover customer resources, choose a direct collection path,
install agents and verify source telemetry. For a new edge pipeline:

```bash
edx pipelines create --file pipeline.yaml --tag my-service --environment Docker
```

The initial configuration is deployed, but no agent is installed. Resolve installation
with `edx pipelines deploy-command <conf-id>`. Kubernetes requires
`--environment Kubernetes --fleet-subtype Edge` (or Gateway/Coordinator).
After uninstalling agents, `edx pipelines delete <conf-id> --yes` removes the config.
These commands require an edx build exposing `create/delete`; inspect `--help`.

## Fleet Visibility

```bash
edx pipelines list --output table --columns id,tag,fleet_type,environment,status,updated
edx pipelines agents <pipeline-id>        # agents running this pipeline
edx pipelines status <pipeline-id>        # running / suspended
edx fleet agents                      # all agents org-wide
edx fleet deployments                 # rollout status across pipelines
edx health problems                   # components currently failing
```

## Config Change Workflow

```bash
# 1. Fetch the current config (content field holds the YAML)
edx pipelines get <pipeline-id> > pipeline.json
jq -r .content pipeline.json > pipeline.yaml

# 2. Edit pipeline.yaml (to develop the transform itself, see the ed-pipeline-tuning skill)

# 3. Dry-run the change on sample logs before saving (offline, no deploy)
edx pipelines test ottl <pipeline-id> --file samples.jsonl --statements '<ottl>'

# 4. Validate before saving
edx pipelines validate --file pipeline.yaml

# 5. Save a new version with a meaningful description
#    (save prints the new version - the epoch-ms timestamp deploy expects)
edx pipelines save <pipeline-id> --file pipeline.yaml -d "mask PII in checkout logs"

# 6. Deploy. --latest deploys the newest saved version; --wait blocks until
#    agents check in after the rollout and fails if one never does.
edx pipelines deploy <pipeline-id> --latest --wait --yes

# 7. Watch the rollout
edx fleet deployments <pipeline-id>
```

The deploy **version** is a saved version's epoch-millisecond timestamp, shown as
the `version` column of `edx pipelines history`. Use `--latest` to skip the
lookup, or pass an explicit version to roll forward/back. (`--latest`/`--wait`
require `edx` >= 0.10.0.)

## OTLP and Other Push Sources

An OTLP source ingests over the network. One `otlp_input` gRPC node on a port
receives **all signals** (logs, metrics and traces) - a single node multiplexes
them, and the per-signal `data_type` field is deprecated:

```yaml
- name: my_otlp
  type: otlp_input
  port: 4317
  protocol: grpc        # or http
```

Point the sender (e.g. an OpenTelemetry SDK/collector, or Claude Code with
`CLAUDE_CODE_ENABLE_TELEMETRY=1`) at `http://<agent-host>:<port>`.

> **A new source whose port is already bound fails the ENTIRE pipeline graph, not
> just that node.** The agent stops, its heartbeat freezes, and no data flows.
> Deploy with `edx pipelines deploy --wait` so a frozen heartbeat is reported
> immediately; if the agent was already up, it must be restarted to bind the new
> port. Pick a free, non-ephemeral port (below the OS ephemeral range, ~49152+).

## Attached multiprocessors and processor names

Include an attached multiprocessor for every application source and destination,
even when it has no processors. Use a separate `type: sequence` node named exactly
`<source-or-destination-node-name>_multiprocessor`. Link the source immediately to
its multiprocessor and the destination multiprocessor immediately to its destination.
The Visual Builder recognizes attachment by this exact name and adjacency; a
standalone sequence in the middle does not replace either attachment. Empty attached
sequences may omit `processors` (as the default pipelines do) or use `processors: []`.
Preserve default direct routing of agent self-telemetry and internal statistics.

Place source-specific parsing/enrichment in the source attachment and destination-wide
processing in the destination attachment. Add intermediate stages only when needed.
When editing an existing pipeline, preserve its routing and processing semantics;
do not duplicate or bypass existing transforms when introducing attachments.

Every processor needs a concise display name describing its purpose, including Custom
OTTL processors. For nested sequence processors, set `name` inside the JSON-encoded
`metadata` string, preserving other metadata keys. A nested YAML `name` or
`user_description` does not set this display name. Never leave it blank or as "Custom".
For top-level nodes, use descriptive node names and `user_description` for display.

```yaml
links:
- from: my_otlp
  to: my_otlp_multiprocessor
- from: my_otlp_multiprocessor
  to: edgedelta_multiprocessor
- from: edgedelta_multiprocessor
  to: edgedelta

nodes:
- name: my_otlp
  type: otlp_input
  port: 4317
  protocol: grpc
- name: my_otlp_multiprocessor
  type: sequence
  processors:
  - type: ottl_transform
    metadata: '{"name":"Set telemetry source"}'
    data_types: [log, metric]
    statements: |-
      set(resource["telemetry.source"], "my_app")
- name: edgedelta_multiprocessor
  type: sequence
- name: edgedelta
  type: ed_output
  user_description: Edge Delta
```

Before validation/save, check exact attachment names and link direction, retained
self-telemetry routing, and meaningful `metadata.name` values for nested processors.

## Redacting PII (OTTL recipes)

Push sources (OTLP especially) put identifiers in **attributes and resource, not
`body`** - so field-level redaction beats body regexes. Scrub structured fields,
and keep body regexes as defense-in-depth:

```yaml
- type: ottl_transform
  metadata: '{"name":"Redact identity fields"}'
  data_types: [log, metric]
  statements: |-
    delete_key(attributes, "user.email")
    set(resource["host.ip"], "[REDACTED_IP]") where EDXCoalesce(resource["host.ip"],"")!=""
- type: ottl_transform
  metadata: '{"name":"Mask email addresses in log body"}'
  data_types: [log]
  statements: |-
    replace_pattern(body, "[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\\.[a-zA-Z]{2,}", "[REDACTED_EMAIL]")
```

Verify with live capture (below): compare `before`/`after` on the multiprocessor
and confirm the PII fields are gone.

## Investigating Config Changes (Who Broke It?)

```bash
edx pipelines history <pipeline-id> --output table --columns version,timestamp,author,status,description
```

Correlate the deploy timestamps with the incident start. Roll back by
deploying the last good version:

```bash
edx pipelines deploy <pipeline-id> <last-good-version> --yes
```

## Live Capture (Debug Data In-Flight)

Sample real data before/after pipeline nodes on the running agents - the
fastest way to verify a processor or find where data is dropped:

```bash
# 1. Start a capture (all nodes, or scope with --nodes)
edx capture start <pipeline-id> --duration 2m --nodes mask_pii --max-items 50
# response contains the task "id"

# 2. Poll agent pickup status
edx capture status <task-id>

# 3. Fetch the captured before/after samples
edx capture results <pipeline-id>
```

Each node reports `before` and `after` arrays: compare them to verify
transformations, filters and routing. Captured items are **JSON-encoded
strings** - decode with jq's `fromjson`:

```bash
edx capture results <pipeline-id> | jq '[.[].nodes[].after[]] | map(fromjson)'
```

`--max-items` is capped at 100 per node.

### Tailing a capture

`--follow` polls and prints one line of compact JSON per newly captured item,
already decoded - use it instead of re-running `results` and diffing by hand:

```bash
edx capture start <pipeline-id> --duration 10m --nodes mask_pii
edx capture results <pipeline-id> --follow --param nodes=mask_pii
# {"timestamp":...,"source":"<agent>","node":"mask_pii","phase":"before","item":{...}}
edx capture results <pipeline-id> --follow --output raw | jq -r .body   # items only
edx capture results <pipeline-id> --follow --since-now  # skip the backlog
```

Notes:

- It tails results only; it does not keep the capture alive. Once the
  `capture start` task expires the stream goes quiet, so re-arm it. edx warns on
  stderr at startup when no task is active (an expired task reads the same way -
  the API stops serving it).
- A capture can be armed and still produce nothing: a node with no traffic
  reports empty `before`/`after` arrays. After a minute with no new item, edx
  notes the silence on stderr (`no new items in the last 1m0s (12 polls)`), so a
  quiet tail is never mistaken for a broken one. A tail that is producing items
  stays quiet - the items are their own evidence.
- The first poll prints everything currently held (can be hundreds of lines).
  `--since-now` gives `tail -f` semantics instead: the backlog is swallowed and
  only items captured after the tail starts are printed. Either way,
  `--param nodes=<a,b>` and `--param limit_per_source=1` narrow the stream.
- Transient poll failures are logged to stderr and retried; five consecutive
  failures exit non-zero, so a broken tail never looks like an idle pipeline.
- Only `--output json` (default) and `raw` work while following.

## Troubleshooting

| Problem | Fix |
|---------|-----|
| Save rejected | Run `edx pipelines validate --file` for the error detail |
| Agent wedged / heartbeat frozen after deploy | A new source's port is already bound (fails the whole graph). Deploy with `--wait`; check `edx health problems` and agent logs; free the port or restart the agent |
| Agents not picking up deploy | `edx fleet deployments <pipeline-id>`; check agent connectivity |
| Capture returns nothing | Confirm node names (`edx pipelines get`), extend `--duration`, ensure traffic flows |
| Data missing downstream | Live-capture the node chain to find where items drop |
