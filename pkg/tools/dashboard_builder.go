package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/tools/validation"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// GetDashboardSchemaTool returns the get_dashboard_schema MCP tool definition and handler.
func GetDashboardSchemaTool() (mcp.Tool, server.ToolHandlerFunc) {
	return mcp.NewTool("get_dashboard_schema",
			mcp.WithDescription(`Returns the complete dashboard v4 schema including all 26 widget types, their required/optional parameters, data source types, and example configurations.

WORKFLOW: Use this tool FIRST to understand available widget types before creating widgets.
1. get_dashboard_schema → discover widget types and parameters
2. create_widget → create individual widget configurations
3. assemble_dashboard → combine widgets into a dashboard

Optional: Filter by category (timeseries, scalar, aggregates, other, layout) to reduce response size.`),
			mcp.WithString("category",
				mcp.Description("Filter widget types by category: timeseries, scalar, aggregates, other, or layout. Leave empty for all types."),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
		),
		getDashboardSchemaHandler
}

func getDashboardSchemaHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	// Get optional category filter
	args := request.GetArguments()
	category, _ := args["category"].(string)

	// Get all widget type schemas
	allSchemas := GetWidgetTypeSchemas()

	// Filter by category if specified
	var filteredSchemas []WidgetTypeSchema
	if category != "" {
		if !ValidateCategory(category) {
			return mcp.NewToolResultError(fmt.Sprintf("Invalid category '%s'. Valid categories: timeseries, scalar, aggregates, other, layout", category)), nil
		}
		for _, schema := range allSchemas {
			if schema.Category == category {
				filteredSchemas = append(filteredSchemas, schema)
			}
		}
	} else {
		filteredSchemas = allSchemas
	}

	// Build response
	response := SchemaResponse{
		Version:            4,
		WidgetTypes:        filteredSchemas,
		DataSourceTypes:    AllDataSourceTypes(),
		AggregationMethods: AllAggregationMethods(),
		VizTypes:           AllWidgetTypes(),
	}

	jsonResponse, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema response: %w", err)
	}

	return mcp.NewToolResultText(string(jsonResponse)), nil
}

// CreateWidgetTool returns the create_widget MCP tool definition and handler.
func CreateWidgetTool() (mcp.Tool, server.ToolHandlerFunc) {
	return mcp.NewTool("create_widget",
			mcp.WithDescription(`Creates a validated widget configuration for use in assemble_dashboard.

PREREQUISITE: Call get_dashboard_schema first to discover available widget types and parameters.

Supports all 26 widget types from the v4 schema. Returns a widget configuration object on success, or validation errors with suggestions on failure.

WORKFLOW:
1. get_dashboard_schema → understand available widget types
2. create_widget (this tool) → create widget config with flat parameters
3. assemble_dashboard → combine widgets into dashboard`),
			// Required parameter
			mcp.WithString("widget_type",
				mcp.Description("Widget type: line, area, bar, scatter, step, smooth (timeseries); bignumber, gauge (scalar); pie, donut, column, radar, sunburst, treemap, sankey, bubble (aggregates); table, raw-table, list, geomap, json, empty, markdown (other); grid, tabs, variable-control (layout)"),
				mcp.Required(),
			),
			// Common optional parameters
			mcp.WithString("title",
				mcp.Description("Widget display title"),
			),
			mcp.WithString("description",
				mcp.Description("Widget tooltip/description text"),
			),
			// Data source parameters
			mcp.WithString("data_source_type",
				mcp.Description("Data source type: log, metric, trace, event, pattern, or formula"),
			),
			mcp.WithString("query",
				mcp.Description("CQL query string for filtering data"),
			),
			mcp.WithString("metric_name",
				mcp.Description("Metric name (required when data_source_type is 'metric')"),
			),
			mcp.WithString("aggregation",
				mcp.Description("Aggregation method: sum, avg, min, max, count, median, p50, p90, p95, p99"),
			),
			mcp.WithArray("group_by",
				mcp.Description("Fields to group by for aggregate widgets (pie, donut, column, radar, sunburst, treemap). Example: ['service.name', 'severity_text']"),
			),
			mcp.WithString("lookback",
				mcp.Description("Time range lookback (e.g., '15m', '1h', '24h', '7d')"),
			),
			// Visualization parameters
			mcp.WithString("show_legend",
				mcp.Description("Legend display mode: auto, always, or never"),
			),
			mcp.WithString("coloring_mode",
				mcp.Description("Color assignment mode: auto, categorical, random, or palette"),
			),
			// Layout parameter
			mcp.WithString("position_area",
				mcp.Description("Grid position area (A, B, C, etc.) for custom layouts"),
			),
			// Markdown-specific
			mcp.WithString("content",
				mcp.Description("Markdown content (required for markdown widget type)"),
			),
			// Variable-control specific
			mcp.WithNumber("variable_id",
				mcp.Description("Variable ID to link to (required for variable-control widget type)"),
			),
			// Tabs-specific
			mcp.WithArray("tab_labels",
				mcp.Description("Array of tab labels (required for tabs widget type). Example: ['Tab 1', 'Tab 2']"),
			),
			mcp.WithNumber("target_tab_id",
				mcp.Description("ID of the tabs widget to place this widget inside (for widgets inside tabs)"),
			),
			mcp.WithNumber("tab_index",
				mcp.Description("Zero-based index of the tab to place this widget in (for widgets inside tabs)"),
			),
			// MCP annotations
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
		),
		createWidgetHandler
}

