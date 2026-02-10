package tools

import "slices"

// Widget type constants - 26 types across 5 categories
const (
	// Timeseries (6 types)
	WidgetTypeLine    = "line"
	WidgetTypeArea    = "area"
	WidgetTypeBar     = "bar"
	WidgetTypeScatter = "scatter"
	WidgetTypeStep    = "step"
	WidgetTypeSmooth  = "smooth"

	// Scalar (2 types)
	WidgetTypeBigNumber = "bignumber"
	WidgetTypeGauge     = "gauge"

	// Aggregates (8 types)
	WidgetTypePie      = "pie"
	WidgetTypeDonut    = "donut"
	WidgetTypeColumn   = "column"
	WidgetTypeRadar    = "radar"
	WidgetTypeSunburst = "sunburst"
	WidgetTypeTreemap  = "treemap"
	WidgetTypeSankey   = "sankey"
	WidgetTypeBubble   = "bubble"

	// Other (7 types)
	WidgetTypeTable    = "table"
	WidgetTypeRawTable = "raw-table"
	WidgetTypeList     = "list"
	WidgetTypeGeomap   = "geomap"
	WidgetTypeJSON     = "json"
	WidgetTypeEmpty    = "empty"
	WidgetTypeMarkdown = "markdown"

	// Layout (3 types)
	WidgetTypeGrid            = "grid"
	WidgetTypeTabs            = "tabs"
	WidgetTypeVariableControl = "variable-control"
)

// Widget category constants
const (
	CategoryTimeseries = "timeseries"
	CategoryScalar     = "scalar"
	CategoryAggregates = "aggregates"
	CategoryOther      = "other"
	CategoryLayout     = "layout"
)

// Data source type constants
const (
	DataSourceTypeLog     = "log"
	DataSourceTypeMetric  = "metric"
	DataSourceTypeTrace   = "trace"
	DataSourceTypeEvent   = "event"
	DataSourceTypePattern = "pattern"
	DataSourceTypeFormula = "formula"
)

// Aggregation method constants
const (
	AggregationSum    = "sum"
	AggregationAvg    = "avg"
	AggregationMin    = "min"
	AggregationMax    = "max"
	AggregationCount  = "count"
	AggregationMedian = "median"
	AggregationP50    = "p50"
	AggregationP90    = "p90"
	AggregationP95    = "p95"
	AggregationP99    = "p99"
)

// Show legend constants
const (
	ShowLegendAuto   = "auto"
	ShowLegendAlways = "always"
	ShowLegendNever  = "never"
)

// Coloring mode constants
const (
	ColoringModeAuto        = "auto"
	ColoringModeCategorical = "categorical"
	ColoringModeRandom      = "random"
	ColoringModePalette     = "palette"
)

// AllWidgetTypes returns all 26 supported widget types.
func AllWidgetTypes() []string {
	return []string{
		// Timeseries
		WidgetTypeLine, WidgetTypeArea, WidgetTypeBar, WidgetTypeScatter, WidgetTypeStep, WidgetTypeSmooth,
		// Scalar
		WidgetTypeBigNumber, WidgetTypeGauge,
		// Aggregates
		WidgetTypePie, WidgetTypeDonut, WidgetTypeColumn, WidgetTypeRadar, WidgetTypeSunburst, WidgetTypeTreemap, WidgetTypeSankey, WidgetTypeBubble,
		// Other
		WidgetTypeTable, WidgetTypeRawTable, WidgetTypeList, WidgetTypeGeomap, WidgetTypeJSON, WidgetTypeEmpty, WidgetTypeMarkdown,
		// Layout
		WidgetTypeGrid, WidgetTypeTabs, WidgetTypeVariableControl,
	}
}

// AllDataSourceTypes returns all supported data source types.
func AllDataSourceTypes() []string {
	return []string{
		DataSourceTypeLog,
		DataSourceTypeMetric,
		DataSourceTypeTrace,
		DataSourceTypeEvent,
		DataSourceTypePattern,
		DataSourceTypeFormula,
	}
}

