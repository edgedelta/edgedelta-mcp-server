package validation

import "fmt"

// GridOverlapRule detects overlapping widgets within the same grid container.
type GridOverlapRule struct{}

func (r *GridOverlapRule) Name() string { return "grid_overlap" }

func (r *GridOverlapRule) Validate(ctx *DashboardContext) *ValidationResult {
	result := &ValidationResult{}

	// Group widgets by their parent (targetId)
	widgetsByParent := make(map[interface{}][]map[string]interface{})

	for _, w := range ctx.Widgets {
		pos, ok := w["position"].(map[string]interface{})
		if !ok {
			continue
		}

		targetID := pos["targetId"]
		if targetID == nil {
			targetID = "root"
		}

		widgetsByParent[targetID] = append(widgetsByParent[targetID], w)
	}

	// Check for overlaps within each parent
	for parentID, widgets := range widgetsByParent {
		areas := extractGridAreas(widgets)
		overlaps := findOverlaps(areas)

		for _, overlap := range overlaps {
			result.AddError(
				fmt.Sprintf("widgets[parent=%v]", parentID),
				fmt.Sprintf("Grid overlap detected between widgets %v and %v at row %d, col %d",
					overlap.widget1, overlap.widget2, overlap.row, overlap.col),
				"Adjust widget positions to avoid overlapping grid areas",
			)
		}
	}

	return result
}

// overlap represents a collision between two widgets.
type overlap struct {
	widget1 interface{}
	widget2 interface{}
	row     int
	col     int
}

// extractGridAreas pulls grid position info from widgets.
// Handles both flat position (row, column) and nested area object (position.area.row, etc.)
func extractGridAreas(widgets []map[string]interface{}) []GridArea {
	var areas []GridArea

	for _, w := range widgets {
		pos, ok := w["position"].(map[string]interface{})
		if !ok {
			continue
		}

		area := GridArea{
			WidgetID:   w["id"],
			Row:        1,
			Column:     1,
			RowSpan:    1,
			ColumnSpan: 1,
		}

		// Check for nested "area" object first (v4 format from buildV4Widgets)
		if areaObj, ok := pos["area"].(map[string]interface{}); ok {
			if row, ok := areaObj["row"].(int); ok {
				area.Row = row
			} else if row, ok := areaObj["row"].(float64); ok {
				area.Row = int(row)
			}
			if col, ok := areaObj["column"].(int); ok {
				area.Column = col
			} else if col, ok := areaObj["column"].(float64); ok {
				area.Column = int(col)
			}
			if rs, ok := areaObj["rowSpan"].(int); ok {
				area.RowSpan = rs
			} else if rs, ok := areaObj["rowSpan"].(float64); ok {
				area.RowSpan = int(rs)
			}
			if cs, ok := areaObj["columnSpan"].(int); ok {
				area.ColumnSpan = cs
			} else if cs, ok := areaObj["columnSpan"].(float64); ok {
				area.ColumnSpan = int(cs)
			}
		} else {
			// Fall back to flat position (from JSON parsing)
			if row, ok := pos["row"].(float64); ok {
				area.Row = int(row)
			}
			if col, ok := pos["column"].(float64); ok {
				area.Column = int(col)
			}
			if rs, ok := pos["rowSpan"].(float64); ok {
				area.RowSpan = int(rs)
			}
			if cs, ok := pos["columnSpan"].(float64); ok {
				area.ColumnSpan = int(cs)
			}
		}

		// Skip widgets with invalid positions
		if area.Row < 1 || area.Column < 1 || area.RowSpan < 1 || area.ColumnSpan < 1 {
			continue
		}

		areas = append(areas, area)
	}

	return areas
}

// findOverlaps detects all overlapping grid areas using rectangle intersection.
func findOverlaps(areas []GridArea) []overlap {
	var overlaps []overlap

	for i := 0; i < len(areas); i++ {
		for j := i + 1; j < len(areas); j++ {
			a, b := areas[i], areas[j]
			if areasCollide(a, b) {
				// Find collision point (first overlapping cell)
				row := max(a.Row, b.Row)
				col := max(a.Column, b.Column)
				overlaps = append(overlaps, overlap{
					widget1: a.WidgetID,
					widget2: b.WidgetID,
					row:     row,
					col:     col,
				})
			}
		}
	}

	return overlaps
}

// areasCollide checks if two grid areas overlap using rectangle intersection.
// Ported from EdgeDelta web UI: doesAreasCollide() in grid/utils.ts
func areasCollide(a, b GridArea) bool {
	// Two rectangles collide if they overlap on both axes
	// Rectangle A: rows [a.Row, a.Row + a.RowSpan), cols [a.Column, a.Column + a.ColumnSpan)
	// Rectangle B: rows [b.Row, b.Row + b.RowSpan), cols [b.Column, b.Column + b.ColumnSpan)
	return b.Row+b.RowSpan > a.Row &&
		a.Row+a.RowSpan > b.Row &&
		b.Column+b.ColumnSpan > a.Column &&
		a.Column+a.ColumnSpan > b.Column
}
