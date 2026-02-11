package tools

import (
	"testing"
)

func TestBuildFilterFromVariables(t *testing.T) {
	tests := []struct {
		name      string
		variables []map[string]interface{}
		dsType    string
		want      string
	}{
		{
			name:      "empty variables returns wildcard",
			variables: nil,
			dsType:    "metric",
			want:      "*",
		},
		{
			name: "single metric-scoped variable",
			variables: []map[string]interface{}{
				{"facet": "host.name", "key": "hostname", "scope": "metric"},
			},
			dsType: "metric",
			want:   "host.name:$hostname",
		},
		{
			name: "multiple variables same scope",
			variables: []map[string]interface{}{
				{"facet": "host.name", "key": "hostname", "scope": "metric"},
				{"facet": "ed.tag", "key": "tag", "scope": "metric"},
			},
			dsType: "metric",
			want:   "host.name:$hostname ed.tag:$tag",
		},
		{
			name: "scope filtering excludes non-matching",
			variables: []map[string]interface{}{
				{"facet": "host.name", "key": "hostname", "scope": "metric"},
				{"facet": "service.name", "key": "service", "scope": "log"},
			},
			dsType: "metric",
			want:   "host.name:$hostname",
		},
		{
			name: "empty scope matches all data source types",
			variables: []map[string]interface{}{
				{"facet": "ed.tag", "key": "tag", "scope": ""},
			},
			dsType: "metric",
			want:   "ed.tag:$tag",
		},
		{
			name: "variable missing facet is skipped",
			variables: []map[string]interface{}{
				{"key": "hostname", "scope": "metric"},
			},
			dsType: "metric",
			want:   "*",
		},
		{
			name: "variable missing key is skipped",
			variables: []map[string]interface{}{
				{"facet": "host.name", "scope": "metric"},
			},
			dsType: "metric",
			want:   "*",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildFilterFromVariables(tt.variables, tt.dsType)
			if got != tt.want {
				t.Errorf("buildFilterFromVariables() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildVisuals(t *testing.T) {
	tests := []struct {
		name      string
		widget    WidgetConfig
		variables []map[string]interface{}
		wantQuery string
	}{
		{
			name: "metric with group_by and variables",
			widget: WidgetConfig{
				DataSourceType: "metric",
				MetricName:     "ed.host.net.read_bytes",
				Aggregation:    "sum",
				GroupBy:        []string{"host.name"},
			},
			variables: []map[string]interface{}{
				{"facet": "host.name", "key": "hostname", "scope": "metric"},
				{"facet": "ed.tag", "key": "tag", "scope": "metric"},
			},
			wantQuery: "sum:ed.host.net.read_bytes{host.name:$hostname ed.tag:$tag} by {host.name}.rollup(60)",
		},
		{
			name: "metric with group_by no variables",
			widget: WidgetConfig{
				DataSourceType: "metric",
				MetricName:     "cpu",
				Aggregation:    "avg",
				GroupBy:        []string{"host.name"},
			},
			variables: nil,
			wantQuery: "avg:cpu{*} by {host.name}.rollup(60)",
		},
		{
			name: "metric no group_by no variables",
			widget: WidgetConfig{
				DataSourceType: "metric",
				MetricName:     "requests",
			},
			variables: nil,
			wantQuery: "sum:requests{*}.rollup(60)",
		},
		{
			name: "metric default aggregation is sum",
			widget: WidgetConfig{
				DataSourceType: "metric",
				MetricName:     "bytes",
			},
			variables: nil,
			wantQuery: "sum:bytes{*}.rollup(60)",
		},
		{
			name: "log with group_by",
			widget: WidgetConfig{
				DataSourceType: "log",
				Query:          "error",
				GroupBy:        []string{"service"},
			},
			variables: nil,
			wantQuery: "error by {service}",
		},
		{
			name: "raw query passthrough unchanged",
			widget: WidgetConfig{
				DataSourceType: "metric",
				Query:          "avg:custom{host:foo}.rollup(120)",
			},
			variables: nil,
			wantQuery: "avg:custom{host:foo}.rollup(120)",
		},
		{
			name: "variable scope filtering in metric query",
			widget: WidgetConfig{
				DataSourceType: "metric",
				MetricName:     "net.bytes",
				Aggregation:    "sum",
			},
			variables: []map[string]interface{}{
				{"facet": "host.name", "key": "hostname", "scope": "metric"},
				{"facet": "service.name", "key": "service", "scope": "log"},
			},
			wantQuery: "sum:net.bytes{host.name:$hostname}.rollup(60)",
		},
		{
			name: "empty data source returns empty visuals",
			widget: WidgetConfig{
				DataSourceType: "",
				Query:          "",
				MetricName:     "",
			},
			variables: nil,
			wantQuery: "", // empty visuals, no query
		},
		{
			name: "metric with explicit query uses passthrough",
			widget: WidgetConfig{
				DataSourceType: "metric",
				MetricName:     "cpu",
				Query:          "max:cpu{env:prod}.rollup(300)",
			},
			variables: nil,
			wantQuery: "max:cpu{env:prod}.rollup(300)",
		},
		{
			name: "empty braces replaced with wildcard in passthrough",
			widget: WidgetConfig{
				DataSourceType: "log",
				Query:          "error{}",
			},
			variables: nil,
			wantQuery: "error{*}",
		},
		{
			name: "multiple group_by fields for metric",
			widget: WidgetConfig{
				DataSourceType: "metric",
				MetricName:     "net.bytes",
				Aggregation:    "sum",
				GroupBy:        []string{"host.name", "region"},
			},
			variables: nil,
			wantQuery: "sum:net.bytes{*} by {host.name, region}.rollup(60)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			visuals := buildVisuals(tt.widget, tt.variables)

			if tt.wantQuery == "" {
				if len(visuals) != 0 {
					t.Errorf("expected empty visuals, got %d", len(visuals))
				}
				return
			}

			if len(visuals) != 1 {
				t.Fatalf("expected 1 visual, got %d", len(visuals))
			}

			ds, ok := visuals[0]["dataSource"].(map[string]interface{})
			if !ok {
				t.Fatal("missing dataSource in visual")
			}
			params, ok := ds["params"].(map[string]interface{})
			if !ok {
				t.Fatal("missing params in dataSource")
			}
			gotQuery, _ := params["query"].(string)
			if gotQuery != tt.wantQuery {
				t.Errorf("query = %q, want %q", gotQuery, tt.wantQuery)
			}
		})
	}
}

// helper to extract position.area from a v4 widget map
func getWidgetArea(w map[string]interface{}) (col, colSpan, row, rowSpan int) {
	pos, _ := w["position"].(map[string]interface{})
	area, _ := pos["area"].(map[string]interface{})
	col, _ = area["column"].(int)
	colSpan, _ = area["columnSpan"].(int)
	row, _ = area["row"].(int)
	rowSpan, _ = area["rowSpan"].(int)
	return
}

func TestBuildV4Widgets_CustomPositions(t *testing.T) {
	tests := []struct {
		name    string
		widgets []WidgetConfig
		checks  func(t *testing.T, result []map[string]interface{})
	}{
		{
			name: "custom positions passed through exactly",
			widgets: []WidgetConfig{
				{Type: "line", Column: 1, ColumnSpan: 6, Row: 2, RowSpan: 3, DataSourceType: "metric", MetricName: "cpu"},
				{Type: "area", Column: 7, ColumnSpan: 6, Row: 2, RowSpan: 3, DataSourceType: "metric", MetricName: "mem"},
			},
			checks: func(t *testing.T, result []map[string]interface{}) {
				// result[0] is root grid, result[1] and [2] are widgets
				col, colSpan, row, rowSpan := getWidgetArea(result[1])
				if col != 1 || colSpan != 6 || row != 2 || rowSpan != 3 {
					t.Errorf("widget 1 position = (%d,%d,%d,%d), want (1,6,2,3)", col, colSpan, row, rowSpan)
				}
				col, colSpan, row, rowSpan = getWidgetArea(result[2])
				if col != 7 || colSpan != 6 || row != 2 || rowSpan != 3 {
					t.Errorf("widget 2 position = (%d,%d,%d,%d), want (7,6,2,3)", col, colSpan, row, rowSpan)
				}
			},
		},
		{
			name: "full-width widget with columnSpan 12",
			widgets: []WidgetConfig{
				{Type: "bar", Column: 1, ColumnSpan: 12, Row: 1, RowSpan: 4, DataSourceType: "metric", MetricName: "net"},
			},
			checks: func(t *testing.T, result []map[string]interface{}) {
				col, colSpan, _, _ := getWidgetArea(result[1])
				if col != 1 || colSpan != 12 {
					t.Errorf("full-width position = col %d, span %d, want col 1, span 12", col, colSpan)
				}
			},
		},
		{
			name: "auto-layout fallback when no position specified",
			widgets: []WidgetConfig{
				{Type: "line", DataSourceType: "metric", MetricName: "a"},
				{Type: "line", DataSourceType: "metric", MetricName: "b"},
				{Type: "line", DataSourceType: "metric", MetricName: "c"},
			},
			checks: func(t *testing.T, result []map[string]interface{}) {
				// Auto: 2 per row, 6 cols each
				col1, _, row1, _ := getWidgetArea(result[1])
				col2, _, row2, _ := getWidgetArea(result[2])
				col3, _, row3, _ := getWidgetArea(result[3])
				if col1 != 1 || row1 != 1 {
					t.Errorf("auto widget 1 = (%d,%d), want (1,1)", col1, row1)
				}
				if col2 != 7 || row2 != 1 {
					t.Errorf("auto widget 2 = (%d,%d), want (7,1)", col2, row2)
				}
				if col3 != 1 || row3 != 5 {
					t.Errorf("auto widget 3 = (%d,%d), want (1,5)", col3, row3)
				}
			},
		},
		{
			name: "mixed custom and auto positions",
			widgets: []WidgetConfig{
				{Type: "markdown", Column: 1, ColumnSpan: 1, Row: 1, RowSpan: 1, Content: "hi"},
				{Type: "variable-control", Column: 2, ColumnSpan: 5, Row: 1, RowSpan: 1, VariableID: 1},
				{Type: "line", DataSourceType: "metric", MetricName: "cpu"}, // auto-layout
			},
			checks: func(t *testing.T, result []map[string]interface{}) {
				// Custom positions
				col1, cs1, row1, rs1 := getWidgetArea(result[1])
				if col1 != 1 || cs1 != 1 || row1 != 1 || rs1 != 1 {
					t.Errorf("markdown = (%d,%d,%d,%d), want (1,1,1,1)", col1, cs1, row1, rs1)
				}
				col2, cs2, row2, rs2 := getWidgetArea(result[2])
				if col2 != 2 || cs2 != 5 || row2 != 1 || rs2 != 1 {
					t.Errorf("var-ctrl = (%d,%d,%d,%d), want (2,5,1,1)", col2, cs2, row2, rs2)
				}
				// Auto-layout widget gets first auto slot
				col3, _, row3, _ := getWidgetArea(result[3])
				if col3 != 1 || row3 != 1 {
					t.Errorf("auto widget = (%d,%d), want (1,1)", col3, row3)
				}
			},
		},
		{
			name: "root grid rows calculated from max widget extent",
			widgets: []WidgetConfig{
				{Type: "line", Column: 1, ColumnSpan: 12, Row: 20, RowSpan: 5, DataSourceType: "metric", MetricName: "x"},
			},
			checks: func(t *testing.T, result []map[string]interface{}) {
				// Root grid should have 24 rows (20 + 5 - 1)
				grid, _ := result[0]["grid"].(string)
				// Count "72px" occurrences
				count := 0
				for i := 0; i+3 < len(grid); i++ {
					if grid[i:i+4] == "72px" {
						count++
					}
				}
				if count != 24 {
					t.Errorf("grid has %d rows, want 24 (from row 20 + rowSpan 5 - 1)", count)
				}
			},
		},
		{
			name: "defaults applied when only row and column set",
			widgets: []WidgetConfig{
				{Type: "line", Column: 1, Row: 1, DataSourceType: "metric", MetricName: "cpu"},
			},
			checks: func(t *testing.T, result []map[string]interface{}) {
				col, colSpan, row, rowSpan := getWidgetArea(result[1])
				if col != 1 || colSpan != 6 || row != 1 || rowSpan != 4 {
					t.Errorf("position = (%d,%d,%d,%d), want (1,6,1,4)", col, colSpan, row, rowSpan)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildV4Widgets(tt.widgets, nil)
			tt.checks(t, result)
		})
	}
}
