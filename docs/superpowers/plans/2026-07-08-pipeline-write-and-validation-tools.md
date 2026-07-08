# Pipeline Write & Validation Tools Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add `validate_pipeline_config`, `save_pipeline_config`, and `revert_pipeline` MCP tools so an agent can apply and verify pipeline configuration changes end to end.

**Architecture:** Each tool follows the existing `pkg/tools/pipelines.go` pattern — a thin MCP handler that calls a data-layer function in `pkg/tools/client.go`, hits an Edge Delta REST endpoint under `/v1/orgs/{org}/…`, and wraps the response in `PipelineToolResponse{Data, Guidance}`. Tools are registered in `server/server.go`. `save_pipeline_config` reuses the already-present-but-orphaned `SavePipeline` data-layer function; `validate` and `revert` get new data-layer functions.

**Tech Stack:** Go 1.23, `github.com/mark3labs/mcp-go@v0.43.1`, standard `net/http` + `net/http/httptest` for tests.

## Global Constraints

- Go module: `github.com/edgedelta/edgedelta-mcp-server`; Go 1.23.0.
- MCP library: `github.com/mark3labs/mcp-go` v0.43.1 (`mcp` and `server` packages).
- All endpoints are prefixed `client.APIURL() + "/v1/orgs/" + keys.OrgID`.
- Auth + org id come from `tools.FetchContextKeys(ctx)` and `tools.applyAuthHeader(req, keys)`.
- Success from the backend is HTTP 200; non-200 → return a Go error containing status code + response body (match existing tools verbatim in style).
- Every tool returns `mcp.NewToolResultText(string(json-of-PipelineToolResponse))`.
- Guidance chaining must reference the next tool by its exact registered name.

---

### Task 1: `validate_pipeline_config` tool + test harness

**Files:**
- Modify: `pkg/tools/client.go` (add `ValidatePipelineConfig`)
- Modify: `pkg/tools/pipelines.go` (add `ValidatePipelineConfigTool`)
- Modify: `server/server.go:61` (register tool)
- Test: `pkg/tools/pipelines_test.go` (create — includes shared test helpers)

**Interfaces:**
- Consumes: `tools.Client` interface, `FetchContextKeys`, `applyAuthHeader`, `PipelineToolResponse`, `PipelineGuidance` (all existing).
- Produces:
  - `func ValidatePipelineConfig(ctx context.Context, client Client, content string) (map[string]any, error)`
  - `func ValidatePipelineConfigTool(client Client) (mcp.Tool, server.ToolHandlerFunc)`
  - Test helpers (same file, reused by Tasks 2 & 3):
    - `func newTestClient(url string) *HTTPClient` → `NewHTTPClient(url, "")`
    - `func testCtx() context.Context` → context with `OrgIDKey="org123"`, `EDTokenKey="tok"`
    - `func callToolRequest(args map[string]any) mcp.CallToolRequest`
    - `func resultText(t *testing.T, r *mcp.CallToolResult) string`

- [ ] **Step 1: Write the failing test**

Create `pkg/tools/pipelines_test.go`:

```go
package tools

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// --- shared test helpers (reused by other pipeline tool tests) ---

func newTestClient(url string) *HTTPClient { return NewHTTPClient(url, "") }

func testCtx() context.Context {
	ctx := context.WithValue(context.Background(), OrgIDKey, "org123")
	return context.WithValue(ctx, EDTokenKey, "tok")
}

func callToolRequest(args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}}
}

func resultText(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if r == nil || len(r.Content) == 0 {
		t.Fatalf("expected content in result, got %+v", r)
	}
	tc, ok := r.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", r.Content[0])
	}
	return tc.Text
}

// --- validate_pipeline_config ---

func TestValidatePipelineConfigTool_Valid(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"valid":true}`))
	}))
	defer srv.Close()

	_, handler := ValidatePipelineConfigTool(newTestClient(srv.URL))
	res, err := handler(testCtx(), callToolRequest(map[string]any{"content": "nodes: []"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/v1/orgs/org123/confs/validate" {
		t.Errorf("path = %s, want /v1/orgs/org123/confs/validate", gotPath)
	}
	if !strings.Contains(gotBody, `"content":"nodes: []"`) {
		t.Errorf("body = %s, want content field", gotBody)
	}
	text := resultText(t, res)
	if !strings.Contains(text, `"valid":true`) {
		t.Errorf("result = %s, want valid:true", text)
	}
	if !strings.Contains(text, "guidance") {
		t.Errorf("result = %s, want guidance", text)
	}
}

func TestValidatePipelineConfigTool_Invalid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"valid":false,"reason":"unknown node type"}`))
	}))
	defer srv.Close()

	_, handler := ValidatePipelineConfigTool(newTestClient(srv.URL))
	res, err := handler(testCtx(), callToolRequest(map[string]any{"content": "bad"}))
	if err != nil {
		t.Fatalf("valid:false must not be a tool error, got: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "unknown node type") {
		t.Errorf("result = %s, want reason echoed", text)
	}
}

func TestValidatePipelineConfigTool_MissingContent(t *testing.T) {
	_, handler := ValidatePipelineConfigTool(newTestClient("http://unused"))
	res, _ := handler(testCtx(), callToolRequest(map[string]any{}))
	if res == nil || !res.IsError {
		t.Fatalf("expected error result for missing content, got %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/tools/ -run TestValidatePipelineConfigTool -v`
