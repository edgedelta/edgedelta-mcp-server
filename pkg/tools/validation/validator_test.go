package validation

import (
	"strings"
	"testing"
)

func TestVersionRule(t *testing.T) {
	tests := []struct {
		name      string
		version   int
		wantError bool
	}{
		{"valid version 4", 4, false},
		{"invalid version 3", 3, true},
		{"invalid version 0", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &DashboardContext{Version: tt.version}
			rule := &VersionRule{}
			result := rule.Validate(ctx)

			if tt.wantError && result.IsValid() {
				t.Errorf("expected error for version %d", tt.version)
			}
			if !tt.wantError && !result.IsValid() {
				t.Errorf("unexpected error for version %d: %v", tt.version, result.Errors)
			}
		})
	}
}

func TestRootWidgetRule(t *testing.T) {
	tests := []struct {
		name      string
		widgets   []map[string]interface{}
		wantError bool
	}{
		{
			name: "has root widget",
			widgets: []map[string]interface{}{
				{"id": "root", "type": "grid"},
			},
			wantError: false,
		},
		{
			name: "missing root widget",
			widgets: []map[string]interface{}{
				{"id": 1, "type": "viz"},
			},
			wantError: true,
		},
		{
			name:      "empty widgets",
			widgets:   []map[string]interface{}{},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &DashboardContext{Widgets: tt.widgets}
			rule := &RootWidgetRule{}
			result := rule.Validate(ctx)

			if tt.wantError && result.IsValid() {
				t.Error("expected error for missing root widget")
			}
			if !tt.wantError && !result.IsValid() {
				t.Errorf("unexpected error: %v", result.Errors)
			}
		})
	}
}

func TestUniqueIDsRule(t *testing.T) {
	tests := []struct {
		name      string
		widgets   []map[string]interface{}
		wantError bool
	}{
		{
			name: "unique IDs",
			widgets: []map[string]interface{}{
				{"id": "root"},
				{"id": 1},
				{"id": 2},
			},
			wantError: false,
		},
		{
			name: "duplicate IDs",
			widgets: []map[string]interface{}{
				{"id": 1},
				{"id": 1},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &DashboardContext{Widgets: tt.widgets}
			rule := &UniqueIDsRule{}
			result := rule.Validate(ctx)

			if tt.wantError && result.IsValid() {
				t.Error("expected error for duplicate IDs")
			}
			if !tt.wantError && !result.IsValid() {
				t.Errorf("unexpected error: %v", result.Errors)
			}
		})
	}
}

func TestWidgetTypeRule(t *testing.T) {
	tests := []struct {
		name      string
		widgets   []map[string]interface{}
		wantError bool
	}{
		{
			name: "valid widget types",
			widgets: []map[string]interface{}{
				{"id": "root", "type": "grid"},
				{"id": 1, "type": "viz"},
				{"id": 2, "type": "line"},
			},
			wantError: false,
		},
		{
			name: "unknown widget type",
			widgets: []map[string]interface{}{
				{"id": 1, "type": "unknown-widget"},
			},
			wantError: true,
		},
		{
			name: "missing widget type",
			widgets: []map[string]interface{}{
				{"id": 1},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &DashboardContext{Widgets: tt.widgets}
			rule := &WidgetTypeRule{}
			result := rule.Validate(ctx)

			if tt.wantError && result.IsValid() {
				t.Error("expected error for invalid widget type")
			}
			if !tt.wantError && !result.IsValid() {
				t.Errorf("unexpected error: %v", result.Errors)
			}
		})
	}
}