func createWidgetHandler(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	var errors []ValidationError
	args := request.GetArguments()

	// Extract widget_type (required)
	widgetType, ok := args["widget_type"].(string)
	if !ok || widgetType == "" {
		errors = append(errors, ValidationError{
			Parameter:  "widget_type",
			Message:    "widget_type is required",
			Suggestion: "Provide a widget_type parameter. Use get_dashboard_schema to see valid widget types.",
		})
		return returnValidationErrors(errors)
	}

	// Validate widget_type
	if !ValidateWidgetType(widgetType) {
		errors = append(errors, ValidationError{
			Parameter:  "widget_type",
			Message:    fmt.Sprintf("Invalid widget type '%s'", widgetType),
			Suggestion: fmt.Sprintf("Use one of: %s", strings.Join(AllWidgetTypes(), ", ")),
		})
		return returnValidationErrors(errors)
	}

	// Extract optional parameters
	title, _ := args["title"].(string)
	description, _ := args["description"].(string)
	dataSourceType, _ := args["data_source_type"].(string)
	query, _ := args["query"].(string)
	metricName, _ := args["metric_name"].(string)
	aggregation, _ := args["aggregation"].(string)
	lookback, _ := args["lookback"].(string)
	showLegend, _ := args["show_legend"].(string)
	coloringMode, _ := args["coloring_mode"].(string)
	positionArea, _ := args["position_area"].(string)
	content, _ := args["content"].(string)

	// Variable-control specific
	var variableID int
	if vid, ok := args["variable_id"].(float64); ok {
		variableID = int(vid)
	}

	// Tabs-specific
	var tabLabels []string
	if labels, ok := args["tab_labels"].([]interface{}); ok {
		for _, l := range labels {
			if s, ok := l.(string); ok {
				tabLabels = append(tabLabels, s)
			}
		}
	}
	var targetTabID int
	if tid, ok := args["target_tab_id"].(float64); ok {
		targetTabID = int(tid)
	}
	var tabIndex int
	if idx, ok := args["tab_index"].(float64); ok {
		tabIndex = int(idx)
	}

	// Extract group_by (can be string or array)
	var groupBy []string
	if gb, ok := args["group_by"].([]interface{}); ok {
		for _, v := range gb {
			if s, ok := v.(string); ok {
				groupBy = append(groupBy, s)
			}
		}
	} else if gb, ok := args["group_by"].(string); ok && gb != "" {
		groupBy = []string{gb}
	}

	// Validate data_source_type if provided
	if dataSourceType != "" && !ValidateDataSourceType(dataSourceType) {
		errors = append(errors, ValidationError{
			Parameter:  "data_source_type",
			Message:    fmt.Sprintf("Invalid data source type '%s'", dataSourceType),
			Suggestion: fmt.Sprintf("Use one of: %s", strings.Join(AllDataSourceTypes(), ", ")),
		})
	}

	// Validate aggregation if provided
	if aggregation != "" && !ValidateAggregation(aggregation) {
		errors = append(errors, ValidationError{
			Parameter:  "aggregation",
			Message:    fmt.Sprintf("Invalid aggregation method '%s'", aggregation),
			Suggestion: fmt.Sprintf("Use one of: %s", strings.Join(AllAggregationMethods(), ", ")),
		})
	}

	// Cross-parameter validation: data_source_type required for most widgets
	if RequiresDataSource(widgetType) && dataSourceType == "" {
		errors = append(errors, ValidationError{
			Parameter:  "data_source_type",
			Message:    fmt.Sprintf("data_source_type is required for %s widget", widgetType),
			Suggestion: fmt.Sprintf("Add data_source_type parameter. Valid values: %s", strings.Join(AllDataSourceTypes(), ", ")),
		})
	}

	// Cross-parameter validation: metric_name required for metric data source
	if dataSourceType == DataSourceTypeMetric && metricName == "" {
		errors = append(errors, ValidationError{
			Parameter:  "metric_name",
			Message:    "metric_name is required when data_source_type is 'metric'",
			Suggestion: "Add metric_name parameter with a valid metric name (e.g., 'http.request.duration')",
		})
	}

	// Cross-parameter validation: group_by required for certain widget types
	if RequiresGroupBy(widgetType) && len(groupBy) == 0 {
		errors = append(errors, ValidationError{
			Parameter:  "group_by",
			Message:    fmt.Sprintf("group_by is required for %s widget", widgetType),
			Suggestion: "Add group_by parameter with field names to group by (e.g., 'service', 'region')",
		})
	}

	// Cross-parameter validation: aggregation required for scalar widgets
	if RequiresAggregation(widgetType) && aggregation == "" {
		errors = append(errors, ValidationError{
			Parameter:  "aggregation",
			Message:    fmt.Sprintf("aggregation is required for %s widget", widgetType),
			Suggestion: fmt.Sprintf("Add aggregation parameter. Valid values: %s", strings.Join(AllAggregationMethods(), ", ")),
		})
	}

	// Cross-parameter validation: content required for markdown widget
	if widgetType == WidgetTypeMarkdown && content == "" {
		errors = append(errors, ValidationError{
			Parameter:  "content",
			Message:    "content is required for markdown widget",
			Suggestion: "Add content parameter with markdown text",
		})
	}

	// Cross-parameter validation: variable_id required for variable-control widget
	if widgetType == WidgetTypeVariableControl && variableID == 0 {
		errors = append(errors, ValidationError{
			Parameter:  "variable_id",
			Message:    "variable_id is required for variable-control widget",
			Suggestion: "Add variable_id parameter matching a variable defined in assemble_dashboard",
		})
	}

	// Cross-parameter validation: tab_labels required for tabs widget
	if widgetType == WidgetTypeTabs && len(tabLabels) == 0 {
		errors = append(errors, ValidationError{
			Parameter:  "tab_labels",
			Message:    "tab_labels is required for tabs widget",
			Suggestion: "Add tab_labels parameter with array of tab names, e.g., ['Tab 1', 'Tab 2']",
		})
	}

	// Return validation errors if any
	if len(errors) > 0 {
		return returnValidationErrors(errors)
	}

	// Build widget config (ID assigned at assembly time to avoid race conditions)
	widget := WidgetConfig{
		Type:           widgetType,
		Title:          title,
		Description:    description,
		DataSourceType: dataSourceType,
		Query:          query,
		MetricName:     metricName,
		Aggregation:    aggregation,
		GroupBy:        groupBy,
		Lookback:       lookback,
		ShowLegend:     showLegend,
		ColoringMode:   coloringMode,
		PositionArea:   positionArea,
		Content:        content,
		VariableID:     variableID,
		TabLabels:      tabLabels,
		TargetTabID:    targetTabID,
		TabIndex:       tabIndex,
	}

	// Return success response
	response := WidgetCreationResponse{Widget: widget}
	jsonResponse, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal widget response: %w", err)
	}

	return mcp.NewToolResultText(string(jsonResponse)), nil
}