Expected: FAIL — `undefined: ValidatePipelineConfigTool`.

- [ ] **Step 3: Add the `ValidatePipelineConfig` data-layer function**

Append to `pkg/tools/client.go` (after `SavePipeline`, before `GetFacets`):

```go
func ValidatePipelineConfig(ctx context.Context, client Client, content string) (map[string]any, error) {
	keys, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	validateURL := fmt.Sprintf("%s/v1/orgs/%s/confs/validate", client.APIURL(), keys.OrgID)

	payload, err := json.Marshal(map[string]any{"content": content})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, validateURL, strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("failed to create validate request: %v", err)
	}

	req.Header.Add("Content-Type", "application/json")
	applyAuthHeader(req, keys)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to validate config, status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode validate response: %v", err)
	}
	return result, nil
}
```

- [ ] **Step 4: Add the `ValidatePipelineConfigTool` handler**

Append to `pkg/tools/pipelines.go` (after `AddPipelineSourceTool`):

```go
// ValidatePipelineConfigTool validates an agent config YAML without saving it.
func ValidatePipelineConfigTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("validate_pipeline_config",
			mcp.WithTitleAnnotation("Validate Pipeline Config"),
			mcp.WithDescription(`Validate an Edge Delta pipeline configuration (YAML) WITHOUT saving or deploying it.

Use this as a pre-flight check before save_pipeline_config. Provide the full pipeline YAML as 'content'.

Returns {valid: true} when the config is accepted, or {valid: false, reason: "..."} describing the problem.
A valid:false result is NOT an error — fix the config and call this tool again.

Typical workflow:
1. get_pipeline_config(conf_id) → read current YAML
2. (edit the YAML)
3. validate_pipeline_config(content) → confirm it is valid
4. save_pipeline_config(conf_id, content, description) → persist a new version`),
			mcp.WithString("content",
				mcp.Description("Full pipeline configuration YAML to validate."),
				mcp.Required(),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			content, err := request.RequireString("content")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: content"), nil
			}

			result, err := ValidatePipelineConfig(ctx, client, content)
			if err != nil {
				return nil, fmt.Errorf("failed to validate pipeline config, err: %w", err)
			}

			rawData, err := json.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal response, err: %w", err)
			}

			guidance := &PipelineGuidance{ResultStatus: "success"}
			if valid, _ := result["valid"].(bool); valid {
				guidance.NextSteps = []string{
					"Config is valid. Use save_pipeline_config with conf_id, content and a description to persist it as a new version.",
					"After saving, use get_pipeline_history then deploy_pipeline to roll it out.",
				}
			} else {
				guidance.ResultStatus = "invalid"
				guidance.NextSteps = []string{
					"Config is invalid. Read the 'reason' field, fix the YAML, and call validate_pipeline_config again.",
				}
			}

			r, err := json.Marshal(PipelineToolResponse{Data: rawData, Guidance: guidance})
			if err != nil {
				return nil, fmt.Errorf("failed to marshal wrapped response, err: %w", err)
			}
			return mcp.NewToolResultText(string(r)), nil
		}
}
```

- [ ] **Step 5: Register the tool**

In `server/server.go`, in `AddCustomTools`, add after the `AddPipelineSourceTool` line (currently line 61):

```go
	s.AddTool(tools.ValidatePipelineConfigTool(client))
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./pkg/tools/ -run TestValidatePipelineConfigTool -v`
Expected: PASS (3 subtests). Then `go build ./...` → no errors.

- [ ] **Step 7: Commit**

