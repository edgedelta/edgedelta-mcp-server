package core

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// PatternStatsTool creates a tool returning log pattern statistics.
func PatternStatsTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("pattern_stats",
			mcp.WithDescription("Returns top log patterns (signatures of log messages) and their stats; count, proportion, sentiment and delta."),
			mcp.WithString("query",
				mcp.Description("Search query using Edge Delta pattern search syntax similar to log search"),
				mcp.DefaultString(""),
			),
			mcp.WithString("limit",
				mcp.Description("Limit number of results"),
				mcp.DefaultNumber(100),
			),
			mcp.WithString("from",
				mcp.Description("Start time in 2006-01-02T15:04:05.000Z format"),
			),
			mcp.WithString("to",
				mcp.Description("End time in 2006-01-02T15:04:05.000Z format"),
			),
			mcp.WithString("order",
				mcp.Description("Sort order ('asc' or 'desc')"),
				mcp.Enum("asc", "desc"),
			),
			mcp.WithString("lookback",
				mcp.Description("Lookback time in duration format (e.g. 60s, 15m, 1h, 1d, 1w)"),
				mcp.DefaultString("15m"),
			),
			mcp.WithString("offset",
				mcp.Description("Comma separated offsets for delta stat calculation. Each offset is in golang duration format and order of offsets determines order of offset_ fields in cluster stat response. Default value is lookback duration. e.g. '24h'."),
				mcp.DefaultString("24h"),
			),
			mcp.WithBoolean("negative",
				mcp.Description("Include negative sentiment patterns"),
				mcp.DefaultBool(false),
			),
			mcp.WithBoolean("summary",
				mcp.Description("If summary true call returns up to 50 interesting clusters with 10 top anomaly, top/bottom delta, top/bottom count. Param size is ignored if summary is true."),
				mcp.DefaultBool(false),
			),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			query, err := optionalParam[string](request, "query")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			order, err := optionalParam[string](request, "order")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			limit, err := optionalParam[string](request, "limit")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			lookback, err := optionalParam[string](request, "lookback")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			from, err := optionalParam[string](request, "from")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			to, err := optionalParam[string](request, "to")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			offset, err := optionalParam[string](request, "offset")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			negative, err := optionalParam[bool](request, "negative")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}
			summary, err := optionalParam[bool](request, "summary")
			if err != nil {
				return mcp.NewToolResultError(err.Error()), nil
			}

			opts := []QueryParamOption{
				WithQuery(query),
				WithLimit(limit),
				WithFromTo(from, to),
				WithOrder(order),
				WithLookback(lookback),
				WithOffset(offset),
				WithNegative(negative),
				WithSummary(summary),
			}

			result, err := client.GetPatternStats(ctx, opts...)
			if err != nil {
				return nil, fmt.Errorf("failed to fetch pattern stats: %w", err)
			}

			r, err := json.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal response: %w", err)
			}

			return mcp.NewToolResultText(string(r)), nil
		}
}