func TestGridOverlapRule(t *testing.T) {
	tests := []struct {
		name      string
		widgets   []map[string]interface{}
		wantError bool
	}{
		{
			name: "no overlap",
			widgets: []map[string]interface{}{
				{
					"id":   1,
					"type": "viz",
					"position": map[string]interface{}{
						"targetId":   "root",
						"row":        float64(1),
						"column":     float64(1),
						"rowSpan":    float64(2),
						"columnSpan": float64(2),
					},
				},
				{
					"id":   2,
					"type": "viz",
					"position": map[string]interface{}{
						"targetId":   "root",
						"row":        float64(1),
						"column":     float64(3),
						"rowSpan":    float64(2),
						"columnSpan": float64(2),
					},
				},
			},
			wantError: false,
		},
		{
			name: "widgets overlap",
			widgets: []map[string]interface{}{
				{
					"id":   1,
					"type": "viz",
					"position": map[string]interface{}{
						"targetId":   "root",
						"row":        float64(1),
						"column":     float64(1),
						"rowSpan":    float64(2),
						"columnSpan": float64(2),
					},
				},
				{
					"id":   2,
					"type": "viz",
					"position": map[string]interface{}{
						"targetId":   "root",
						"row":        float64(2),
						"column":     float64(2),
						"rowSpan":    float64(2),
						"columnSpan": float64(2),
					},
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &DashboardContext{Widgets: tt.widgets}
			rule := &GridOverlapRule{}
			result := rule.Validate(ctx)

			if tt.wantError && result.IsValid() {
				t.Error("expected error for overlapping widgets")
			}
			if !tt.wantError && !result.IsValid() {
				t.Errorf("unexpected error: %v", result.Errors)
			}
		})
	}
}

func TestHierarchyCycleRule(t *testing.T) {
	tests := []struct {
		name      string
		widgets   []map[string]interface{}
		wantError bool
	}{
		{
			name: "no cycle",
			widgets: []map[string]interface{}{
				{"id": "root", "type": "grid"},
				{
					"id":   1,
					"type": "viz",
					"position": map[string]interface{}{
						"targetId": "root",
					},
				},
				{
					"id":   2,
					"type": "viz",
					"position": map[string]interface{}{
						"targetId": float64(1),
					},
				},
			},
			wantError: false,
		},
		{
			name: "direct cycle",
			widgets: []map[string]interface{}{
				{
					"id":   1,
					"type": "grid",
					"position": map[string]interface{}{
						"targetId": float64(2),
					},
				},
				{
					"id":   2,
					"type": "grid",
					"position": map[string]interface{}{
						"targetId": float64(1),
					},
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &DashboardContext{Widgets: tt.widgets}
			rule := &HierarchyCycleRule{}
			result := rule.Validate(ctx)

			if tt.wantError && result.IsValid() {
				t.Error("expected error for hierarchy cycle")
			}
			if !tt.wantError && !result.IsValid() {
				t.Errorf("unexpected error: %v", result.Errors)
			}
		})
	}
}

func TestVariableReferenceRule(t *testing.T) {
	tests := []struct {
		name      string
		widgets   []map[string]interface{}
		variables []map[string]interface{}
		wantError bool
	}{
		{
			name: "valid variable reference",
			widgets: []map[string]interface{}{
				{
					"id":         1,
					"type":       "variable-control",
					"variableId": float64(1),
				},
			},
			variables: []map[string]interface{}{
				{"id": float64(1), "key": "fleet"},
			},
			wantError: false,
		},
		{
			name: "missing variable reference",
			widgets: []map[string]interface{}{
				{
					"id":   1,
					"type": "variable-control",
				},
			},
			variables: []map[string]interface{}{},
			wantError: true,
		},
		{
			name: "invalid variable reference",
			widgets: []map[string]interface{}{
				{
					"id":         1,
					"type":       "variable-control",
					"variableId": float64(99),
				},
			},
			variables: []map[string]interface{}{
				{"id": float64(1), "key": "fleet"},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &DashboardContext{
				Widgets:   tt.widgets,
				Variables: tt.variables,
			}
			rule := &VariableReferenceRule{}
			result := rule.Validate(ctx)

			if tt.wantError && result.IsValid() {
				t.Error("expected error for invalid variable reference")
			}
			if !tt.wantError && !result.IsValid() {
				t.Errorf("unexpected error: %v", result.Errors)
			}
		})
	}
}

func TestValidateDashboard(t *testing.T) {
	validDashboard := map[string]interface{}{
		"version": float64(4),
		"widgets": []interface{}{
			map[string]interface{}{
				"id":   "root",
				"type": "grid",
			},
			map[string]interface{}{
				"id":   float64(1),
				"type": "viz",
				"position": map[string]interface{}{
					"targetId":   "root",
					"row":        float64(1),
					"column":     float64(1),
					"rowSpan":    float64(2),
					"columnSpan": float64(6),
				},
			},
		},
	}

	result := ValidateDashboard(validDashboard)
	if !result.IsValid() {
		t.Errorf("expected valid dashboard, got errors: %v", result.Errors)
	}
}

func TestMarkdownContentRule(t *testing.T) {
	tests := []struct {
		name      string
		widgets   []map[string]interface{}
		wantError bool
		errorMsg  string
	}{
		{
			name: "valid markdown - inline styles only",
			widgets: []map[string]interface{}{
				{
					"id":   1,
					"type": "markdown",
					"params": map[string]interface{}{
						"content": `<div style="color: red;">Hello</div>`,
					},
				},
			},
			wantError: false,
		},
		{
			name: "forbidden style tag",
			widgets: []map[string]interface{}{
				{
					"id":   1,
					"type": "markdown",
					"params": map[string]interface{}{
						"content": `<style>.foo { color: red; }</style><div>Hello</div>`,
					},
				},
			},
			wantError: true,
			errorMsg:  "style",
		},
		{
			name: "forbidden script tag",
			widgets: []map[string]interface{}{
				{
					"id":   1,
					"type": "markdown",
					"params": map[string]interface{}{
						"content": `<script>alert('xss')</script>`,
					},
				},
			},
			wantError: true,
			errorMsg:  "script",
		},
		{
			name: "forbidden event handler",
			widgets: []map[string]interface{}{
				{
					"id":   1,
					"type": "markdown",
					"params": map[string]interface{}{
						"content": `<div onclick="alert('xss')">Click me</div>`,
					},
				},
			},
			wantError: true,
			errorMsg:  "onclick",
		},
		{
			name: "forbidden javascript protocol",
			widgets: []map[string]interface{}{
				{
					"id":   1,
					"type": "markdown",
					"params": map[string]interface{}{
						"content": `<a href="javascript:alert('xss')">Click</a>`,
					},
				},
			},
			wantError: true,
			errorMsg:  "JavaScript protocol",
		},
		{
			name: "valid details/summary",
			widgets: []map[string]interface{}{
				{
					"id":   1,
					"type": "markdown",
					"params": map[string]interface{}{
						"content": `<details><summary>Click to expand</summary><p>Content</p></details>`,
					},
				},
			},
			wantError: false,
		},
		{
			name: "non-markdown widget ignored",
			widgets: []map[string]interface{}{
				{
					"id":   1,
					"type": "viz",
					"params": map[string]interface{}{
						"content": `<script>alert('xss')</script>`,
					},
				},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &DashboardContext{Widgets: tt.widgets}
			rule := &MarkdownContentRule{}
			result := rule.Validate(ctx)

			if tt.wantError && result.IsValid() {
				t.Errorf("expected error containing '%s'", tt.errorMsg)
			}
			if !tt.wantError && !result.IsValid() {
				t.Errorf("unexpected error: %v", result.Errors)
			}
			if tt.wantError && !result.IsValid() {
				// Check error message contains expected text
				found := false
				for _, e := range result.Errors {
					if strings.Contains(e.Message, tt.errorMsg) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("error message should contain '%s', got: %v", tt.errorMsg, result.Errors)
				}
			}
		})
	}
}

func TestAreasCollide(t *testing.T) {
	tests := []struct {
		name string
		a, b GridArea
		want bool
	}{
		{
			name: "no overlap - side by side",
			a:    GridArea{Row: 1, Column: 1, RowSpan: 2, ColumnSpan: 2},
			b:    GridArea{Row: 1, Column: 3, RowSpan: 2, ColumnSpan: 2},
			want: false,
		},
		{
			name: "no overlap - stacked",
			a:    GridArea{Row: 1, Column: 1, RowSpan: 2, ColumnSpan: 2},
			b:    GridArea{Row: 3, Column: 1, RowSpan: 2, ColumnSpan: 2},
			want: false,
		},
		{
			name: "overlap - partial",
			a:    GridArea{Row: 1, Column: 1, RowSpan: 2, ColumnSpan: 2},
			b:    GridArea{Row: 2, Column: 2, RowSpan: 2, ColumnSpan: 2},
			want: true,
		},
		{
			name: "overlap - same position",
			a:    GridArea{Row: 1, Column: 1, RowSpan: 2, ColumnSpan: 2},
			b:    GridArea{Row: 1, Column: 1, RowSpan: 2, ColumnSpan: 2},
			want: true,
		},
		{
			name: "overlap - contained",
			a:    GridArea{Row: 1, Column: 1, RowSpan: 4, ColumnSpan: 4},
			b:    GridArea{Row: 2, Column: 2, RowSpan: 1, ColumnSpan: 1},
			want: true,
		},
		{
			name: "no overlap - adjacent edge",
			a:    GridArea{Row: 1, Column: 1, RowSpan: 1, ColumnSpan: 1},
			b:    GridArea{Row: 2, Column: 1, RowSpan: 1, ColumnSpan: 1},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := areasCollide(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("areasCollide(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestTargetIDReferenceRule(t *testing.T) {
	tests := []struct {
		name      string
		widgets   []map[string]interface{}
		wantError bool
	}{
		{
			name: "valid targetId reference",
			widgets: []map[string]interface{}{
				{"id": "root", "type": "grid"},
				{
					"id":   1,
					"type": "viz",
					"position": map[string]interface{}{
						"targetId": "root",
					},
				},
			},
			wantError: false,
		},
		{
			name: "invalid targetId reference",
			widgets: []map[string]interface{}{
				{"id": "root", "type": "grid"},
				{
					"id":   1,
					"type": "viz",
					"position": map[string]interface{}{
						"targetId": "nonexistent",
					},
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &DashboardContext{Widgets: tt.widgets}
			rule := &TargetIDReferenceRule{}
			result := rule.Validate(ctx)

			if tt.wantError && result.IsValid() {
				t.Error("expected error for invalid targetId reference")
			}
			if !tt.wantError && !result.IsValid() {
				t.Errorf("unexpected error: %v", result.Errors)
			}
		})
	}
}
