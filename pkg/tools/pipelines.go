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

	"github.com/edgedelta/edgedelta-mcp-server/pkg/params"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
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

func WithOffset(offset string) QueryParamOption {
	return func(v url.Values) {
		if offset != "" {
			v.Add("offset", offset)
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
				mcp.Description("Limit number of results, default is 100 and maximum is 100"),
				mcp.DefaultNumber(100),
			),
			mcp.WithNumber("offset",
				mcp.Description("Offset for pagination, default is 0"),
				mcp.DefaultNumber(0),
			),
			mcp.WithString("keyword",
				mcp.Description("Keyword to filter pipelines if provided should be in the pipeline tag"),
				mcp.DefaultString(""),
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

			offset, err := params.Optional[float64](request, "offset")
			if err != nil {
				return nil, fmt.Errorf("failed to get offset, err: %w", err)
			}

			keyword, err := params.Optional[string](request, "keyword")
			if err != nil {
				return nil, fmt.Errorf("failed to get keyword, err: %w", err)
			}

			limitStr := ""
			if limit > 0 {
				limitStr = strconv.Itoa(int(limit))
			}

			offsetStr := ""
			if offset > 0 {
				offsetStr = strconv.Itoa(int(offset))
			}

			result, err := GetPipelines(ctx, client, WithLimit(limitStr), WithOffset(offsetStr), WithKeyword(keyword))
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
			keys, err := FetchContextKeys(ctx)
			if err != nil {
				return nil, err
			}

			confID, err := request.RequireString("conf_id")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: conf_id"), err
			}

			historyURL := fmt.Sprintf("%s/v1/orgs/%s/confs/%s", client.APIURL(), keys.OrgID, confID)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, historyURL, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %w", err)
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

Returns history entries with timestamps that serve as version identifiers.

Workflow for deployment:
1. get_pipeline_history(conf_id) → get version timestamps
2. deploy_pipeline(conf_id, version) → use timestamp as version`),
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
			keys, err := FetchContextKeys(ctx)
			if err != nil {
				return nil, err
			}

			confID, err := request.RequireString("conf_id")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: conf_id"), err
			}

			historyURL := fmt.Sprintf("%s/v1/orgs/%s/pipelines/%s/history", client.APIURL(), keys.OrgID, confID)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, historyURL, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %v", err)
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
				return nil, fmt.Errorf("failed to read response body: %v", err)
			}

			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to get pipeline history, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			// Wrap with guidance
			response := PipelineToolResponse{
				Data: bodyBytes,
				Guidance: &PipelineGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"Use deploy_pipeline tool with conf_id and version (timestamp field from history) to deploy a specific version.",
						"The version parameter should be the timestamp from the most recent history entry.",
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
			keys, err := FetchContextKeys(ctx)
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

			deployURL := fmt.Sprintf("%s/v1/orgs/%s/pipelines/%s/deploy/%s", client.APIURL(), keys.OrgID, confID, version)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, deployURL, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %v", err)
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
				return nil, fmt.Errorf("failed to read response body: %v", err)
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
			keys, err := FetchContextKeys(ctx)
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
				return nil, fmt.Errorf("failed to marshal payload: %v", err)
			}

			addSourceURL := fmt.Sprintf("%s/v1/orgs/%s/pipelines/%s/add_source", client.APIURL(), keys.OrgID, confID)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, addSourceURL, bytes.NewReader(payloadBytes))
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %v", err)
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
				return nil, fmt.Errorf("failed to read response body: %v", err)
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

// RevertPipelineTool rolls a pipeline back to a previously saved version.
func RevertPipelineTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("revert_pipeline",
			mcp.WithTitleAnnotation("Revert Pipeline"),
			mcp.WithDescription(`Revert a pipeline to a previously saved version.

This copies the target version's content into a NEW version and does NOT deploy it —
the running fleet is unchanged until you deploy. To actually roll the fleet back, call
deploy_pipeline afterwards (or use deploy_pipeline directly on the target version).

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
			mcp.WithString("description",
				mcp.Description("Change description shown in version history for the new reverted version."),
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
			version, err := request.RequireString("version")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: version"), nil
			}
			description, err := params.Optional[string](request, "description")
			if err != nil {
				return nil, fmt.Errorf("failed to get description, err: %w", err)
			}

			result, err := RevertPipeline(ctx, client, confID, version, description)
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
						"Reverted content saved as a NEW version (not deployed).",
						"Use get_pipeline_history to get the new version timestamp.",
						"Use deploy_pipeline with that version to roll it out to the fleet.",
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