func returnValidationErrors(errors []ValidationError) (*mcp.CallToolResult, error) {
	response := ValidationErrorResponse{
		Error:            true,
		ValidationErrors: errors,
	}
	jsonResponse, err := json.Marshal(response)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal validation errors: %w", err)
	}
	return mcp.NewToolResultText(string(jsonResponse)), nil
}

// AssembleDashboardTool returns the assemble_dashboard MCP tool definition and handler.
func AssembleDashboardTool(client Client) (mcp.Tool, server.ToolHandlerFunc) {
	return mcp.NewTool("assemble_dashboard",
			mcp.WithDescription(`Combines widget configurations from create_widget into a complete dashboard and creates it in EdgeDelta via API.

PREREQUISITES:
1. Call get_dashboard_schema to understand widget types
2. Call create_widget for each widget you want to add
3. Call this tool with the widget configurations

Returns the created dashboard_id on success. Widgets are arranged in a single-column grid layout by default unless position_area hints are provided.`),
			mcp.WithString("name",
				mcp.Description("Dashboard name (required)"),
				mcp.Required(),
			),
			mcp.WithString("description",
				mcp.Description("Dashboard description"),
			),
			mcp.WithString("lookback",
				mcp.Description("Default time filter lookback for the dashboard (e.g., '1h', '24h'). Defaults to '1h'"),
			),
			mcp.WithArray("widgets",
				mcp.Description("Array of widget configurations from create_widget (required, minimum 1)"),
				mcp.Required(),
			),
			mcp.WithArray("tags",
				mcp.Description("List of tags for organizing the dashboard"),
			),
			mcp.WithArray("variables",
				mcp.Description("Array of variable configurations for dashboard filters. Each variable needs: id (int), key (string), label (string), type ('facet-option'), facet (string), scope ('log'|'metric'), allow_empty (bool), allow_multiple (bool), auto_select (bool)"),
			),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return assembleDashboardHandler(ctx, request, client)
		}
}