```bash
git add pkg/tools/client.go pkg/tools/pipelines.go pkg/tools/pipelines_test.go server/server.go
git commit -m "feat: add validate_pipeline_config MCP tool"
```

---

### Task 2: `save_pipeline_config` tool

**Files:**
- Modify: `pkg/tools/pipelines.go` (add `SavePipelineConfigTool`)
- Modify: `server/server.go` (register tool after validate)
- Test: `pkg/tools/pipelines_test.go` (add test)

**Interfaces:**
- Consumes: existing `SavePipeline(ctx, client, confID, description, pipeline, content string) (map[string]any, error)` from `client.go`; test helpers from Task 1.
- Produces: `func SavePipelineConfigTool(client Client) (mcp.Tool, server.ToolHandlerFunc)`.

- [ ] **Step 1: Write the failing test**

Append to `pkg/tools/pipelines_test.go`:

```go
func TestSavePipelineConfigTool(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"version":"1752190141312"}`))
	}))
	defer srv.Close()

	_, handler := SavePipelineConfigTool(newTestClient(srv.URL))
	res, err := handler(testCtx(), callToolRequest(map[string]any{
		"conf_id":     "abc123",
		"content":     "nodes: []",
		"description": "restore severity",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/v1/orgs/org123/pipelines/abc123/save" {
		t.Errorf("path = %s, want .../pipelines/abc123/save", gotPath)
	}
	if !strings.Contains(gotBody, `"content":"nodes: []"`) || !strings.Contains(gotBody, `"description":"restore severity"`) {
		t.Errorf("body = %s, want content+description", gotBody)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "1752190141312") || !strings.Contains(text, "deploy_pipeline") {
		t.Errorf("result = %s, want version echoed and deploy guidance", text)
	}
}

func TestSavePipelineConfigTool_MissingConfID(t *testing.T) {
	_, handler := SavePipelineConfigTool(newTestClient("http://unused"))
	res, _ := handler(testCtx(), callToolRequest(map[string]any{"content": "x"}))
	if res == nil || !res.IsError {
		t.Fatalf("expected error result for missing conf_id, got %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/tools/ -run TestSavePipelineConfigTool -v`
Expected: FAIL — `undefined: SavePipelineConfigTool`.

- [ ] **Step 3: Add the `SavePipelineConfigTool` handler**

Append to `pkg/tools/pipelines.go`:

```go
// SavePipelineConfigTool saves a new pipeline version from full YAML content (does not deploy).
func SavePipelineConfigTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("save_pipeline_config",
			mcp.WithTitleAnnotation("Save Pipeline Config"),
			mcp.WithDescription(`Save a NEW version of a pipeline configuration from full YAML content.

This SAVES a new version but does NOT deploy it — the running fleet is unchanged until you deploy.
Provide the complete pipeline YAML as 'content' (edit the output of get_pipeline_config).

RECOMMENDED: call validate_pipeline_config(content) first.

Workflow:
1. get_pipeline_config(conf_id) → current YAML
2. (edit YAML) → validate_pipeline_config(content)
3. save_pipeline_config(conf_id, content, description) → new version (NOT deployed)
4. get_pipeline_history(conf_id) → get the new version timestamp
5. deploy_pipeline(conf_id, version) → roll out to the fleet`),
			mcp.WithString("conf_id",
				mcp.Description("Config ID of the pipeline. Get this from get_pipelines."),
				mcp.Required(),
			),
			mcp.WithString("content",
				mcp.Description("Full pipeline configuration YAML to save as a new version."),
				mcp.Required(),
			),
			mcp.WithString("description",
				mcp.Description("Change description shown in version history."),
				mcp.DefaultString(""),
			),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			confID, err := request.RequireString("conf_id")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: conf_id"), nil
			}
			content, err := request.RequireString("content")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: content"), nil
			}
			description, err := params.Optional[string](request, "description")
			if err != nil {
				return nil, fmt.Errorf("failed to get description, err: %w", err)
			}

			result, err := SavePipeline(ctx, client, confID, description, "", content)
			if err != nil {
				return nil, fmt.Errorf("failed to save pipeline config, err: %w", err)
			}

			rawData, err := json.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal response, err: %w", err)
			}

			response := PipelineToolResponse{
				Data: rawData,
				Guidance: &PipelineGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"New version saved but NOT deployed.",
						"Use get_pipeline_history with conf_id to get the new version timestamp.",
						"Use deploy_pipeline with conf_id and version to roll it out to the fleet.",
					},
				},
			}

			r, err := json.Marshal(response)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal wrapped response, err: %w", err)
			}
			return mcp.NewToolResultText(string(r)), nil
		}
}
```

- [ ] **Step 4: Register the tool**

In `server/server.go`, add after the `ValidatePipelineConfigTool` registration:

```go
	s.AddTool(tools.SavePipelineConfigTool(client))
```

- [ ] **Step 5: Run tests to verify they pass**

Run: `go test ./pkg/tools/ -run TestSavePipelineConfigTool -v`
Expected: PASS (2 subtests). Then `go build ./...` → no errors.

- [ ] **Step 6: Commit**

```bash
git add pkg/tools/pipelines.go pkg/tools/pipelines_test.go server/server.go
git commit -m "feat: add save_pipeline_config MCP tool"
```

---

### Task 3: `revert_pipeline` tool

**Files:**
- Modify: `pkg/tools/client.go` (add `RevertPipeline`)
- Modify: `pkg/tools/pipelines.go` (add `RevertPipelineTool`)
- Modify: `server/server.go` (register tool after save)
- Test: `pkg/tools/pipelines_test.go` (add test)

**Interfaces:**
- Consumes: test helpers from Task 1; existing `FetchContextKeys`, `applyAuthHeader`.
- Produces:
  - `func RevertPipeline(ctx context.Context, client Client, confID, version string) (map[string]any, error)`
  - `func RevertPipelineTool(client Client) (mcp.Tool, server.ToolHandlerFunc)`

- [ ] **Step 1: Write the failing test**

Append to `pkg/tools/pipelines_test.go`:

```go
func TestRevertPipelineTool(t *testing.T) {
	var gotMethod, gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"reverted"}`))
	}))
	defer srv.Close()

	_, handler := RevertPipelineTool(newTestClient(srv.URL))
	res, err := handler(testCtx(), callToolRequest(map[string]any{
		"conf_id": "abc123",
		"version": "1752190141312",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/v1/orgs/org123/pipelines/abc123/revert/1752190141312" {
		t.Errorf("path = %s, want .../revert/1752190141312", gotPath)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "reverted") || !strings.Contains(text, "guidance") {
		t.Errorf("result = %s, want status + guidance", text)
	}
}

func TestRevertPipelineTool_MissingVersion(t *testing.T) {
	_, handler := RevertPipelineTool(newTestClient("http://unused"))
	res, _ := handler(testCtx(), callToolRequest(map[string]any{"conf_id": "abc123"}))
	if res == nil || !res.IsError {
		t.Fatalf("expected error result for missing version, got %+v", res)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./pkg/tools/ -run TestRevertPipelineTool -v`
Expected: FAIL — `undefined: RevertPipelineTool`.

- [ ] **Step 3: Add the `RevertPipeline` data-layer function**

Append to `pkg/tools/client.go` (after `ValidatePipelineConfig`):

```go
func RevertPipeline(ctx context.Context, client Client, confID, version string) (map[string]any, error) {
	keys, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	revertURL := fmt.Sprintf("%s/v1/orgs/%s/pipelines/%s/revert/%s", client.APIURL(), keys.OrgID, confID, version)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, revertURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create revert request: %v", err)
	}

	req.Header.Add("Content-Type", "application/json")
	applyAuthHeader(req, keys)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read revert response: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to revert pipeline, status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	result := map[string]any{}
	if len(bodyBytes) > 0 {
		if err := json.Unmarshal(bodyBytes, &result); err != nil {
			// Some endpoints return a non-object body; expose it raw.
			result = map[string]any{"response": string(bodyBytes)}
		}
	}
	return result, nil
}
```

- [ ] **Step 4: Add the `RevertPipelineTool` handler**

Append to `pkg/tools/pipelines.go`:

```go
// RevertPipelineTool rolls a pipeline back to a previously saved version.
func RevertPipelineTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("revert_pipeline",
			mcp.WithTitleAnnotation("Revert Pipeline"),
			mcp.WithDescription(`Roll a pipeline back to a previously saved version.

This is a DESTRUCTIVE operation: it changes what the fleet runs.

PREREQUISITES:
1. get_pipelines → conf_id
2. get_pipeline_history(conf_id) → choose a prior version timestamp

The version parameter is the timestamp field from get_pipeline_history.`),
			mcp.WithString("conf_id",
				mcp.Description("Config ID of the pipeline."),
				mcp.Required(),
			),
			mcp.WithString("version",
				mcp.Description("Version timestamp to revert to (from get_pipeline_history), e.g. 1752190141312."),
				mcp.Required(),
			),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			confID, err := request.RequireString("conf_id")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: conf_id"), nil
			}
			version, err := request.RequireString("version")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: version"), nil
			}

			result, err := RevertPipeline(ctx, client, confID, version)
			if err != nil {
				return nil, fmt.Errorf("failed to revert pipeline, err: %w", err)
			}

			rawData, err := json.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal response, err: %w", err)
			}

			response := PipelineToolResponse{
				Data: rawData,
				Guidance: &PipelineGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"Pipeline reverted to the requested version.",
						"Use get_pipeline_config to verify the active configuration.",
						"Use get_pipeline_history to confirm the current deployed version.",
					},
				},
			}

			r, err := json.Marshal(response)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal wrapped response, err: %w", err)
			}
			return mcp.NewToolResultText(string(r)), nil
		}
}
```

- [ ] **Step 5: Register the tool**

In `server/server.go`, add after the `SavePipelineConfigTool` registration:

```go
	s.AddTool(tools.RevertPipelineTool(client))
