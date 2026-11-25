---
name: ed-ai-teammate
description: AI Teammate - manage connectors (PagerDuty, Slack, GitHub, ...), update teammates (agents), view teammate activity, query the knowledge graph and inspect/run AI workflows.
metadata:
  version: "1.0.0"
  author: edgedelta
  repository: https://github.com/edgedelta/agent-skills
  tags: edgedelta,ai,teammate,connectors,workflows
  alwaysApply: "false"
---

# Edge Delta AI Teammate

The AI Teammate ingests signals from connected tools (PagerDuty, Slack,
GitHub, ...) and acts on them. This skill manages those **connectors**,
inspects teammate **activity** and **workflows**, and queries the
**knowledge graph**.

## Prerequisites

The `edx` CLI must be installed and authenticated. See the **ed-edx** skill.

## Inspect

```bash
edx ai connectors list             # configured connectors
edx ai connectors specs            # available connector types + required fields
edx ai connectors environments     # where connectors can run
edx ai activity --lookback 24h     # teammate activity metrics
edx ai agents list                 # AI Teammates (agents); alias: edx ai teammates
edx ai agents get <agent-id>       # a single teammate's full definition
```

AI-service lists (`issues list/threads`, `channels list/messages`,
`threads list/messages`, `workflows list`, `workflows runs list`) are
cursor-paginated with a top-level `nextCursor`; all take `--cursor` and
`--all` (**ed-edx** > Pagination).

## Update a Teammate (Agent)

`edx ai agents update` prompts for confirmation unless `--yes`.

### Just the prompts (the common case)

Use the `--*-prompt` flags — each takes an inline string or `@file` (`@-` for
stdin). The command reads the current teammate, backfills whichever prompt you
did not pass (the service requires both `masterPrompt` and `userPrompt` on every
update), and sends only the prompts. Model, temperature, tools and everything
else are left untouched, so you never deal with model-tuning validation:

```bash
edx ai agents update <id> --master-prompt @master.md --user-prompt @user.md
edx ai agents update <id> --master-prompt "You are a concise SRE assistant."
```

### Any field (clone-and-edit)

Like `edx monitors update`: fetch, edit the JSON, apply it back.

```bash
edx ai agents get <agent-id> > agent.json      # edit fields under "data":
                                               # model, toolConfigurations,
                                               # connectors, priority, ...
edx ai agents update <agent-id> --file agent.json
```

The whole `get` envelope is accepted and unwrapped automatically. Only the
fields you include are changed; a field set to `null` is cleared. Use
`-f`/`--file -` to read from stdin. Note: re-sending the full object re-validates
model-tuning fields (`model`, `modelTemperature`) even if untouched — to change
only prompts prefer the flags above, or send just the prompt fields:

```bash
edx ai agents get <id> | jq '.data | {masterPrompt,userPrompt,toolingPrompt}' \
  | edx ai agents update <id> --file - --yes
```

## Knowledge Graph (read-only)

The AI Teammate builds a knowledge graph from the connected tools: services,
repos, teams, people, channels, incidents, documents and the edges between
them (OWNED_BY, DEPENDS_ON, RUNS_ON, MONITORED_BY, DISCUSSED_IN, TRACKED_IN,
AFFECTS, DOCUMENTS). It is the org's top-down map — use it to answer
"who owns X", "what depends on X", "what should we monitor" before digging
into logs and metrics.

```bash
edx ai knowledge stats                    # node/edge counts by type + source, last sync
edx ai knowledge topology --limit 500     # graph slice: nodes, edges, stats
edx ai knowledge search "payment" --types Service   # find entities by name/alias
edx ai knowledge search --query "payment" --types Service   # same thing
edx ai knowledge get <entity-id>          # one entity + neighbors + edges
edx ai knowledge subgraph <entity-id> --hops 2      # N-hop neighborhood
edx ai knowledge blast-radius <entity-id> # downstream impact if it fails
edx ai knowledge criticality --limit 20   # most-depended-on entities
```

- Entity IDs are `{orgId}::{type}::{externalId}` — always find them with
  `search` first; quote them (they contain `::` and may contain `/`).