func assembleDashboardHandler(ctx context.Context, request mcp.CallToolRequest, client Client) (*mcp.CallToolResult, error) {
	args := request.GetArguments()

	// Extract parameters
	name, _ := args["name"].(string)
	if name == "" {
		return mcp.NewToolResultError("name is required for dashboard creation"), nil
	}

	description, _ := args["description"].(string)
	lookback, _ := args["lookback"].(string)
	if lookback == "" {
		lookback = "1h"
	}

	// Extract tags
	var tags []string
	if tagsArg, ok := args["tags"].([]interface{}); ok {
		for _, t := range tagsArg {
			if s, ok := t.(string); ok {
				tags = append(tags, s)
			}
		}
	}

	// Extract variables for dashboard filters
	var variables []map[string]interface{}
	if varsArg, ok := args["variables"].([]interface{}); ok {
		for _, v := range varsArg {
			if varMap, ok := v.(map[string]interface{}); ok {
				variable := parseVariableFromMap(varMap)
				variables = append(variables, variable)
			}
		}
	}

	// Extract widgets
	var widgets []WidgetConfig
	if widgetsArg, ok := args["widgets"].([]interface{}); ok {
		for _, w := range widgetsArg {
			if widgetMap, ok := w.(map[string]interface{}); ok {
				widget := parseWidgetFromMap(widgetMap)
				widgets = append(widgets, widget)
			}
		}
	}

	// Validate widgets
	if len(widgets) == 0 {
		return mcp.NewToolResultText(marshalErrorResponse("Dashboard must contain at least one widget", "The widgets array is empty. Use create_widget to create widget configurations first.")), nil
	}

	// Build dashboard definition for API (v4 schema)
	definition := map[string]interface{}{
		"version": 4,
		"timeFilters": map[string]interface{}{
			"lookback": lookback,
		},
		"widgets": buildV4Widgets(widgets),
	}

	// Add variables if provided
	if len(variables) > 0 {
		definition["variables"] = variables
	}

	// Validate the assembled dashboard definition
	validationResult := validation.ValidateDashboard(definition)
	if !validationResult.IsValid() {
		// Convert validation errors to our format
		var validationErrors []ValidationError
		for _, ve := range validationResult.Errors {
			validationErrors = append(validationErrors, ValidationError{
				Parameter:  ve.Parameter,
				Message:    ve.Message,
				Suggestion: ve.Suggestion,
			})
		}
		return returnValidationErrors(validationErrors)
	}

	dashboardDef := map[string]interface{}{
		"dashboard_name": name,
		"description":    description,
		"tags":           tags,
		"definition":     definition,
	}

	// Make API call
	orgID, token, err := FetchContextKeys(ctx)
	if err != nil {
		return mcp.NewToolResultText(marshalErrorResponse("Authentication failed", err.Error())), nil
	}

	dashboardJSON, err := json.Marshal(dashboardDef)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal dashboard definition: %w", err)
	}

	dashboardURL := fmt.Sprintf("%s/v1/orgs/%s/dashboards", client.APIURL(), orgID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dashboardURL, strings.NewReader(string(dashboardJSON)))
	if err != nil {
		return mcp.NewToolResultText(marshalErrorResponse("Failed to create request", err.Error())), nil
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("X-ED-API-Token", token)

	resp, err := client.Do(req)
	if err != nil {
		return mcp.NewToolResultText(marshalErrorResponse("API request failed", err.Error())), nil
	}
	defer resp.Body.Close()

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return mcp.NewToolResultText(marshalErrorResponse("Failed to read response", err.Error())), nil
	}

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		return mcp.NewToolResultText(marshalErrorResponse(
			fmt.Sprintf("Failed to create dashboard (status %d)", resp.StatusCode),
			string(bodyBytes),
		)), nil
	}

	// Parse response to get dashboard ID
	var apiResponse map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &apiResponse); err != nil {
		return mcp.NewToolResultText(marshalErrorResponse("Failed to parse response", err.Error())), nil
	}

	dashboardID, _ := apiResponse["dashboard_id"].(string)
	if dashboardID == "" {
		if id, ok := apiResponse["id"].(string); ok {
			dashboardID = id
		}
	}

	// Build success response
	successResponse := AssembleDashboardResponse{
		DashboardID: dashboardID,
		Name:        name,
		WidgetCount: len(widgets),
		URL:         fmt.Sprintf("https://app.edgedelta.com/dashboards/%s", dashboardID),
	}

	jsonResponse, err := json.Marshal(successResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal success response: %w", err)
	}

	return mcp.NewToolResultText(string(jsonResponse)), nil
}