```

- [ ] **Step 6: Run tests to verify they pass**

Run: `go test ./pkg/tools/ -run TestRevertPipelineTool -v`
Expected: PASS (2 subtests). Then `go test ./... && go build ./...` → all pass.

- [ ] **Step 7: Commit**

```bash
git add pkg/tools/client.go pkg/tools/pipelines.go pkg/tools/pipelines_test.go server/server.go
git commit -m "feat: add revert_pipeline MCP tool"
```

---

### Task 4: Document the new tools in README

**Files:**
- Modify: `README.md` (add a tools section after "Use Cases", line ~10)

**Interfaces:**
- Consumes: nothing. Produces: documentation only.

- [ ] **Step 1: Add a Pipeline Tools section**

In `README.md`, immediately after the "## Use Cases" block (before "## Prerequisites"), insert:

```markdown
## Pipeline Configuration Tools

The server exposes tools to inspect and change Edge Delta pipelines. Write
operations follow a safe loop: **validate → save → deploy**, with rollback.

| Tool | What it does |
|------|--------------|
| `get_pipelines` / `get_pipeline_config` / `get_pipeline_history` | Discover pipelines and read their config and version history. |
| `validate_pipeline_config` | Validate a config YAML without saving it. Returns `{valid, reason}`. |
| `save_pipeline_config` | Save the full config YAML as a new version (does **not** deploy). |
| `deploy_pipeline` | Deploy a saved version to the fleet. |
| `revert_pipeline` | Roll back to a previously saved version. |
| `add_pipeline_source` | Add a data source node to a pipeline. |

