package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/params"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

const captureTimeFormat = "2006-01-02T15:04:05.000Z"

// doCaptureJSON performs an authenticated request and returns the raw body,
// erroring on non-2xx. method GET has a nil payload.
func doCaptureJSON(ctx context.Context, client Client, method, url string, payload []byte) ([]byte, error) {
	keys, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create capture request: %w", err)
	}
	req.Header.Add("Content-Type", "application/json")
	applyAuthHeader(req, keys)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read capture response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("capture request failed, status code %d: %s", resp.StatusCode, string(b))
	}
	return b, nil
}

// orgBase returns the /v1/orgs/{org} prefix.
func orgBase(ctx context.Context, client Client) (string, *ContextKeys, error) {
	keys, err := FetchContextKeys(ctx)
	if err != nil {
		return "", nil, err
	}
	return fmt.Sprintf("%s/v1/orgs/%s", client.APIURL(), keys.OrgID), keys, nil
}

// clampFloat clamps v into [min, max].
func clampFloat(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// StartPipelineCaptureTool creates a tool that starts a live-capture task on a pipeline.
func StartPipelineCaptureTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("start_pipeline_capture",
			mcp.WithTitleAnnotation("Start Pipeline Capture"),
			mcp.WithDescription(`Start a live-capture task on a pipeline to sample real before/after data flowing through its nodes.

This is the way to verify an ACTUAL transformation happened at runtime (not just that config is valid).
The task is ephemeral and self-expiring; nothing about the pipeline is modified.

PREREQUISITE: Call get_pipelines tool first to obtain the conf_id.

Workflow:
1. start_pipeline_capture(conf_id) → returns a task id
2. get_pipeline_capture_status(task_id) → poll until an agent reports "success"
3. get_pipeline_capture_results(conf_id) → inspect before/after samples per node`),
			mcp.WithString("conf_id",
				mcp.Description("Config ID of the pipeline. Get this from get_pipelines response."),
				mcp.Required(),
			),
			mcp.WithNumber("duration_seconds",
				mcp.Description("How long the capture task stays active, in seconds. Default 120, clamped to [10, 900]."),
				mcp.DefaultNumber(120),
			),
			mcp.WithArray("nodes",
				mcp.Description("Node names to capture. Omit or leave empty to capture all nodes."),
			),
			mcp.WithNumber("max_items",
				mcp.Description("Maximum number of captured items. Default 20, clamped to [1, 100]."),
				mcp.DefaultNumber(20),
			),
			mcp.WithString("polling_interval",
				mcp.Description("Optional polling interval for the capture task, e.g. \"1s\"."),
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

			durationSeconds, err := params.Optional[float64](request, "duration_seconds")
			if err != nil {
				return nil, fmt.Errorf("failed to get duration_seconds, err: %w", err)
			}
			if durationSeconds == 0 {
				durationSeconds = 120
			}
			durationSeconds = clampFloat(durationSeconds, 10, 900)

			maxItems, err := params.Optional[float64](request, "max_items")
			if err != nil {
				return nil, fmt.Errorf("failed to get max_items, err: %w", err)
			}
			if maxItems == 0 {
				maxItems = 20
			}
			maxItems = clampFloat(maxItems, 1, 100)

			pollingInterval, err := params.Optional[string](request, "polling_interval")
			if err != nil {
				return nil, fmt.Errorf("failed to get polling_interval, err: %w", err)
			}

			var nodes []string
			if raw, exists := request.GetArguments()["nodes"]; exists {
				if arr, ok := raw.([]any); ok {
					for _, n := range arr {
						if s, ok := n.(string); ok {
							nodes = append(nodes, s)
						}
					}
				}
			}

			payload := map[string]any{
				"expires_at": time.Now().UTC().Add(time.Duration(durationSeconds) * time.Second).Format(captureTimeFormat),
				"max_items":  maxItems,
			}
			if len(nodes) > 0 {
				payload["nodes_to_capture"] = nodes
			}
			if pollingInterval != "" {
				payload["polling_interval"] = pollingInterval
			}

			payloadBytes, err := json.Marshal(payload)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal payload: %w", err)
			}

			base, _, err := orgBase(ctx, client)
			if err != nil {
				return nil, err
			}
			taskURL := fmt.Sprintf("%s/pipelines/%s/capture/task", base, confID)

			bodyBytes, err := doCaptureJSON(ctx, client, http.MethodPost, taskURL, payloadBytes)
			if err != nil {
				return nil, fmt.Errorf("failed to start pipeline capture, err: %w", err)
			}

			response := PipelineToolResponse{
				Data: bodyBytes,
				Guidance: &PipelineGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"Use get_pipeline_capture_status tool with the returned task id ('id' field) to poll for completion.",
						"Once an agent reports status \"success\", use get_pipeline_capture_results tool with conf_id to fetch before/after samples.",
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

// GetPipelineCaptureStatusTool creates a tool that checks the status of a live-capture task.
func GetPipelineCaptureStatusTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_pipeline_capture_status",
			mcp.WithTitleAnnotation("Get Pipeline Capture Status"),
			mcp.WithDescription(`Check the status of a pipeline live-capture task.

PREREQUISITE: Call start_pipeline_capture tool first to obtain the task_id.

Returns per-agent capture status. Once any agent reports status "success", call
get_pipeline_capture_results to fetch the captured before/after samples.`),
			mcp.WithString("task_id",
				mcp.Description("Task ID returned by start_pipeline_capture."),
				mcp.Required(),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			taskID, err := request.RequireString("task_id")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: task_id"), nil
			}

			base, _, err := orgBase(ctx, client)
			if err != nil {
				return nil, err
			}
			statusURL := fmt.Sprintf("%s/pipelines/capture/task/%s/status", base, taskID)

			bodyBytes, err := doCaptureJSON(ctx, client, http.MethodGet, statusURL, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to get pipeline capture status, err: %w", err)
			}

			response := PipelineToolResponse{
				Data: bodyBytes,
				Guidance: &PipelineGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"If any agent in capture_status shows status \"success\", use get_pipeline_capture_results tool with conf_id to fetch the samples.",
						"Otherwise, wait briefly and call get_pipeline_capture_status again to keep polling.",
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

// GetPipelineCaptureResultsTool creates a tool that fetches captured before/after samples for a pipeline.
func GetPipelineCaptureResultsTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_pipeline_capture_results",
			mcp.WithTitleAnnotation("Get Pipeline Capture Results"),
			mcp.WithDescription(`Fetch captured live-capture results for a pipeline.

PREREQUISITES:
1. start_pipeline_capture(conf_id) → start a task
2. get_pipeline_capture_status(task_id) → wait until an agent reports "success"

Returns a JSON array of payloads, each with a "source" (agent id), "timestamp", and
"nodes" map of node name to {"before": [...], "after": [...]} samples. Compare the
"before" and "after" arrays for each node to confirm the transformation actually
happened (e.g. a field present in "after" but not in "before").`),
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
			confID, err := request.RequireString("conf_id")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: conf_id"), nil
			}

			base, _, err := orgBase(ctx, client)
			if err != nil {
				return nil, err
			}
			resultsURL := fmt.Sprintf("%s/pipelines/%s/capture", base, confID)

			bodyBytes, err := doCaptureJSON(ctx, client, http.MethodGet, resultsURL, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to get pipeline capture results, err: %w", err)
			}

			response := PipelineToolResponse{
				Data: bodyBytes,
				Guidance: &PipelineGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"Results contain per-node 'before' and 'after' samples.",
						"Compare 'before' and 'after' for each node to confirm the expected transformation happened (e.g. a field present in 'after' but not 'before').",
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

// GetPipelineCaptureTaskTool creates a tool that fetches the active live-capture task for a pipeline, if any.
func GetPipelineCaptureTaskTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_pipeline_capture_task",
			mcp.WithTitleAnnotation("Get Pipeline Capture Task"),
			mcp.WithDescription(`Get the currently active live-capture task for a pipeline, if any.

PREREQUISITE: Call get_pipelines tool first to obtain the conf_id.

Returns the active task (id, expiry, max_items) or an empty result if no capture task is running.`),
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
			confID, err := request.RequireString("conf_id")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: conf_id"), nil
			}

			base, _, err := orgBase(ctx, client)
			if err != nil {
				return nil, err
			}
			taskURL := fmt.Sprintf("%s/pipelines/%s/capture/task", base, confID)

			bodyBytes, err := doCaptureJSON(ctx, client, http.MethodGet, taskURL, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to get active pipeline capture task, err: %w", err)
			}

			response := PipelineToolResponse{
				Data: bodyBytes,
				Guidance: &PipelineGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"If a task id is present, use get_pipeline_capture_status with that id to check progress before it expires.",
						"If no task is active, use start_pipeline_capture to start a new one.",
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