- Node types: Org, Integration, Service, Repo, Channel, JiraProject,
  PagerDutyService, AwsResource, Team, Person, Incident, Document.
- `search` takes the query either positionally or as `--query`/`-q` — the two
  are equivalent (requires `edx` >= 0.20.0; older builds are positional-only
  and reject `--query` with `unknown flag`). Giving both is an error.
- Filter flags: `--types` (csv), `--min-confidence` (0..1), `--source`,
  `--namespaces topology,learned`. Search is cursor-paginated (`nextCursor`
  inside `data`): pass it back as `--cursor`, or sweep with `--all`
  (**ed-edx** > Pagination).
- `topology` on a large org returns megabytes — use `--output-file` and parse
  the file rather than stdout (see **ed-edx** > Large outputs).
- All commands are read-only; graph writes happen via connector sync, not edx.

Workflow examples:

```bash
# What should this org monitor? Rank by dependency, then check coverage.
edx ai knowledge criticality --limit 20
edx monitors list --all

# Who owns a service and where is it discussed?
edx ai knowledge search "ingestion" --types Service
edx ai knowledge get "<id>"        # OWNED_BY team, DISCUSSED_IN channels

# Impact analysis before a risky change
edx ai knowledge blast-radius "<id>" --max-hops 3
```

Caveat: `criticality` returns `"basis": "degree"` when the org has few
dependency edges; blast-radius quality grows with the graph's DEPENDS_ON /
RUNS_ON / USES coverage.

## Workflows (read + run)

AI Team workflows are node graphs (Start, Task, Action, If/Else, Transform,
Wait) built in the web app under AI Team > Workflows. edx can inspect them,
read their run history and trigger manual runs; editing the graph
(create/update/delete, revisions, deploy) stays in the web app.

```bash
edx ai workflows list --output table --columns workflowId,displayName,status
edx ai workflows get <workflow-id>          # node graph is a JSON string in "content"
edx ai workflows runs list <workflow-id>    # run history, newest first
edx ai workflows runs get <workflow-id> <execution-id>
edx ai workflows runs steps <workflow-id> <execution-id>   # per-node step records
edx ai workflows run <workflow-id> --input '{"alert":"cpu high"}'
```

- `run` triggers a manual run and streams progress as one JSON line per event
  until the run finishes; it exits non-zero when the run reports an error.
  Runs can take minutes — Ctrl-C stops watching, the run itself keeps going
  server-side.
- **A run executes the workflow's real actions** (emails, channel messages,
  Jira/PagerDuty updates). Confirm with the user before triggering one.
- `--input` takes inline JSON, a plain string, or `@file` / `@-` (stdin);
  default `{}`. Nodes reference it as `data` in their templates.
- Run stats shown in the web app (total runs, success rate, avg runtime) are
  metrics, not an API: query `ed.oncall.ai.workflow_execution.count` and
  `ed.oncall.ai.workflow_execution.duration` with `edx metrics query`.

## Add or Update a Connector

1. Find the connector type and its required fields:

```bash
edx ai connectors specs --output json | jq '.[] | select(.type=="pagerduty")'
```

2. Build the request JSON per the spec (type, name, credentials/settings).

3. Apply it:

```bash
edx ai connectors update --file connector.json
```

Connector data flows through an ingestion pipeline that Edge Delta provisions
automatically; check it with `edx pipelines list --keyword ai`.

## Remove a Connector

The delete request body identifies the connector (same shape as update):

```bash
edx ai connectors delete --file connector.json --yes
```

## Troubleshooting

| Problem | Fix |
|---------|-----|
| Connector not ingesting | `edx pipelines list --keyword ai` then `edx health problems` |
| Unknown required fields | `edx ai connectors specs` is the source of truth |
| Credential errors | Re-apply with `edx ai connectors update --file` and fresh secrets |
| Teammate update rejected (4xx) | Re-fetch with `edx ai agents get <id>`, edit only the `data` fields, re-apply |
| Workflow run fails with "Workflow not active" | The workflow's status is `inactive`; activate it in the web app |
