# Pipeline write & validation tools for the Edge Delta MCP server

**Date:** 2026-07-08
**Status:** Approved design, pending implementation plan
**Author:** Fatih Yildiz (with Claude)

## Problem

The MCP server can *read* pipelines and *deploy* an existing version, but it
cannot *change* a pipeline's configuration or *validate* a config before
writing it. An SRE/support agent driven by Claude Code can therefore produce
grounded pipeline recommendations (restore dropped severity, stop deleting
structured fields, re-enable the traces path, add processors/destinations) but
has **no tool to apply them**. This is the blocker reported in the Slack thread
(`thread-implement-extract-and-aggregate-separately-…`): *"SRE agent can't find
a tool to use to implement the changes."*

The `edx` CLI already exposes the full loop — `list > get > save > deploy` plus
`validate` — against these backend endpoints. The MCP server implements only a
subset.

### Current state

Existing pipeline tools (`pkg/tools/pipelines.go`, registered in
`server/server.go`):

- `get_pipelines`, `get_pipeline_config`, `get_pipeline_history` (read)
- `deploy_pipeline` — `POST /pipelines/{conf_id}/deploy/{version}`
- `add_pipeline_source` — `POST /pipelines/{conf_id}/add_source` (source node only)

Notably, `pkg/tools/client.go` already defines a `SavePipeline(ctx, client,
confID, description, pipeline, content)` data-layer function that hits
`POST /pipelines/{conf_id}/save` — but **no MCP tool wraps it**, so it is
currently dead code.

### Backend endpoints (source of truth)

From `edgedelta/backend/admin/service_v1.go` and the `edx` CLI:

| Purpose | Method + path | Request body | Response |
|---|---|---|---|
| Validate a config (no save) | `POST /v1/orgs/{org}/confs/validate` | `{"content": "<yaml>"}` | `core.ConfigValidationResult` → `{"valid": bool, "reason": string}` |
| Save a new version (no deploy) | `POST /v1/orgs/{org}/pipelines/{conf_id}/save` | `{"content": "<yaml>", "description": "<note>"}` (`pipeline.SaveRequest`, content XOR pipeline) | saved version metadata |
| Deploy a saved version | `POST /v1/orgs/{org}/pipelines/{conf_id}/deploy/{version}` | none | — (already wrapped) |
| Roll back to a version | `POST /v1/orgs/{org}/pipelines/{conf_id}/revert/{version}` | none | — |

The **only** dedicated node-level write endpoint is `add_source` (plus
`/extend` for sources). There is no `add_processor` / `add_destination`
endpoint. Full-content `save` is the general mechanism for every other change.

## Goal

Close the agent's edit loop end to end:

```
get_pipeline_config(conf_id)             read current YAML
        (agent edits the YAML)
validate_pipeline_config(content)        pre-flight: {valid:true} before writing
save_pipeline_config(conf_id, content,   create a new version — NOT deployed
                     description)
get_pipeline_history(conf_id)            obtain the new version timestamp
deploy_pipeline(conf_id, version)        roll out to the fleet
        (rollback if needed)
revert_pipeline(conf_id, oldVersion)     roll back
```

This mirrors the safe "validate → save → deploy w/ rollback" path.

## Non-goals (YAGNI)

- **No client-side YAML graph mutators.** No `add_pipeline_processor` /
  `add_pipeline_destination`. The backend has no endpoint for them, and
  full-content `save_pipeline_config` already covers every such change. An LLM
  edits the YAML from `get_pipeline_config` directly.
- No changes to `add_pipeline_source` or `deploy_pipeline`.
- No pipeline create/delete, fleet suspend/resume, or metadata tools in this
  iteration.

## Design

All new code follows the existing conventions in `pkg/tools/pipelines.go`:
the `PipelineToolResponse` / `PipelineGuidance` wrapper, `FetchContextKeys`,
`applyAuthHeader`, `client.APIURL()`. Three new tools are added and registered
in `server/server.go` immediately after the existing pipeline tools.

### Tool 1: `validate_pipeline_config`