// AllAggregationMethods returns all supported aggregation methods.
func AllAggregationMethods() []string {
	return []string{
		AggregationSum,
		AggregationAvg,
		AggregationMin,
		AggregationMax,
		AggregationCount,
		AggregationMedian,
		AggregationP50,
		AggregationP90,
		AggregationP95,
		AggregationP99,
	}
}

// AllCategories returns all widget categories.
func AllCategories() []string {
	return []string{
		CategoryTimeseries,
		CategoryScalar,
		CategoryAggregates,
		CategoryOther,
		CategoryLayout,
	}
}

// ValidateWidgetType checks if the widget type is valid.
func ValidateWidgetType(widgetType string) bool {
	return slices.Contains(AllWidgetTypes(), widgetType)
}

// ValidateDataSourceType checks if the data source type is valid.
func ValidateDataSourceType(dsType string) bool {
	return slices.Contains(AllDataSourceTypes(), dsType)
}

// ValidateAggregation checks if the aggregation method is valid.
func ValidateAggregation(agg string) bool {
	return slices.Contains(AllAggregationMethods(), agg)
}

// ValidateCategory checks if the category is valid.
func ValidateCategory(category string) bool {
	return slices.Contains(AllCategories(), category)
}

// GetWidgetCategory returns the category for a widget type.
func GetWidgetCategory(widgetType string) string {
	switch widgetType {
	case WidgetTypeLine, WidgetTypeArea, WidgetTypeBar, WidgetTypeScatter, WidgetTypeStep, WidgetTypeSmooth:
		return CategoryTimeseries
	case WidgetTypeBigNumber, WidgetTypeGauge:
		return CategoryScalar
	case WidgetTypePie, WidgetTypeDonut, WidgetTypeColumn, WidgetTypeRadar, WidgetTypeSunburst, WidgetTypeTreemap, WidgetTypeSankey, WidgetTypeBubble:
		return CategoryAggregates
	case WidgetTypeTable, WidgetTypeRawTable, WidgetTypeList, WidgetTypeGeomap, WidgetTypeJSON, WidgetTypeEmpty, WidgetTypeMarkdown:
		return CategoryOther
	case WidgetTypeGrid, WidgetTypeTabs, WidgetTypeVariableControl:
		return CategoryLayout
	default:
		return ""
	}
}

// RequiresGroupBy returns true if the widget type requires the group_by parameter.
func RequiresGroupBy(widgetType string) bool {
	switch widgetType {
	case WidgetTypePie, WidgetTypeDonut, WidgetTypeColumn, WidgetTypeRadar, WidgetTypeSunburst, WidgetTypeTreemap:
		return true
	default:
		return false
	}
}

// RequiresAggregation returns true if the widget type requires the aggregation parameter.
func RequiresAggregation(widgetType string) bool {
	switch widgetType {
	case WidgetTypeBigNumber, WidgetTypeGauge:
		return true
	default:
		return false
	}
}

// RequiresDataSource returns true if the widget type requires data source configuration.
func RequiresDataSource(widgetType string) bool {
	switch widgetType {
	case WidgetTypeEmpty, WidgetTypeMarkdown, WidgetTypeGrid, WidgetTypeTabs, WidgetTypeVariableControl:
		return false
	default:
		return true
	}
}

