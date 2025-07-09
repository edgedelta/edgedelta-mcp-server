package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// SavePipelineTool creates a tool to save Edge Delta pipeline configurations.
func SavePipelineTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("save_pipeline",
			mcp.WithDescription("Save Edge Delta pipeline configuration. This tool allows you to save pipeline configurations by providing either a structured pipeline object or raw YAML content but not both."),
			mcp.WithString("conf_id",
				mcp.Description("The configuration/pipeline ID to save"),
			),
			mcp.WithString("description",
				mcp.Description("Description of the changes being saved"),
			),
			mcp.WithString("pipeline",
				mcp.Description("Structured pipeline configuration object as JSON string (optional)"),
			),
			mcp.WithString("content",
				mcp.Description("Raw YAML configuration content (optional, alternative to pipeline object)"),
			),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			confID, err := request.RequireString("conf_id")
			if err != nil {
				return nil, fmt.Errorf("failed to get conf_id: %w", err)
			}

			description, err := request.RequireString("description")
			if err != nil {
				return nil, fmt.Errorf("failed to get description: %w", err)
			}

			pipeline, err := optionalParam[string](request, "pipeline")
			if err != nil {
				return nil, fmt.Errorf("failed to get pipeline: %w", err)
			}

			content, err := optionalParam[string](request, "content")
			if err != nil {
				return nil, fmt.Errorf("failed to get content: %w", err)
			}

			if pipeline == "" && content == "" {
				return nil, fmt.Errorf("either pipeline or content must be provided")
			}

			if pipeline != "" && content != "" {
				return nil, fmt.Errorf("pipeline and content cannot be used together")
			}

			result, err := client.SavePipeline(ctx, confID, description, pipeline, content)
			if err != nil {
				return nil, fmt.Errorf("failed to save pipeline: %w", err)
			}

			r, err := json.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal response: %w", err)
			}

			return mcp.NewToolResultText(string(r)), nil
		}
}
