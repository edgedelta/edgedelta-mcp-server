package validation

import (
	"fmt"
	"slices"
)

// Valid widget types - mirrors dashboard_schema.go constants
var validWidgetTypes = []string{
	// Timeseries
	"line", "area", "bar", "scatter", "step", "smooth",
	// Scalar
	"bignumber", "gauge",
	// Aggregates
	"pie", "donut", "column", "radar", "sunburst", "treemap", "sankey", "bubble",
	// Other
	"table", "raw-table", "list", "geomap", "json", "empty", "markdown",
	// Layout
	"grid", "tabs", "variable-control",
	// Special
	"viz", // The actual v4 type for data widgets
}

// VersionRule ensures the dashboard version is correct.
type VersionRule struct{}

func (r *VersionRule) Name() string { return "version" }

func (r *VersionRule) Validate(ctx *DashboardContext) *ValidationResult {
	result := &ValidationResult{}
	if ctx.Version != DashboardVersion {
		result.AddError(
			"version",
			fmt.Sprintf("Dashboard version must be %d, got %d", DashboardVersion, ctx.Version),
			fmt.Sprintf("Set version to %d", DashboardVersion),
		)
	}
	return result
}

// RootWidgetRule ensures a root widget exists.
type RootWidgetRule struct{}

func (r *RootWidgetRule) Name() string { return "root_widget" }

func (r *RootWidgetRule) Validate(ctx *DashboardContext) *ValidationResult {
	result := &ValidationResult{}
	hasRoot := false
	for _, w := range ctx.Widgets {
		if id, ok := w["id"]; ok && id == "root" {
			hasRoot = true
			break
		}
	}
	if !hasRoot {
		result.AddError(
			"widgets",
			"Dashboard must have a root widget with id 'root'",
			"Add a widget with id: 'root'",
		)
	}
	return result
}

// UniqueIDsRule ensures all widget IDs are unique.
type UniqueIDsRule struct{}

func (r *UniqueIDsRule) Name() string { return "unique_ids" }

func (r *UniqueIDsRule) Validate(ctx *DashboardContext) *ValidationResult {
	result := &ValidationResult{}
	seen := make(map[interface{}]bool)

	for _, w := range ctx.Widgets {
		if id, ok := w["id"]; ok {
			if seen[id] {
				result.AddError(
					"widgets",
					fmt.Sprintf("Duplicate widget ID: %v", id),
					"Ensure all widget IDs are unique",
				)
			}
			seen[id] = true
		}
	}
	return result
}

// WidgetTypeRule validates that all widget types are known.
type WidgetTypeRule struct{}

func (r *WidgetTypeRule) Name() string { return "widget_type" }

func (r *WidgetTypeRule) Validate(ctx *DashboardContext) *ValidationResult {
	result := &ValidationResult{}

	types := validWidgetTypes
	if WidgetTypeProvider != nil {
		types = WidgetTypeProvider()
	}

	for _, w := range ctx.Widgets {
		widgetType, ok := w["type"].(string)
		if !ok {
			id := w["id"]
			result.AddError(
				fmt.Sprintf("widget[%v].type", id),
				"Widget type is required",
				"Specify a valid widget type",
			)
			continue
		}

		if !slices.Contains(types, widgetType) {
			id := w["id"]
			result.AddError(
				fmt.Sprintf("widget[%v].type", id),
				fmt.Sprintf("Unknown widget type: %s", widgetType),
				"Use a valid widget type: line, bar, bignumber, table, etc.",
			)
		}
	}
	return result
}
