package tools

import "github.com/edgedelta/edgedelta-mcp-server/pkg/tools/validation"

// WidgetConfig is the output of create_widget, passed to assemble_dashboard.
type WidgetConfig struct {
	// Identity
	ID   int    `json:"id"`   // Auto-generated unique ID
	Type string `json:"type"` // Widget type (line, bar, bignumber, etc.)

	// Display
	Title       string `json:"title,omitempty"`
	Description string `json:"description,omitempty"`

	// Data Source
	DataSourceType string   `json:"data_source_type,omitempty"` // log, metric, trace, event, pattern, formula
	Query          string   `json:"query,omitempty"`            // CQL query string
	MetricName     string   `json:"metric_name,omitempty"`      // For metric data sources
	Aggregation    string   `json:"aggregation,omitempty"`      // sum, avg, min, max, count, median
	GroupBy        []string `json:"group_by,omitempty"`         // Grouping keys
	Lookback       string   `json:"lookback,omitempty"`         // Time range (e.g., "1h", "24h")

	// Visualization
	VizType      string `json:"viz_type,omitempty"`      // Override default visualization
	ShowLegend   string `json:"show_legend,omitempty"`   // auto, always, never
	ColoringMode string `json:"coloring_mode,omitempty"` // auto, categorical, random, palette

	// Layout (optional) - grid position fields (1-indexed, 12-column grid)
	Column     int `json:"column,omitempty"`      // Grid column start (1-12)
	ColumnSpan int `json:"column_span,omitempty"` // Column span (1-12, default 6)
	Row        int `json:"row,omitempty"`         // Grid row start (1-based)
	RowSpan    int `json:"row_span,omitempty"`    // Row span (default 4)

	// Markdown-specific
	Content string `json:"content,omitempty"` // For markdown widget type

	// Variable-control specific
	VariableID int `json:"variable_id,omitempty"` // Links to a dashboard variable

	// Tabs-specific
	TabLabels   []string `json:"tab_labels,omitempty"`    // Labels for tab items
	TargetTabID int      `json:"target_tab_id,omitempty"` // For widgets inside a tab
	TabIndex    int      `json:"tab_index,omitempty"`     // Index of the tab (0-based)
}

// VariableConfig defines a dashboard variable for filtering.
type VariableConfig struct {
	ID            int    `json:"id"`                       // Unique variable ID
	Key           string `json:"key"`                      // Variable key used in queries (e.g., "fleet", "host")
	Label         string `json:"label"`                    // Display label
	Type          string `json:"type"`                     // Variable type: facet-option, string, duration, query, facet
	Facet         string `json:"facet,omitempty"`          // Facet name (e.g., "ed.tag", "host.name")
	Scope         string `json:"scope,omitempty"`          // Data scope: log, metric, trace, event
	AllowMultiple bool   `json:"allow_multiple,omitempty"` // Allow multiple selections
	AllowEmpty    bool   `json:"allow_empty,omitempty"`    // Allow empty selection
	AutoSelect    bool   `json:"auto_select,omitempty"`    // Auto-select first value
}

// WidgetTypeSchema describes a widget type for schema discovery.
type WidgetTypeSchema struct {
	Type           string         `json:"type"`            // Widget type identifier
	Category       string         `json:"category"`        // timeseries, scalar, aggregates, other, layout
	Description    string         `json:"description"`     // Human-readable description
	RequiredParams []string       `json:"required_params"` // Required parameter names
	OptionalParams []string       `json:"optional_params"` // Optional parameter names
	Example        map[string]any `json:"example"`         // Example configuration
}

// DashboardDefinition is the assembled dashboard structure for API submission.
type DashboardDefinition struct {
	Name         string         `json:"name"`
	Description  string         `json:"description,omitempty"`
	Tags         []string       `json:"tags,omitempty"`
	Version      int            `json:"version"` // Always 4
	TimeFilter   TimeFilter     `json:"time_filter"`
	Widgets      []WidgetConfig `json:"widgets"`
	GridTemplate string         `json:"grid_template,omitempty"` // Optional custom grid
}

// TimeFilter defines the time range for dashboard queries.
type TimeFilter struct {
	Type     string `json:"type"`     // relative
	Lookback string `json:"lookback"` // Default: "1h"
}

// SchemaResponse is the response from get_dashboard_schema.
type SchemaResponse struct {
	Version            int                `json:"version"` // Schema version (4)
	WidgetTypes        []WidgetTypeSchema `json:"widget_types"`
	DataSourceTypes    []string           `json:"data_source_types"`
	AggregationMethods []string           `json:"aggregation_methods"`
	VizTypes           []string           `json:"viz_types"`
}

// ValidationError is an alias for validation.ValidationError (single source of truth).
type ValidationError = validation.ValidationError

// ValidationErrorResponse is returned when widget creation fails validation.
type ValidationErrorResponse struct {
	Error            bool              `json:"error"`
	ValidationErrors []ValidationError `json:"validation_errors"`
}

// WidgetCreationResponse is returned on successful widget creation.
type WidgetCreationResponse struct {
	Widget WidgetConfig `json:"widget"`
}

// AssembleDashboardResponse is returned on successful dashboard assembly.
type AssembleDashboardResponse struct {
	DashboardID string `json:"dashboard_id"`
	Name        string `json:"name"`
	WidgetCount int    `json:"widget_count"`
	URL         string `json:"url,omitempty"`
}

// AssembleDashboardErrorResponse is returned when dashboard assembly fails.
type AssembleDashboardErrorResponse struct {
	Error   bool   `json:"error"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}
