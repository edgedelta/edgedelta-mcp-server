package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/params"
	"github.com/edgedelta/edgedelta-mcp-server/pkg/tools/validation"
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
			mcp.WithTitleAnnotation("Get All Dashboards"),
			mcp.WithDescription(`List all dashboards in the organization.

WORKFLOW: This is the entry point for dashboard operations.
1. get_all_dashboards → list dashboards with their dashboard_id
2. get_dashboard(dashboard_id) → get detailed dashboard configuration

Returns dashboard summaries. Use include_definitions:true for full widget definitions.`),
			mcp.WithBoolean("include_definitions",
				mcp.Description("Include definitions in the response"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
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
						"Use get_dashboard tool with dashboard_id to get detailed information for a specific dashboard.",
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
			mcp.WithTitleAnnotation("Get Dashboard"),
			mcp.WithDescription(`Get detailed configuration for a specific dashboard.

PREREQUISITE: Call get_all_dashboards tool first to obtain the dashboard_id.

Returns full dashboard configuration including widget definitions and layout.`),
			mcp.WithString("dashboard_id",
				mcp.Description("Dashboard ID"),
				mcp.Required(),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
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
						"Use get_all_dashboards tool to see other available dashboards.",
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

// CreateDashboardTool creates a tool to create a new dashboard
func CreateDashboardTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("create_dashboard",
			mcp.WithDescription("Create a new dashboard in the organization. Returns the created dashboard with auto-populated fields (dashboard_id, creator, timestamps)."),
			mcp.WithString("dashboard_definition",
				mcp.Description("JSON string containing the dashboard definition (e.g., {\"dashboard_name\":\"My Dashboard\",\"description\":\"Dashboard description\",\"definition\":{...},\"tags\":[...]})"),
				mcp.Required(),
			),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			orgID, token, err := FetchContextKeys(ctx)
			if err != nil {
				return nil, err
			}

			dashboardDefinition, err := request.RequireString("dashboard_definition")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: dashboard_definition"), err
			}

			// Parse and validate the dashboard definition
			var defMap map[string]interface{}
			if err := json.Unmarshal([]byte(dashboardDefinition), &defMap); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid JSON in dashboard_definition: %v", err)), nil
			}

			// Extract the inner definition object if present (it contains widgets, version, etc.)
			if innerDef, ok := defMap["definition"].(map[string]interface{}); ok {
				validationResult := validation.ValidateDashboard(innerDef)
				if !validationResult.IsValid() {
					var errMsgs []string
					for _, ve := range validationResult.Errors {
						errMsgs = append(errMsgs, fmt.Sprintf("%s: %s", ve.Parameter, ve.Message))
					}
					return mcp.NewToolResultError(fmt.Sprintf("Dashboard validation failed: %s", strings.Join(errMsgs, "; "))), nil
				}
			}

			dashboardURL := fmt.Sprintf("%s/v1/orgs/%s/dashboards", client.APIURL(), orgID)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, dashboardURL, strings.NewReader(dashboardDefinition))
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

			if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to create dashboard, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			return mcp.NewToolResultText(string(bodyBytes)), nil
		}
}

// UpdateDashboardTool creates a tool to update an existing dashboard
func UpdateDashboardTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("update_dashboard",
			mcp.WithDescription("Update an existing dashboard. Immutable fields (creator, created, shared_hash) are preserved automatically. Returns the updated dashboard with refreshed updater and updated timestamps."),
			mcp.WithString("dashboard_id",
				mcp.Description("Dashboard ID to update"),
				mcp.Required(),
			),
			mcp.WithString("dashboard_definition",
				mcp.Description("JSON string containing the dashboard fields to update (e.g., {\"dashboard_name\":\"Updated Name\",\"description\":\"New description\",\"definition\":{...}})"),
				mcp.Required(),
			),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
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

			dashboardDefinition, err := request.RequireString("dashboard_definition")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: dashboard_definition"), err
			}

			// Parse and validate the dashboard definition
			var defMap map[string]interface{}
			if err := json.Unmarshal([]byte(dashboardDefinition), &defMap); err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid JSON in dashboard_definition: %v", err)), nil
			}

			// Extract the inner definition object if present (it contains widgets, version, etc.)
			if innerDef, ok := defMap["definition"].(map[string]interface{}); ok {
				validationResult := validation.ValidateDashboard(innerDef)
				if !validationResult.IsValid() {
					var errMsgs []string
					for _, ve := range validationResult.Errors {
						errMsgs = append(errMsgs, fmt.Sprintf("%s: %s", ve.Parameter, ve.Message))
					}
					return mcp.NewToolResultError(fmt.Sprintf("Dashboard validation failed: %s", strings.Join(errMsgs, "; "))), nil
				}
			}

			dashboardURL := fmt.Sprintf("%s/v1/orgs/%s/dashboards/%s", client.APIURL(), orgID, dashboardID)
			req, err := http.NewRequestWithContext(ctx, http.MethodPut, dashboardURL, strings.NewReader(dashboardDefinition))
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
				return nil, fmt.Errorf("failed to update dashboard, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			return mcp.NewToolResultText(string(bodyBytes)), nil
		}
}

// DeleteDashboardTool creates a tool to delete a dashboard
func DeleteDashboardTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("delete_dashboard",
			mcp.WithDescription("Permanently delete a dashboard from the organization. This operation cannot be undone."),
			mcp.WithString("dashboard_id",
				mcp.Description("Dashboard ID to delete"),
				mcp.Required(),
			),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(true),
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
			req, err := http.NewRequestWithContext(ctx, http.MethodDelete, dashboardURL, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %v", err)
			}

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
				return nil, fmt.Errorf("failed to delete dashboard, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			return mcp.NewToolResultText(fmt.Sprintf("Dashboard %s deleted successfully", dashboardID)), nil
		}
}
