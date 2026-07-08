package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// --- data layer ---

// TestPipelineOTTL dry-runs OTTL statements against sample OTEL log items without saving or
// deploying any pipeline configuration.
func TestPipelineOTTL(ctx context.Context, client Client, confID, statements string, items []string) (json.RawMessage, error) {
	base, _, err := orgBase(ctx, client)
	if err != nil {
		return nil, err
	}
	payload, err := json.Marshal(map[string]any{"items": items, "statements": statements})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal test-ottl payload: %w", err)
	}
	return doCaptureJSON(ctx, client, http.MethodPost, base+"/pipelines/"+confID+"/test/ottl", payload)
}

// stringArrayParam extracts a required array-of-strings parameter from the request arguments.
func stringArrayParam(request mcp.CallToolRequest, name string) ([]string, error) {
	raw, exists := request.GetArguments()[name]
	if !exists {
		return nil, fmt.Errorf("missing required parameter: %s", name)
	}
	arr, ok := raw.([]any)
	if !ok {
		return nil, fmt.Errorf("parameter %s must be an array", name)
	}
	items := make([]string, 0, len(arr))
	for _, e := range arr {
		if s, ok := e.(string); ok {
			items = append(items, s)
			continue
		}
		items = append(items, fmt.Sprintf("%v", e))
	}
	return items, nil
}

// --- tools ---

// TestPipelineOTTLTool creates a tool that dry-runs OTTL statements against sample OTEL logs.
func TestPipelineOTTLTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("test_pipeline_ottl",
			mcp.WithTitleAnnotation("Test Pipeline OTTL"),
			mcp.WithDescription(`Dry-run OTTL (OpenTelemetry Transformation Language) statements against sample OTEL log
items WITHOUT saving or deploying any pipeline configuration. This is the offline complement
to live-capture — use it to develop and verify a transform before it ever touches real traffic.

Each entry in 'items' is a JSON-encoded string of an OTEL log record, e.g.:
  {"body":"...","attributes":{},"resource":{},"timestamp":1}

'statements' is one or more OTTL statements (may be multiline), e.g.:
  set(severity_text, "INFO")
  merge_maps(attributes, ParseJSON(body), "upsert")

The response's "items" are the transformed logs in the same order as the input — diff them
against your input items to confirm the statements did what you expect.

PREREQUISITE: Call get_pipelines tool first to obtain the conf_id.`),
			mcp.WithString("conf_id",
				mcp.Description("Config ID of the pipeline. Get this from get_pipelines response."),
				mcp.Required(),
			),
			mcp.WithString("statements",
				mcp.Description(`OTTL statements to apply, e.g. set(severity_text, "INFO"). May be multiline for multiple statements.`),
				mcp.Required(),
			),
			mcp.WithArray("items",
				mcp.Description(`Sample OTEL log items to transform. Each item is a JSON-encoded string with body/attributes/resource/timestamp fields, e.g. {"body":"...","attributes":{},"resource":{},"timestamp":1}.`),
				mcp.WithStringItems(),
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
			statements, err := request.RequireString("statements")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: statements"), nil
			}
			items, err := stringArrayParam(request, "items")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			bodyBytes, err := TestPipelineOTTL(ctx, client, confID, statements, items)
			if err != nil {
				return nil, fmt.Errorf("failed to test pipeline ottl, err: %w", err)
			}

			response := PipelineToolResponse{
				Data: bodyBytes,
				Guidance: &PipelineGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"Compare the returned items against your input items to confirm the OTTL statements produced the expected transformation.",
						"Once the statements produce the right output, put them in an ottl_transform node and call save_pipeline_config.",
						"After saving and deploying, verify on live traffic with start_pipeline_capture / get_pipeline_capture_results.",
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