- **Endpoint:** `POST /v1/orgs/{org}/confs/validate`
- **Params:** `content` (string, required) — full pipeline YAML.
- **Annotations:** readOnly ✅, idempotent ✅, destructive ❌, openWorld ❌.
- **Behavior:** POST `{"content": content}`. Decode the backend
  `ConfigValidationResult`. A `{"valid": false, "reason": "..."}` response is a
  **successful tool call** (not an error): return it with guidance to fix the
  config and re-validate. Only transport/non-2xx failures are tool errors.
- **Guidance NextSteps:** on valid → "call `save_pipeline_config` to persist
  this config"; on invalid → "fix the reported issue and re-run
  `validate_pipeline_config`".
- **Data layer:** new `ValidatePipelineConfig(ctx, client, content) (map[string]any, error)`
  in `client.go`, following the `SavePipeline` shape.

### Tool 2: `save_pipeline_config`

- **Endpoint:** `POST /v1/orgs/{org}/pipelines/{conf_id}/save`
- **Params:** `conf_id` (string, required), `content` (string, required — full
  YAML), `description` (string, optional — shown in version history).
- **Annotations:** readOnly ❌, idempotent ❌, destructive ❌ (creates a new
  version but does not deploy; nothing is overwritten), openWorld ❌.
- **Behavior:** reuse the **existing** `SavePipeline` data-layer function
  (`content` path; `pipeline` arg passed empty). Returns saved-version metadata.
- **Guidance NextSteps:** "config saved as a new version but NOT deployed →
  call `get_pipeline_history` to get the version timestamp → call
  `deploy_pipeline` to roll out." Also recommend running
  `validate_pipeline_config` first if not already done.

### Tool 3: `revert_pipeline`

- **Endpoint:** `POST /v1/orgs/{org}/pipelines/{conf_id}/revert/{version}`
- **Params:** `conf_id` (string, required), `version` (string, required —
  timestamp from `get_pipeline_history`).
- **Annotations:** readOnly ❌, idempotent ❌, destructive ✅, openWorld ❌.
- **Behavior:** POST with no body. Complements `deploy_pipeline` as an explicit
  rollback affordance.
- **Data layer:** new `RevertPipeline(ctx, client, confID, version) (map[string]any, error)`
  in `client.go`.
- **Guidance NextSteps:** "reverted → verify with `get_pipeline_config` /
  `get_pipeline_history`."

## Data flow

1. Tool handler calls `FetchContextKeys(ctx)` for `OrgID` + auth.
2. Builds the URL from `client.APIURL()` + `/v1/orgs/{org}/…`.
3. Marshals the JSON body (validate/save) or none (revert).
4. `applyAuthHeader(req, keys)`; `client.Do(req)`.
5. Non-2xx → tool error including status + response body (existing pattern).
6. Wraps the decoded result in `PipelineToolResponse{Data, Guidance}` and
   returns via `mcp.NewToolResultText`.

## Error handling

- Missing required params → `mcp.NewToolResultError(...)`.
- Transport errors / non-2xx → returned as Go errors with status + body, exactly
  as the existing pipeline tools do.
- `validate` `valid:false` → **not** an error; returned as a successful result
  with the `reason` and corrective guidance.

## Testing

The repo currently has **no** Go tests. This work introduces the first ones for
`pkg/tools`:

- Table-driven handler tests using `httptest.NewServer` to stub the three
  endpoints.
- A test `Client` implementation whose `APIURL()` returns the stub server URL;
  context seeded with `ContextKeys` (org id + token) via the same mechanism
  `FetchContextKeys` reads.
- Assertions: correct HTTP method, path (`/v1/orgs/{org}/confs/validate`,
  `/pipelines/{conf_id}/save`, `/pipelines/{conf_id}/revert/{version}`), request
  body JSON, and that the wrapped response contains the expected `guidance`
  chaining. Cover: happy path, `valid:false` validate, and non-2xx error.

## Documentation

Add the three tools to the tool list in `README.md`.

## Files touched

- `pkg/tools/client.go` — add `ValidatePipelineConfig`, `RevertPipeline`
  (reuse existing `SavePipeline`).
- `pkg/tools/pipelines.go` — add `ValidatePipelineConfigTool`,
  `SavePipelineConfigTool`, `RevertPipelineTool`.
- `server/server.go` — register the three tools.
- `pkg/tools/pipelines_test.go` (new) — handler tests.
- `README.md` — document the new tools.