func parseWidgetFromMap(m map[string]interface{}) WidgetConfig {
	widget := WidgetConfig{}

	if id, ok := m["id"].(float64); ok {
		widget.ID = int(id)
	}
	if t, ok := m["type"].(string); ok {
		widget.Type = t
	}
	if title, ok := m["title"].(string); ok {
		widget.Title = title
	}
	if desc, ok := m["description"].(string); ok {
		widget.Description = desc
	}
	if dst, ok := m["data_source_type"].(string); ok {
		widget.DataSourceType = dst
	}
	if q, ok := m["query"].(string); ok {
		widget.Query = q
	}
	if mn, ok := m["metric_name"].(string); ok {
		widget.MetricName = mn
	}
	if agg, ok := m["aggregation"].(string); ok {
		widget.Aggregation = agg
	}
	if lb, ok := m["lookback"].(string); ok {
		widget.Lookback = lb
	}
	if sl, ok := m["show_legend"].(string); ok {
		widget.ShowLegend = sl
	}
	if cm, ok := m["coloring_mode"].(string); ok {
		widget.ColoringMode = cm
	}
	if pa, ok := m["position_area"].(string); ok {
		widget.PositionArea = pa
	}
	if c, ok := m["content"].(string); ok {
		widget.Content = c
	} else if params, ok := m["params"].(map[string]interface{}); ok {
		// Fallback: check params.content (v4 format)
		if c, ok := params["content"].(string); ok {
			widget.Content = c
		}
	}
	if gb, ok := m["group_by"].([]interface{}); ok {
		for _, v := range gb {
			if s, ok := v.(string); ok {
				widget.GroupBy = append(widget.GroupBy, s)
			}
		}
	}

	// Variable-control specific
	if vid, ok := m["variable_id"].(float64); ok {
		widget.VariableID = int(vid)
	}

	// Tabs-specific
	if labels, ok := m["tab_labels"].([]interface{}); ok {
		for _, l := range labels {
			if s, ok := l.(string); ok {
				widget.TabLabels = append(widget.TabLabels, s)
			}
		}
	}
	if tid, ok := m["target_tab_id"].(float64); ok {
		widget.TargetTabID = int(tid)
	}
	if idx, ok := m["tab_index"].(float64); ok {
		widget.TabIndex = int(idx)
	}

	return widget
}

