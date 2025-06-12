package core

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// EventsSearchTool creates a tool to search for events.
func EventsSearchTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("events_search",
			mcp.WithDescription("Search for Edge Delta events"),
			mcp.WithString("query",
				mcp.Description("Search query using Edge Delta events search syntax"),
				mcp.DefaultString(""),
			),
			mcp.WithString("limit",
				mcp.Description("Limit number of results"),
				mcp.DefaultNumber(100),
			),
			mcp.WithString("cursor",
				mcp.Description("Cursor provided from previous response, pass it to next request so that we can move the cursor with given limit."),
			),
			mcp.WithString("order",
				mcp.Description("Sort order ('asc' or 'desc')"),
				mcp.Enum("asc", "desc"),
			),
			mcp.WithString("from",
				mcp.Description("Start time in 2006-01-02T15:04:05.000Z format"),
			),
			mcp.WithString("to",
				mcp.Description("End time in 2006-01-02T15:04:05.000Z format"),
			),
			mcp.WithString("lookback",
				mcp.Description("Lookback time in duration format (e.g. 60s, 15m, 1h, 1d, 1w)"),
				mcp.DefaultString("15m"),
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
			cursor, err := optionalParam[string](request, "cursor")
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

			opts := []QueryParamOption{
				WithQuery(query),
				WithOrder(order),
				WithLimit(limit),
				WithCursor(cursor),
				WithLookback(lookback),
				WithFromTo(from, to),
			}

			result, err := client.GetEvents(ctx, opts...)
			if err != nil {
				return nil, fmt.Errorf("failed to search events: %w", err)
			}

			r, err := json.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal response: %w", err)
			}

			return mcp.NewToolResultText(string(r)), nil
		}
}
