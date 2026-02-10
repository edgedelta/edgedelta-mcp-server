package validation

import "fmt"

// HierarchyCycleRule detects cycles in widget parent-child relationships.
type HierarchyCycleRule struct{}

func (r *HierarchyCycleRule) Name() string { return "hierarchy_cycle" }

func (r *HierarchyCycleRule) Validate(ctx *ValidationContext) *ValidationResult {
	result := &ValidationResult{}

	// Build parent map: normalized widget ID -> normalized parent ID (targetId)
	// Also build a map of all widget IDs for lookup
	parentMap := make(map[string]string)
	widgetIDs := make(map[string]bool)

	for _, w := range ctx.Widgets {
		id := normalizeID(w["id"])
		widgetIDs[id] = true
		if pos, ok := w["position"].(map[string]interface{}); ok {
			if targetID, ok := pos["targetId"]; ok {
				parentMap[id] = normalizeID(targetID)
			}
		}
	}

	// Check each widget for cycles using DFS
	// Ported from EdgeDelta web UI: isWidgetDescendant() in widget/utils.ts
	for _, w := range ctx.Widgets {
		startID := normalizeID(w["id"])
		if hasCycle(startID, parentMap) {
			result.AddError(
				fmt.Sprintf("widget[%v]", w["id"]),
				"Widget hierarchy contains a cycle",
				"Ensure no circular parent-child relationships exist",
			)
		}
	}

	return result
}

// normalizeID converts any ID type to a string for consistent comparison.
func normalizeID(id interface{}) string {
	switch v := id.(type) {
	case string:
		return v
	case float64:
		return fmt.Sprintf("%v", int(v))
	case int:
		return fmt.Sprintf("%d", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// hasCycle checks if following the parent chain from startID creates a cycle.
func hasCycle(startID string, parentMap map[string]string) bool {
	visited := make(map[string]bool)
	current := startID

	for {
		if visited[current] {
			return true // Found a cycle
		}
		visited[current] = true

		parent, hasParent := parentMap[current]
		if !hasParent {
			return false // Reached root, no cycle
		}
		current = parent
	}
}

// MaxDepthRule ensures widget nesting doesn't exceed a maximum depth.
type MaxDepthRule struct {
	MaxDepth int
}

func (r *MaxDepthRule) Name() string { return "max_depth" }

func (r *MaxDepthRule) Validate(ctx *ValidationContext) *ValidationResult {
	result := &ValidationResult{}

	if r.MaxDepth == 0 {
		r.MaxDepth = 10 // Default max depth
	}

	// Build parent map with normalized IDs
	parentMap := make(map[string]string)
	for _, w := range ctx.Widgets {
		id := normalizeID(w["id"])
		if pos, ok := w["position"].(map[string]interface{}); ok {
			if targetID, ok := pos["targetId"]; ok {
				parentMap[id] = normalizeID(targetID)
			}
		}
	}

	// Calculate depth for each widget
	for _, w := range ctx.Widgets {
		id := normalizeID(w["id"])
		depth := calculateDepth(id, parentMap)
		if depth > r.MaxDepth {
			result.AddError(
				fmt.Sprintf("widget[%v]", w["id"]),
				fmt.Sprintf("Widget nesting depth (%d) exceeds maximum (%d)", depth, r.MaxDepth),
				"Reduce nesting by restructuring the dashboard layout",
			)
		}
	}

	return result
}

// calculateDepth returns the depth of a widget in the hierarchy.
func calculateDepth(id string, parentMap map[string]string) int {
	depth := 0
	current := id
	visited := make(map[string]bool)

	for {
		if visited[current] {
			return depth // Cycle detected, stop counting
		}
		visited[current] = true

		parent, hasParent := parentMap[current]
		if !hasParent {
			return depth
		}
		depth++
		current = parent
	}
}