// buildV4Widgets converts simplified WidgetConfig structs to EdgeDelta v4 API format.
// Returns the root grid widget followed by all content widgets.
func buildV4Widgets(widgets []WidgetConfig) []map[string]interface{} {
	var result []map[string]interface{}

	// Calculate grid dimensions based on widget count
	// Use 12 columns (standard), rows based on widget layout
	numWidgets := len(widgets)
	rowsNeeded := ((numWidgets + 1) / 2) * 4 // 4 rows per widget row, 2 widgets per row

	// Build grid template string: "72px 72px ... / 1fr 1fr ... 1fr" (12 columns)
	var rowParts []string
	for i := 0; i < rowsNeeded; i++ {
		rowParts = append(rowParts, "72px")
	}
	gridTemplate := strings.Join(rowParts, " ") + " / 1fr 1fr 1fr 1fr 1fr 1fr 1fr 1fr 1fr 1fr 1fr 1fr"

	// Create root grid widget
	rootGrid := map[string]interface{}{
		"displayOptions": map[string]interface{}{
			"hideBackground": true,
		},
		"grid": gridTemplate,
		"id":   "root",
		"type": "grid",
	}
	result = append(result, rootGrid)

	// Convert each widget to v4 format (IDs assigned here to avoid race conditions)
	for i, w := range widgets {
		widgetID := i + 1 // 1-indexed widget IDs
		var position map[string]interface{}

		// Check if this widget should be positioned inside a tab
		if w.TargetTabID > 0 {
			// Widget goes inside a tab container
			position = map[string]interface{}{
				"type":     "tab",
				"targetId": w.TargetTabID,
				"index":    w.TabIndex,
			}
		} else {
			// Standard grid position (2 widgets per row, 6 columns each)
			col := (i%2)*6 + 1 // 1 or 7
			row := (i/2)*4 + 1 // 1, 5, 9, ...
			colSpan := 6
			rowSpan := 4

			position = map[string]interface{}{
				"area": map[string]interface{}{
					"column":     col,
					"columnSpan": colSpan,
					"row":        row,
					"rowSpan":    rowSpan,
				},
				"targetId": "root",
				"type":     "grid",
			}
		}

		var widget map[string]interface{}

		// Handle special widget types that don't use "viz" type
		switch w.Type {
		case "markdown":
			widget = map[string]interface{}{
				"type":     "markdown",
				"id":       widgetID,
				"position": position,
				"displayOptions": map[string]interface{}{
					"hideBackground": false,
				},
				"params": map[string]interface{}{
					"content": w.Content,
				},
			}
		case "empty":
			widget = map[string]interface{}{
				"type":     "empty",
				"id":       widgetID,
				"position": position,
				"displayOptions": map[string]interface{}{
					"title": w.Title,
				},
			}
		case "variable-control":
			widget = map[string]interface{}{
				"type":       "variable-control",
				"id":         widgetID,
				"position":   position,
				"variableId": w.VariableID,
				"displayOptions": map[string]interface{}{
					"hideBackground": true,
				},
			}
		case "tabs":
			// Build items array from tab labels
			items := make([]map[string]interface{}, 0, len(w.TabLabels))
			for _, label := range w.TabLabels {
				items = append(items, map[string]interface{}{
					"label": label,
				})
			}
			widget = map[string]interface{}{
				"type":     "tabs",
				"id":       widgetID,
				"position": position,
				"items":    items,
				"displayOptions": map[string]interface{}{
					"hideBackground": true,
				},
			}
		case "grid":
			// Nested grid container
			widget = map[string]interface{}{
				"type":     "grid",
				"id":       widgetID,
				"position": position,
				"grid":     "72px 72px 72px 72px / 1fr 1fr 1fr 1fr 1fr 1fr 1fr 1fr 1fr 1fr 1fr 1fr",
				"displayOptions": map[string]interface{}{
					"hideBackground": true,
				},
			}
		default:
			// Standard viz widget
			widget = map[string]interface{}{
				"type": "viz",
				"id":   widgetID,
				"displayOptions": map[string]interface{}{
					"title":       w.Title,
					"description": w.Description,
				},
				"position":   position,
				"resultType": getResultType(w.Type),
				"visualizer": buildVisualizer(w),
				"visuals":    buildVisuals(w),
			}
		}

		result = append(result, widget)
	}

	return result
}

