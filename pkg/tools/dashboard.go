package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/params"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// GetAllDashboardsTool creates a tool to get all dashboards
func GetAllDashboardsTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_all_dashboards",
			mcp.WithDescription("Returns all dashboards of users in the org."),
			mcp.WithBoolean("include_definitions",
				mcp.Description("Include definitions in the response"),
			),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			orgID, token, err := FetchContextKeys(ctx)
			if err != nil {
				return nil, err
			}

			dashboardsURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/dashboards", client.APIURL(), orgID))
			if err != nil {
				return nil, err
			}

			queryParams := dashboardsURL.Query()
			if includeDefinitions, _ := params.Optional[bool](request, "include_definitions"); includeDefinitions {
				queryParams.Add("include_definitions", "true")
			}
			dashboardsURL.RawQuery = queryParams.Encode()

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, dashboardsURL.String(), nil)
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
				return nil, fmt.Errorf("failed to get dashboards, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			return mcp.NewToolResultText(string(bodyBytes)), nil
		}
}

// GetDashboardTool creates a tool to get a specific dashboard
func GetDashboardTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_dashboard",
			mcp.WithDescription("Returns the dashboard for the given ID."),
			mcp.WithString("dashboard_id",
				mcp.Description("Dashboard ID"),
				mcp.Required(),
			),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			orgID, token, err := FetchContextKeys(ctx)
			if err != nil {
				return nil, err
			}

			dashboardID, err := request.RequireString("dashboard_id")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: dashboard_id"), err
			}

			dashboardURL := fmt.Sprintf("%s/v1/orgs/%s/dashboards/%s", client.APIURL(), orgID, dashboardID)

			req, err := http.NewRequestWithContext(ctx, http.MethodGet, dashboardURL, nil)
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
				return nil, fmt.Errorf("failed to get dashboard, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			return mcp.NewToolResultText(string(bodyBytes)), nil
		}
}