// GetWidgetTypeSchemas returns schema definitions for all 26 widget types with examples.
func GetWidgetTypeSchemas() []WidgetTypeSchema {
	return []WidgetTypeSchema{
		// Timeseries widgets (6)
		{
			Type:           WidgetTypeLine,
			Category:       CategoryTimeseries,
			Description:    "Line chart for time-series data visualization",
			RequiredParams: []string{"data_source_type"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "aggregation", "group_by", "lookback", "show_legend", "coloring_mode", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeLine,
				"title":            "API Latency Over Time",
				"data_source_type": DataSourceTypeMetric,
				"metric_name":      "http.request.duration",
				"aggregation":      AggregationAvg,
				"lookback":         "1h",
			},
		},
		{
			Type:           WidgetTypeArea,
			Category:       CategoryTimeseries,
			Description:    "Stacked area chart for cumulative time-series visualization",
			RequiredParams: []string{"data_source_type"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "aggregation", "group_by", "lookback", "show_legend", "coloring_mode", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeArea,
				"title":            "Request Volume by Service",
				"data_source_type": DataSourceTypeMetric,
				"metric_name":      "http.requests",
				"aggregation":      AggregationSum,
				"group_by":         []string{"service"},
			},
		},
		{
			Type:           WidgetTypeBar,
			Category:       CategoryTimeseries,
			Description:    "Vertical bar chart over time",
			RequiredParams: []string{"data_source_type"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "aggregation", "group_by", "lookback", "show_legend", "coloring_mode", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeBar,
				"title":            "Errors per Hour",
				"data_source_type": DataSourceTypeLog,
				"query":            "level:error",
				"aggregation":      AggregationCount,
			},
		},
		{
			Type:           WidgetTypeScatter,
			Category:       CategoryTimeseries,
			Description:    "Scatter plot for correlation analysis",
			RequiredParams: []string{"data_source_type"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "aggregation", "group_by", "lookback", "show_legend", "coloring_mode", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeScatter,
				"title":            "Latency vs Throughput",
				"data_source_type": DataSourceTypeMetric,
				"metric_name":      "http.request.duration",
			},
		},
		{
			Type:           WidgetTypeStep,
			Category:       CategoryTimeseries,
			Description:    "Step line chart showing discrete value changes",
			RequiredParams: []string{"data_source_type"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "aggregation", "group_by", "lookback", "show_legend", "coloring_mode", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeStep,
				"title":            "Pod Count Over Time",
				"data_source_type": DataSourceTypeMetric,
				"metric_name":      "kubernetes.pod.count",
			},
		},
		{
			Type:           WidgetTypeSmooth,
			Category:       CategoryTimeseries,
			Description:    "Smoothed line chart with curve interpolation",
			RequiredParams: []string{"data_source_type"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "aggregation", "group_by", "lookback", "show_legend", "coloring_mode", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeSmooth,
				"title":            "CPU Utilization Trend",
				"data_source_type": DataSourceTypeMetric,
				"metric_name":      "system.cpu.usage",
				"aggregation":      AggregationAvg,
			},
		},

		// Scalar widgets (2)
		{
			Type:           WidgetTypeBigNumber,
			Category:       CategoryScalar,
			Description:    "Single large metric value for key KPIs",
			RequiredParams: []string{"data_source_type", "aggregation"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "lookback", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeBigNumber,
				"title":            "Total Errors (24h)",
				"data_source_type": DataSourceTypeLog,
				"query":            "level:error",
				"aggregation":      AggregationCount,
				"lookback":         "24h",
			},
		},
		{
			Type:           WidgetTypeGauge,
			Category:       CategoryScalar,
			Description:    "Gauge with min/max range for threshold visualization",
			RequiredParams: []string{"data_source_type", "aggregation"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "lookback", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeGauge,
				"title":            "CPU Usage",
				"data_source_type": DataSourceTypeMetric,
				"metric_name":      "system.cpu.usage",
				"aggregation":      AggregationAvg,
			},
		},

		// Aggregates widgets (8)
		{
			Type:           WidgetTypePie,
			Category:       CategoryAggregates,
			Description:    "Pie chart for proportional distribution",
			RequiredParams: []string{"data_source_type", "group_by"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "aggregation", "lookback", "show_legend", "coloring_mode", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypePie,
				"title":            "Errors by Service",
				"data_source_type": DataSourceTypeLog,
				"query":            "level:error",
				"group_by":         []string{"service"},
				"aggregation":      AggregationCount,
			},
		},
		{
			Type:           WidgetTypeDonut,
			Category:       CategoryAggregates,
			Description:    "Donut chart with center space for additional info",
			RequiredParams: []string{"data_source_type", "group_by"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "aggregation", "lookback", "show_legend", "coloring_mode", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeDonut,
				"title":            "Traffic by Region",
				"data_source_type": DataSourceTypeMetric,
				"metric_name":      "http.requests",
				"group_by":         []string{"region"},
				"aggregation":      AggregationSum,
			},
		},
		{
			Type:           WidgetTypeColumn,
			Category:       CategoryAggregates,
			Description:    "Horizontal bar chart for category comparison",
			RequiredParams: []string{"data_source_type", "group_by"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "aggregation", "lookback", "show_legend", "coloring_mode", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeColumn,
				"title":            "Top Services by Latency",
				"data_source_type": DataSourceTypeMetric,
				"metric_name":      "http.request.duration",
				"group_by":         []string{"service"},
				"aggregation":      AggregationP95,
			},
		},
		{
			Type:           WidgetTypeRadar,
			Category:       CategoryAggregates,
			Description:    "Radar/spider chart for multi-dimensional comparison",
			RequiredParams: []string{"data_source_type", "group_by"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "aggregation", "lookback", "show_legend", "coloring_mode", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeRadar,
				"title":            "Service Health Metrics",
				"data_source_type": DataSourceTypeMetric,
				"metric_name":      "service.health.score",
				"group_by":         []string{"metric_type"},
			},
		},
		{
			Type:           WidgetTypeSunburst,
			Category:       CategoryAggregates,
			Description:    "Hierarchical sunburst for nested category visualization",
			RequiredParams: []string{"data_source_type", "group_by"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "aggregation", "lookback", "show_legend", "coloring_mode", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeSunburst,
				"title":            "Logs by Service and Level",
				"data_source_type": DataSourceTypeLog,
				"group_by":         []string{"service", "level"},
				"aggregation":      AggregationCount,
			},
		},
		{
			Type:           WidgetTypeTreemap,
			Category:       CategoryAggregates,
			Description:    "Treemap for hierarchical data with area-based sizing",
			RequiredParams: []string{"data_source_type", "group_by"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "aggregation", "lookback", "show_legend", "coloring_mode", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeTreemap,
				"title":            "Storage by Namespace",
				"data_source_type": DataSourceTypeMetric,
				"metric_name":      "storage.bytes",
				"group_by":         []string{"namespace"},
				"aggregation":      AggregationSum,
			},
		},
		{
			Type:           WidgetTypeSankey,
			Category:       CategoryAggregates,
			Description:    "Sankey flow diagram for visualizing data flow",
			RequiredParams: []string{"data_source_type"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "aggregation", "group_by", "lookback", "show_legend", "coloring_mode", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeSankey,
				"title":            "Request Flow",
				"data_source_type": DataSourceTypeTrace,
				"query":            "service:api-gateway",
			},
		},
		{
			Type:           WidgetTypeBubble,
			Category:       CategoryAggregates,
			Description:    "Bubble chart with three dimensions (x, y, size)",
			RequiredParams: []string{"data_source_type"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "aggregation", "group_by", "lookback", "show_legend", "coloring_mode", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeBubble,
				"title":            "Services by Latency and Volume",
				"data_source_type": DataSourceTypeMetric,
				"metric_name":      "http.request.duration",
				"group_by":         []string{"service"},
			},
		},

		// Other widgets (7)
		{
			Type:           WidgetTypeTable,
			Category:       CategoryOther,
			Description:    "Formatted data table with sortable columns",
			RequiredParams: []string{"data_source_type"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "aggregation", "group_by", "lookback", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeTable,
				"title":            "Top Errors",
				"data_source_type": DataSourceTypeLog,
				"query":            "level:error",
				"lookback":         "1h",
			},
		},
		{
			Type:           WidgetTypeRawTable,
			Category:       CategoryOther,
			Description:    "Raw data table showing individual records",
			RequiredParams: []string{"data_source_type"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "lookback", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeRawTable,
				"title":            "Recent Logs",
				"data_source_type": DataSourceTypeLog,
				"lookback":         "15m",
			},
		},
		{
			Type:           WidgetTypeList,
			Category:       CategoryOther,
			Description:    "List view for displaying items vertically",
			RequiredParams: []string{"data_source_type"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "aggregation", "group_by", "lookback", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeList,
				"title":            "Active Alerts",
				"data_source_type": DataSourceTypeEvent,
				"query":            "severity:critical",
			},
		},
		{
			Type:           WidgetTypeGeomap,
			Category:       CategoryOther,
			Description:    "Geographic map for location-based data",
			RequiredParams: []string{"data_source_type"},
			OptionalParams: []string{"title", "description", "query", "metric_name", "aggregation", "group_by", "lookback", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeGeomap,
				"title":            "Requests by Location",
				"data_source_type": DataSourceTypeLog,
				"group_by":         []string{"geo.country"},
				"aggregation":      AggregationCount,
			},
		},
		{
			Type:           WidgetTypeJSON,
			Category:       CategoryOther,
			Description:    "Raw JSON display for debugging or detailed data inspection",
			RequiredParams: []string{"data_source_type"},
			OptionalParams: []string{"title", "description", "query", "lookback", "position_area"},
			Example: map[string]any{
				"widget_type":      WidgetTypeJSON,
				"title":            "Raw Event Data",
				"data_source_type": DataSourceTypeEvent,
				"query":            "event_type:deployment",
			},
		},
		{
			Type:           WidgetTypeEmpty,
			Category:       CategoryOther,
			Description:    "Placeholder widget for reserving dashboard space",
			RequiredParams: []string{},
			OptionalParams: []string{"title", "description", "position_area"},
			Example: map[string]any{
				"widget_type": WidgetTypeEmpty,
				"title":       "Coming Soon",
			},
		},
		{
			Type:           WidgetTypeMarkdown,
			Category:       CategoryOther,
			Description:    "Markdown text for documentation and notes",
			RequiredParams: []string{"content"},
			OptionalParams: []string{"title", "description", "position_area"},
			Example: map[string]any{
				"widget_type": WidgetTypeMarkdown,
				"title":       "Dashboard Guide",
				"content":     "## Overview\nThis dashboard shows API performance metrics.",
			},
		},

		// Layout widgets (3)
		{
			Type:           WidgetTypeGrid,
			Category:       CategoryLayout,
			Description:    "Grid container for organizing nested widgets",
			RequiredParams: []string{},
			OptionalParams: []string{"title", "description", "position_area"},
			Example: map[string]any{
				"widget_type": WidgetTypeGrid,
				"title":       "Metrics Section",
			},
		},
		{
			Type:           WidgetTypeTabs,
			Category:       CategoryLayout,
			Description:    "Tabbed container for grouping related widgets. Child widgets use target_tab_id and tab_index to position inside tabs.",
			RequiredParams: []string{"tab_labels"},
			OptionalParams: []string{"title", "description", "position_area"},
			Example: map[string]any{
				"widget_type": WidgetTypeTabs,
				"title":       "Service Details",
				"tab_labels":  []string{"Overview", "Details", "Logs"},
			},
		},
		{
			Type:           WidgetTypeVariableControl,
			Category:       CategoryLayout,
			Description:    "Variable selector for dynamic dashboard filtering. Requires a variable_id matching a variable defined in assemble_dashboard.",
			RequiredParams: []string{"variable_id"},
			OptionalParams: []string{"title", "description", "position_area"},
			Example: map[string]any{
				"widget_type": WidgetTypeVariableControl,
				"title":       "Fleet Selector",
				"variable_id": 1,
			},
		},
	}
}