// getResultType maps widget visualization type to v4 resultType.
func getResultType(vizType string) string {
	switch vizType {
	case "line", "area", "bar", "scatter", "step", "smooth":
		return "timeseries"
	case "bignumber", "gauge":
		return "aggregate"
	case "pie", "donut", "column", "radar", "sunburst", "treemap", "sankey", "bubble":
		return "aggregate"
	case "table", "list":
		return "aggregate"
	case "geomap":
		return "aggregate"
	case "json":
		// json supports both timeseries and aggregate; default to timeseries
		return "timeseries"
	case "raw-table":
		return "raw"
	default:
		return "timeseries"
	}
}

// buildVisualizer creates the v4 visualizer object.
func buildVisualizer(w WidgetConfig) map[string]interface{} {
	viz := map[string]interface{}{
		"type": w.Type,
	}

	// Add format options for certain types
	if w.Type == "line" || w.Type == "area" || w.Type == "bar" {
		viz["format"] = map[string]interface{}{
			"formatOptions": map[string]interface{}{},
			"options":       map[string]interface{}{},
		}
	}

	// Add coloring if specified
	if w.ColoringMode != "" {
		viz["coloring"] = map[string]interface{}{
			"mode": w.ColoringMode,
		}
	}

	return viz
}

// buildVisuals creates the v4 visuals array with dataSource.
func buildVisuals(w WidgetConfig) []map[string]interface{} {
	if w.DataSourceType == "" && w.Query == "" && w.MetricName == "" {
		return []map[string]interface{}{}
	}

	// Map our data source types to v4 types
	dsType := w.DataSourceType
	if dsType == "formula" {
		dsType = "metric" // formula queries use metric type in v4
	}

	var query string

	// Build metric query from metric_name and aggregation if provided
	if dsType == "metric" && w.MetricName != "" && w.Query == "" {
		// Format: {aggregation}:{metric_name}{filter}.rollup(interval)
		// Example: sum:istio_requests_total{*}.rollup(60)
		agg := w.Aggregation
		if agg == "" {
			agg = "sum" // default aggregation
		}
		query = fmt.Sprintf("%s:%s{*}.rollup(60)", agg, w.MetricName)
	} else {
		// Use provided query directly
		query = w.Query
		// Fix empty filter syntax: {} should be {*} for "all"
		query = strings.ReplaceAll(query, "{}", "{*}")
	}

	// Append group_by clause for log queries with aggregate widgets
	// Format: "{*} by {field1, field2}"
	if len(w.GroupBy) > 0 && (dsType == "log" || dsType == "trace" || dsType == "event") {
		groupByClause := " by {" + strings.Join(w.GroupBy, ", ") + "}"
		query = query + groupByClause
	}

	dataSource := map[string]interface{}{
		"type": dsType,
		"params": map[string]interface{}{
			"query": query,
		},
	}

	return []map[string]interface{}{
		{
			"id":         "A",
			"dataSource": dataSource,
		},
	}
}

func parseVariableFromMap(m map[string]interface{}) map[string]interface{} {
	// Build v4 variable structure
	variable := map[string]interface{}{}

	if id, ok := m["id"].(float64); ok {
		variable["id"] = int(id)
	}
	if key, ok := m["key"].(string); ok {
		variable["key"] = key
	}
	if label, ok := m["label"].(string); ok {
		variable["label"] = label
	}
	if t, ok := m["type"].(string); ok {
		variable["type"] = t
	}

	// Build params object
	params := map[string]interface{}{}
	if facet, ok := m["facet"].(string); ok {
		params["facet"] = facet
	}
	if scope, ok := m["scope"].(string); ok {
		params["scope"] = scope
	}
	if allowMultiple, ok := m["allow_multiple"].(bool); ok {
		params["allowMultiple"] = allowMultiple
	}
	if allowEmpty, ok := m["allow_empty"].(bool); ok {
		params["allowEmpty"] = allowEmpty
	}
	if autoSelect, ok := m["auto_select"].(bool); ok {
		params["autoSelect"] = autoSelect
	}

	if len(params) > 0 {
		variable["params"] = params
	}

	// Set value to null for initial state
	variable["value"] = nil

	return variable
}

func marshalErrorResponse(message, details string) string {
	response := AssembleDashboardErrorResponse{
		Error:   true,
		Message: message,
		Details: details,
	}
	jsonResponse, _ := json.Marshal(response)
	return string(jsonResponse)
}
