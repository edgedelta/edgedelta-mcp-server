package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/params"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
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

// GetPipelinesTool creates a tool to search for logs.
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
			result, err := GetPipelines(ctx, client, WithLimit(limit), WithKeyword(keyword))
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
