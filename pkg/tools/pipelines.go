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

const (
	defaultLookbackDaysForGetPipelines = 7
)

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
			mcp.WithDescription("Get pipelines from Edge Delta for last 5 recent updated pipelines. It is a tool to get the pipelines from Edge Delta."),
			mcp.WithString("limit",
				mcp.Description("Limit number of results, default is 5 and max is 10"),
				mcp.DefaultNumber(5),
			),
			mcp.WithString("keyword",
				mcp.Description("Keyword to filter pipelines if provided should be in the pipeline tag"),
				mcp.DefaultString(""),
			),
			mcp.WithString("lookback_days",
				mcp.Description("Lookback days to get pipelines, default is 7"),
				mcp.DefaultNumber(7),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			limit, err := params.Optional[string](request, "limit")
			if err != nil {
				return nil, fmt.Errorf("failed to get limit, err: %w", err)
			}

			keyword, err := params.Optional[string](request, "keyword")
			if err != nil {
				return nil, fmt.Errorf("failed to get keyword, err: %w", err)
			}

			lookbackDays, err := params.Optional[string](request, "lookback_days")
			if err != nil {
				return nil, fmt.Errorf("failed to get lookback_days, err: %w", err)
			}

			lookbackDaysVal, ok := getNumber(lookbackDays)
			if !ok {
				lookbackDaysVal = defaultLookbackDaysForGetPipelines
			}

			result, err := GetPipelines(ctx, client, lookbackDaysVal, WithLimit(limit), WithKeyword(keyword))
			if err != nil {
				return nil, fmt.Errorf("failed to get pipelines, err: %w", err)
			}

			r, err := json.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal response, err: %w", err)
			}

			return mcp.NewToolResultText(string(r)), nil
		}
}

// GetPipelineConfigTool creates a tool to retrieve a specific pipeline.
func GetPipelineConfigTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_pipeline_config",
			mcp.WithDescription("Retrieve a specific pipeline's (according to config ID) details from Edge Delta. This will return pipeline's config content in addition to other details such as fleet and environment type etc."),
			mcp.WithString("conf_id",
				mcp.Description("Config ID of the pipeline"),
				mcp.Required(),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(false),
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

			return mcp.NewToolResultText(string(bodyBytes)), nil
		}
}

// GetPipelineHistoryTool creates a tool to get pipeline configuration history
func GetPipelineHistoryTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_pipeline_history",
			mcp.WithDescription("Returns the history of a Pipeline configuration. Timestamp of the Pipeline history is used as version when deploying the Pipeline."),
			mcp.WithString("conf_id",
				mcp.Description("Config ID of the pipeline"),
				mcp.Required(),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(false),
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

			historyURL := fmt.Sprintf("%s/v1/orgs/%s/pipelines/%s/history", client.APIURL(), orgID, confID)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, historyURL, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %v", err)
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
				return nil, fmt.Errorf("failed to read response body: %v", err)
			}

			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to get pipeline history, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			return mcp.NewToolResultText(string(bodyBytes)), nil
		}
}

// DeployPipelineTool creates a tool to deploy a pipeline configuration
func DeployPipelineTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("deploy_pipeline",
			mcp.WithDescription("Deploys the pipeline configuration. Version is the timestamp of the Pipeline history. Pipeline history should be called to get the version."),
			mcp.WithString("conf_id",
				mcp.Description("Config ID of the pipeline"),
				mcp.Required(),
			),
			mcp.WithString("version",
				mcp.Description("Version use lastUpdated field from pipeline in milliseconds timestamp format. Example: 1752190141312. This is the timestamp field of the most recent element in the result of pipeline history. So, pipeline_history should be called before this tool to get the latest version of the pipeline."),
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
				return nil, fmt.Errorf("failed to create request: %v", err)
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
				return nil, fmt.Errorf("failed to read response body: %v", err)
			}

			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to deploy pipeline, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			return mcp.NewToolResultText(string(bodyBytes)), nil
		}
}

// AddPipelineSourceTool creates a tool to add a source to a pipeline
func AddPipelineSourceTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	description := `Adds the given source node configuration to the pipeline and connect it to Edgedelta Destination. Saves the updated pipeline configuration without deploying changes.

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

			node, ok := nodeInterface.(map[string]interface{})
			if !ok {
				return mcp.NewToolResultError("node parameter must be an object"), fmt.Errorf("node parameter is not a map")
			}

			// Prepare request payload
			payload := map[string]interface{}{
				"node": node,
			}

			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal payload: %v", err)
			}

			addSourceURL := fmt.Sprintf("%s/v1/orgs/%s/pipelines/%s/add_source", client.APIURL(), orgID, confID)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, addSourceURL, bytes.NewReader(payloadBytes))
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %v", err)
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
				return nil, fmt.Errorf("failed to read response body: %v", err)
			}

			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to add pipeline source, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			return mcp.NewToolResultText(string(bodyBytes)), nil
		}
}

func getNumber(s string) (int, bool) {
	if i, err := strconv.Atoi(s); err == nil {
		return i, true
	}
	return 0, false
}
