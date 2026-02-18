package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/params"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const (
	defaultLookbackDaysForGetPipelines = 7
)

type PipelineToolResponse struct {
	Data     json.RawMessage   `json:"data"`
	Guidance *PipelineGuidance `json:"guidance,omitempty"`
}

type PipelineGuidance struct {
	ResultStatus string   `json:"result_status"`
	NextSteps    []string `json:"next_steps,omitempty"`
	Suggestions  []string `json:"suggestions,omitempty"`
}

func WithKeyword(keyword string) QueryParamOption {
	return func(v url.Values) {
		if keyword != "" {
			v.Add("keyword", keyword)
		}
	}
}

func WithLimit(limit string) QueryParamOption {
	return func(v url.Values) {
		if limit != "" {
			v.Add("limit", limit)
		}
	}
}

// GetPipelinesTool creates a tool to search for pipelines.
func GetPipelinesTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_pipelines",
			mcp.WithTitleAnnotation("Get Pipelines"),
			mcp.WithDescription(`List pipelines from Edge Delta.

WORKFLOW: This is the starting point for pipeline operations.
1. get_pipelines → list available pipelines with their conf_id
2. get_pipeline_config(conf_id) → get detailed configuration
3. get_pipeline_history(conf_id) → see version history
4. deploy_pipeline(conf_id, version) → deploy a specific version
5. add_pipeline_source(conf_id, source) → add data source

Returns recently updated pipelines with their conf_id (configuration ID) which is required for all other pipeline operations.`),
			mcp.WithNumber("limit",
				mcp.Description("Limit number of results, default is 5 and max is 10"),
				mcp.DefaultNumber(5),
			),
			mcp.WithString("keyword",
				mcp.Description("Keyword to filter pipelines if provided should be in the pipeline tag"),
				mcp.DefaultString(""),
			),
			mcp.WithNumber("lookback_days",
				mcp.Description("Lookback days to get pipelines, default is 7"),
				mcp.DefaultNumber(7),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			limit, err := params.Optional[float64](request, "limit")
			if err != nil {
				return nil, fmt.Errorf("failed to get limit, err: %w", err)
			}

			keyword, err := params.Optional[string](request, "keyword")
			if err != nil {
				return nil, fmt.Errorf("failed to get keyword, err: %w", err)
			}

			lookbackDays, err := params.Optional[float64](request, "lookback_days")
			if err != nil {
				return nil, fmt.Errorf("failed to get lookback_days, err: %w", err)
			}

			lookbackDaysVal := defaultLookbackDaysForGetPipelines
			if lookbackDays > 0 {
				lookbackDaysVal = int(lookbackDays)
			}

			limitStr := ""
			if limit > 0 {
				limitStr = strconv.Itoa(int(limit))
			}

			result, err := GetPipelines(ctx, client, lookbackDaysVal, WithLimit(limitStr), WithKeyword(keyword))
			if err != nil {
				return nil, fmt.Errorf("failed to get pipelines, err: %w", err)
			}

			rawData, err := json.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal response, err: %w", err)
			}

			// Wrap with guidance
			response := PipelineToolResponse{
				Data: rawData,
				Guidance: &PipelineGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"Use get_pipeline_config tool with conf_id to get detailed configuration for a specific pipeline.",
						"Use get_pipeline_history tool with conf_id to see configuration change history.",
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

// GetPipelineConfigTool creates a tool to retrieve a specific pipeline.
func GetPipelineConfigTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_pipeline_config",
			mcp.WithTitleAnnotation("Get Pipeline Config"),
			mcp.WithDescription(`Get detailed configuration for a specific pipeline.

PREREQUISITE: Call get_pipelines tool first to obtain the conf_id.

Returns the full pipeline configuration including:
- Config content (YAML/JSON)
- Fleet and environment type
- Pipeline metadata

After viewing config, you can:
- Use get_pipeline_history tool to see version history
- Use add_pipeline_source tool to add data sources
- Use deploy_pipeline tool to deploy changes`),
			mcp.WithString("conf_id",
				mcp.Description("Config ID of the pipeline. Get this from get_pipelines response."),
				mcp.Required(),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			orgID, token, err := FetchContextKeys(ctx)
			if err != nil {
				return nil, err
			}

			confID, err := request.RequireString("conf_id")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: conf_id"), err
			}

			historyURL := fmt.Sprintf("%s/v1/orgs/%s/confs/%s", client.APIURL(), orgID, confID)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, historyURL, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %w", err)
			}

			req.Header.Add("Content-Type", "application/json")
			req.Header.Add("X-ED-API-Token", token)

			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}

			defer resp.Body.Close()
			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to read response body: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to get pipeline, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			// Wrap with guidance
			response := PipelineToolResponse{
				Data: bodyBytes,
				Guidance: &PipelineGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"Use get_pipeline_history tool to see the configuration change history.",
						"Use deploy_pipeline tool to deploy the pipeline after making changes.",
						"Use add_pipeline_source tool to add new data sources to the pipeline.",
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

// GetPipelineHistoryTool creates a tool to get pipeline configuration history
func GetPipelineHistoryTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_pipeline_history",
			mcp.WithTitleAnnotation("Get Pipeline History"),
			mcp.WithDescription(`Get version history for a pipeline configuration.

PREREQUISITE: Call get_pipelines tool first to obtain the conf_id.

REQUIRED FOR DEPLOYMENT: The version (timestamp field) from history is required when calling deploy_pipeline tool.

Returns metadata-only history entries (timestamp, description, updatedBy, tag). Full YAML content is stripped to keep responses small. Use get_pipeline_config to read full content.

Workflow for deployment:
1. get_pipeline_history(conf_id) → get version timestamps
2. deploy_pipeline(conf_id, version) → use timestamp as version`),
			mcp.WithString("conf_id",
				mcp.Description("Config ID of the pipeline. Get this from get_pipelines response."),
				mcp.Required(),
			),
			mcp.WithNumber("limit",
				mcp.Description("Maximum number of history entries to return (default 5, max 50)"),
				mcp.DefaultNumber(5),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			orgID, token, err := FetchContextKeys(ctx)
			if err != nil {
				return nil, err
			}

			confID, err := request.RequireString("conf_id")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: conf_id"), err
			}

			limit := 5
			if l, err := params.Optional[float64](request, "limit"); err == nil && l > 0 {
				limit = int(l)
				if limit > 50 {
					limit = 50
				}
			}

			historyURL := fmt.Sprintf("%s/v1/orgs/%s/pipelines/%s/history", client.APIURL(), orgID, confID)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, historyURL, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %w", err)
			}

			req.Header.Add("Content-Type", "application/json")
			req.Header.Add("X-ED-API-Token", token)

			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}

			defer resp.Body.Close()
			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to read response body: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to get pipeline history, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			// Strip full YAML content from history entries to reduce response size.
			// The API returns full config content per entry which can be 100KB+.
			trimmedData := trimHistoryContent(bodyBytes, limit)

			// Wrap with guidance
			response := PipelineToolResponse{
				Data: trimmedData,
				Guidance: &PipelineGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"Use deploy_pipeline tool with conf_id and version (timestamp field from history) to deploy a specific version.",
						"The version parameter should be the timestamp from the most recent history entry.",
						"Use get_pipeline_config to read the full YAML content of the current configuration.",
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

// getLatestVersionTimestamp fetches pipeline history and returns the most recent
// version's timestamp as a string (milliseconds format used by deploy API).
func getLatestVersionTimestamp(ctx context.Context, client Client, orgID, token, confID string) (string, error) {
	historyURL := fmt.Sprintf("%s/v1/orgs/%s/pipelines/%s/history", client.APIURL(), orgID, confID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, historyURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create history request: %w", err)
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("X-ED-API-Token", token)

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch history: %w", err)
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read history response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("history request failed with status %d", resp.StatusCode)
	}

	var entries []map[string]any
	if err := json.Unmarshal(bodyBytes, &entries); err != nil {
		return "", fmt.Errorf("failed to parse history response: %w", err)
	}

	if len(entries) == 0 {
		return "", fmt.Errorf("no history entries found")
	}

	ts, ok := entries[0]["timestamp"]
	if !ok {
		return "", fmt.Errorf("first history entry has no timestamp field")
	}

	switch v := ts.(type) {
	case float64:
		return strconv.FormatInt(int64(v), 10), nil
	case json.Number:
		return v.String(), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// deployPipelineVersion calls the deploy API for a specific version.
func deployPipelineVersion(ctx context.Context, client Client, orgID, token, confID, version string) error {
	deployURL := fmt.Sprintf("%s/v1/orgs/%s/pipelines/%s/deploy/%s", client.APIURL(), orgID, confID, version)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, deployURL, nil)
	if err != nil {
		return fmt.Errorf("failed to create deploy request: %w", err)
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("X-ED-API-Token", token)

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("deploy request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("deploy failed with status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return nil
}

// trimHistoryContent removes bulky fields (content, pipeline) from history entries
// and limits the number of entries returned.
func trimHistoryContent(data []byte, limit int) json.RawMessage {
	// Try as array of objects (most common shape)
	var entries []map[string]any
	if err := json.Unmarshal(data, &entries); err != nil {
		// Try as object with nested array
		var wrapper map[string]any
		if err := json.Unmarshal(data, &wrapper); err != nil {
			return data // can't parse, return as-is
		}
		// Look for an array field like "history" or "items"
		for _, key := range []string{"history", "items", "data", "versions"} {
			if arr, ok := wrapper[key]; ok {
				if arrBytes, err := json.Marshal(arr); err == nil {
					if err := json.Unmarshal(arrBytes, &entries); err == nil {
						trimEntries(entries, limit)
						wrapper[key] = entries
						if out, err := json.Marshal(wrapper); err == nil {
							return out
						}
					}
				}
			}
		}
		return data
	}

	trimEntries(entries, limit)
	if len(entries) > limit {
		entries = entries[:limit]
	}

	if out, err := json.Marshal(entries); err == nil {
		return out
	}
	return data
}

func trimEntries(entries []map[string]any, limit int) {
	for i := range entries {
		delete(entries[i], "content")
		delete(entries[i], "pipeline")
		delete(entries[i], "config_content")
	}
}

// DeployPipelineTool creates a tool to deploy a pipeline configuration
func DeployPipelineTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("deploy_pipeline",
			mcp.WithTitleAnnotation("Deploy Pipeline"),
			mcp.WithDescription(`Deploys a specific version of a pipeline configuration.

PREREQUISITES (must be called in order):
1. get_pipelines → obtain conf_id for the target pipeline
2. get_pipeline_history(conf_id) → obtain version timestamp (timestamp field)

The version parameter is the timestamp from get_pipeline_history response.
This is a DESTRUCTIVE operation that will apply configuration changes.

Workflow example:
1. get_pipelines → find pipeline with conf_id:"abc123"
2. get_pipeline_history(conf_id:"abc123") → get version:"1752190141312"
3. deploy_pipeline(conf_id:"abc123", version:"1752190141312") → deploy`),
			mcp.WithString("conf_id",
				mcp.Description("Config ID of the pipeline"),
				mcp.Required(),
			),
			mcp.WithString("version",
				mcp.Description("Version uses the timestamp field from pipeline history in milliseconds format. Example: 1752190141312. This is the timestamp field of the most recent element in the result of get_pipeline_history tool. Call get_pipeline_history tool first to get the latest version."),
				mcp.Required(),
			),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			orgID, token, err := FetchContextKeys(ctx)
			if err != nil {
				return nil, err
			}

			confID, err := request.RequireString("conf_id")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: conf_id"), err
			}

			version, err := request.RequireString("version")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: version"), err
			}

			deployURL := fmt.Sprintf("%s/v1/orgs/%s/pipelines/%s/deploy/%s", client.APIURL(), orgID, confID, version)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, deployURL, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %w", err)
			}

			req.Header.Add("Content-Type", "application/json")
			req.Header.Add("X-ED-API-Token", token)

			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}

			defer resp.Body.Close()
			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to read response body: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to deploy pipeline, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			// Wrap with guidance
			response := PipelineToolResponse{
				Data: bodyBytes,
				Guidance: &PipelineGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"Pipeline deployment initiated successfully.",
						"Use get_pipeline_config tool to verify the deployed configuration.",
						"Monitor pipeline status to ensure successful deployment.",
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

// AddPipelineSourceTool creates a tool to add a source to a pipeline
func AddPipelineSourceTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	description := `Adds a data source node to a pipeline configuration.

PREREQUISITE: Call get_pipelines tool first to obtain the conf_id.

This tool SAVES the configuration but does NOT deploy it.
After adding source, you must deploy to apply changes:
1. add_pipeline_source tool (conf_id, node) → saves configuration
2. get_pipeline_history tool (conf_id) → get new version timestamp
3. deploy_pipeline tool (conf_id, version) → deploy the changes

Example node configurations:

1. File input node:
{
  "node": {
    "name": "my_file_input",
    "type": "file_input",
    "path": "path/to/my_logs/logs.txt",
    "separate_source": true,
    "description": "A user-defined description of the Node. Users may add any additional comments describing the function of their node."
  }
}

2. Kubernetes input node:
{
  "node": {
    "name": "my_k8s_input",
    "type": "kubernetes_input",
    "include": [
      "k8s.pod.name=^apache.*$,k8s.namespace.name=.*web*"
    ],
    "exclude": [
      "k8s.namespace.name=^kube-nginx$",
      "k8s.pod.name=.*nginx*,k8s.container.name=testing"
    ],
    "auto_detect_line_pattern": true,
    "boost_stacktrace_detection": true,
    "enable_persisting_cursor": true,
    "description": "A user-defined description of the Node"
  }
}

3. Demo input node:
{
  "node": {
    "name": "my_demo_input",
    "type": "demo_input",
    "events_per_sec": 1,
    "log_type": "apache_common",
    "description": "A user-defined description of the Node"
  }
}`

	return mcp.NewTool("add_pipeline_source",
			mcp.WithTitleAnnotation("Add Pipeline Source"),
			mcp.WithDescription(description),
			mcp.WithString("conf_id",
				mcp.Description("Config ID of the pipeline"),
				mcp.Required(),
			),
			mcp.WithObject("node",
				mcp.Description("Source node configuration to add. Must include 'name' and 'type' fields. Type can be 'file_input', 'kubernetes_input', or 'demo_input'. See examples in the tool description for specific field requirements for each node type."),
				mcp.Required(),
			),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			orgID, token, err := FetchContextKeys(ctx)
			if err != nil {
				return nil, err
			}

			confID, err := request.RequireString("conf_id")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: conf_id"), err
			}

			args := request.GetArguments()
			nodeInterface, exists := args["node"]
			if !exists {
				return mcp.NewToolResultError("missing required parameter: node"), fmt.Errorf("missing required parameter: node")
			}

			node, ok := nodeInterface.(map[string]any)
			if !ok {
				return mcp.NewToolResultError("node parameter must be an object"), fmt.Errorf("node parameter is not a map")
			}

			// Prepare request payload
			payload := map[string]any{
				"node": node,
			}

			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal payload: %w", err)
			}

			addSourceURL := fmt.Sprintf("%s/v1/orgs/%s/pipelines/%s/add_source", client.APIURL(), orgID, confID)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, addSourceURL, bytes.NewReader(payloadBytes))
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %w", err)
			}

			req.Header.Add("Content-Type", "application/json")
			req.Header.Add("X-ED-API-Token", token)

			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}

			defer resp.Body.Close()
			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to read response body: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to add pipeline source, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			// Wrap with guidance
			response := PipelineToolResponse{
				Data: bodyBytes,
				Guidance: &PipelineGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"Source added and configuration saved (not yet deployed).",
						"Use get_pipeline_history tool to get the latest version timestamp.",
						"Use deploy_pipeline tool with the version to deploy the updated configuration.",
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

func SavePipelineTool(client Client) (mcp.Tool, server.ToolHandlerFunc) {
	description := `Save pipeline configuration. By default saves only (does NOT deploy).
Set deploy=true to save AND deploy in one step, eliminating the need for separate get_pipeline_history and deploy_pipeline calls.

TIP: If unsure about field names for a node type, call get_pipeline_config on an existing pipeline that uses that node type and match its YAML structure exactly.

IMPORTANT pipeline v3 requirements:
- 'version: v3' MUST be the first line
- 'settings.tag' is required (pipeline identifier)
- Must include an 'ed_self_telemetry_input' node and at least one output (usually 'ed_output')
- Use 'links:' (not 'routing:') to define connections between nodes
- All link 'from'/'to' values must reference existing node names
- Sequence processors must be sequence-compatible (ottl_transform, generic_mask, extract_metric, regex_filter, sample, json_unroll, etc.)
- Only the LAST processor in a sequence should have 'final: true'
- If 'deotel' processor is used, it MUST be last in the sequence
- Avoid 'persisting_cursor_settings' (causes API 500 errors)
- No Unicode characters in YAML comments (no arrows, checkmarks)
- json_field_path must use '$' not '.' as root

NODE TYPE FIELD REFERENCE (common mistakes cause cryptic 500 errors):

http_pull_input:
  name: my_source
  type: http_pull_input
  method: GET
  endpoint: "https://api.example.com/data"  # NOT 'url:'
  pull_interval: 10m                         # NOT 'interval:'
  headers:                                   # MUST be array, NOT map
  - header: Accept
    value: application/json
  - header: Authorization
    value: "Bearer {{env:MY_TOKEN}}"

http_workflow_input:
  name: my_workflow
  type: http_workflow_input
  workflow_pull_interval: 5m                 # NOT 'interval:' or 'pull_interval:'
  steps:                                     # NOT 'initial_request:'
  - name: fetch_data
    method: GET
    endpoint: "https://api.example.com/data"
    headers:
      Accept: application/json
    is_last_step: true

lookup (processor inside sequence):
  - type: lookup
    location_path: "ed://my-lookup.csv"
    reload_period: 5m
    match_mode: exact
    key_fields:
    - event_field: resource["source.api_name"]
      lookup_field: api_name
    out_fields:
    - event_field: attributes["_enriched"]
      lookup_field: enrichment_value`

	return mcp.NewTool("save_pipeline",
			mcp.WithTitleAnnotation("Save Pipeline"),
			mcp.WithDescription(description),
			mcp.WithString("conf_id",
				mcp.Description("Config ID of the pipeline to save"),
				mcp.Required(),
			),
			mcp.WithString("content",
				mcp.Description("Full pipeline YAML content to save"),
				mcp.Required(),
			),
			mcp.WithString("description",
				mcp.Description("Save description/commit message for this change"),
				mcp.Required(),
			),
			mcp.WithBoolean("deploy",
				mcp.Description("If true, automatically deploy after saving (default false). Eliminates the need for separate get_pipeline_history + deploy_pipeline calls."),
				mcp.DefaultBool(false),
			),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			confID, err := request.RequireString("conf_id")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: conf_id"), err
			}

			content, err := request.RequireString("content")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: content"), err
			}

			desc, err := request.RequireString("description")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: description"), err
			}

			// Pre-flight validation to catch common errors with clear messages
			validation := validatePipelineYAML(content)
			if !validation.Valid {
				validationJSON, _ := json.Marshal(validation)
				return mcp.NewToolResultError(fmt.Sprintf("Pipeline validation failed (not sent to API). Fix these errors and retry:\n%s", string(validationJSON))), nil
			}

			autoDeploy, _ := params.Optional[bool](request, "deploy")

			result, err := SavePipeline(ctx, client, confID, desc, "", content)
			if err != nil {
				// Enhance the unhelpful 500 "Failed to read request content" error
				if strings.Contains(err.Error(), "Failed to read request content") {
					return nil, fmt.Errorf("API rejected the pipeline YAML (500). The YAML passed client-side validation but the server's strict parser rejected it. Common causes: wrong field names for the node type (e.g. 'url' instead of 'endpoint' for http_pull_input), headers as a map instead of array of {header, value} objects, or missing required node-specific fields. Use get_pipeline_config on a working pipeline to see the correct field format. Original error: %w", err)
				}
				return nil, fmt.Errorf("failed to save pipeline: %w", err)
			}

			nextSteps := []string{
				"Pipeline configuration saved (not yet deployed).",
				"Use get_pipeline_history tool to get the latest version timestamp.",
				"Use deploy_pipeline tool with the version to deploy the updated configuration.",
			}

			// Auto-deploy: fetch latest history entry for timestamp, then deploy
			if autoDeploy {
				orgID, token, err := FetchContextKeys(ctx)
				if err != nil {
					return nil, fmt.Errorf("save succeeded but deploy failed (cannot get context): %w", err)
				}

				version, err := getLatestVersionTimestamp(ctx, client, orgID, token, confID)
				if err != nil {
					// Save succeeded but couldn't get version — tell user to deploy manually
					result["deploy_error"] = fmt.Sprintf("Save succeeded but could not fetch version for auto-deploy: %v. Deploy manually via get_pipeline_history + deploy_pipeline.", err)
				} else {
					deployErr := deployPipelineVersion(ctx, client, orgID, token, confID, version)
					if deployErr != nil {
						result["deploy_error"] = fmt.Sprintf("Save succeeded but deploy failed: %v. Deploy manually with deploy_pipeline(version=%s).", deployErr, version)
					} else {
						result["deployed_version"] = version
						nextSteps = []string{
							"Pipeline saved and deployed successfully.",
							"Use get_pipeline_config to verify the deployed configuration.",
						}
					}
				}
			}

			resultBytes, err := json.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal result: %w", err)
			}

			response := PipelineToolResponse{
				Data: resultBytes,
				Guidance: &PipelineGuidance{
					ResultStatus: "success",
					NextSteps:    nextSteps,
				},
			}

			r, err := json.Marshal(response)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal wrapped response: %w", err)
			}

			return mcp.NewToolResultText(string(r)), nil
		}
}
