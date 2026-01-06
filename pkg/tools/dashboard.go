package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/params"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type DashboardToolResponse struct {
	Data     json.RawMessage    `json:"data"`
	Guidance *DashboardGuidance `json:"guidance,omitempty"`
}

type DashboardGuidance struct {
	ResultStatus string   `json:"result_status"`
	NextSteps    []string `json:"next_steps,omitempty"`
	Suggestions  []string `json:"suggestions,omitempty"`
}

// GetAllDashboardsTool creates a tool to get all dashboards
func GetAllDashboardsTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_all_dashboards",
			mcp.WithDescription(`List all dashboards in the organization.

WORKFLOW: This is the entry point for dashboard operations.
1. get_all_dashboards → list dashboards with their dashboard_id
2. get_dashboard(dashboard_id) → get detailed dashboard configuration

Returns dashboard summaries. Use include_definitions:true for full widget definitions.`),
			mcp.WithBoolean("include_definitions",
				mcp.Description("Include definitions in the response"),
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

			// Wrap with guidance
			response := DashboardToolResponse{
				Data: bodyBytes,
				Guidance: &DashboardGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"Use get_dashboard with dashboard_id to get detailed information for a specific dashboard.",
						"Dashboard IDs can be found in the response above.",
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

// GetDashboardTool creates a tool to get a specific dashboard
func GetDashboardTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_dashboard",
			mcp.WithDescription(`Get detailed configuration for a specific dashboard.

PREREQUISITE: Call get_all_dashboards first to obtain the dashboard_id.

Returns full dashboard configuration including widget definitions and layout.`),
			mcp.WithString("dashboard_id",
				mcp.Description("Dashboard ID"),
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

			// Wrap with guidance
			response := DashboardToolResponse{
				Data: bodyBytes,
				Guidance: &DashboardGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"Dashboard details retrieved successfully.",
						"Use get_all_dashboards to see other available dashboards.",
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