Typical edit flow: `get_pipeline_config` → edit YAML → `validate_pipeline_config`
→ `save_pipeline_config` → `get_pipeline_history` → `deploy_pipeline`
(`revert_pipeline` to roll back).
```

- [ ] **Step 2: Verify the doc renders and links are consistent**

Run: `grep -n "validate_pipeline_config\|save_pipeline_config\|revert_pipeline" README.md`
Expected: three tool names present in the new table.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: document pipeline write & validation tools"
```

---

## Self-Review

**Spec coverage:**
- `validate_pipeline_config` → Task 1 ✅
- `save_pipeline_config` (reuse `SavePipeline`) → Task 2 ✅
- `revert_pipeline` → Task 3 ✅
- Registration in `server.go` → folded into Tasks 1–3 ✅
- Guidance chaining → present in every handler ✅
- `valid:false` is a success result, not an error → Task 1 `TestValidatePipelineConfigTool_Invalid` ✅
- Tests via `httptest` (first tests in repo) → Tasks 1–3 ✅
- README docs → Task 4 ✅
- Non-goals (no YAML mutators, no changes to add_source/deploy) → respected ✅

**Placeholder scan:** No TBD/TODO; all steps contain full code and exact commands.

**Type consistency:** `ValidatePipelineConfig`, `RevertPipeline` return `(map[string]any, error)`; `SavePipeline` reused with its existing signature `(ctx, client, confID, description, pipeline, content)` passing `pipeline=""`. Tool constructors return `(mcp.Tool, server.ToolHandlerFunc)`. Test helpers defined once in Task 1 and reused. Handlers use `params.Optional[string]` (already imported in `pipelines.go`) and `request.RequireString` (mcp-go v0.43.1).
