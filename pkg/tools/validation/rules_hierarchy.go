package validation

import "fmt"

// HierarchyCycleRule detects cycles in widget parent-child relationships.
type HierarchyCycleRule struct{}

func (r *HierarchyCycleRule) Name() string { return "hierarchy_cycle" }

func (r *HierarchyCycleRule) Validate(ctx *DashboardContext) *ValidationResult {
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
