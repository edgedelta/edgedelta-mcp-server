package validation

import (
	"encoding/json"
	"fmt"
)

// DashboardVersion is the required dashboard schema version.
const DashboardVersion = 4

// Validator orchestrates validation rules and collects all errors.
type Validator struct {
	rules []Rule
}

// NewValidator creates a validator with all default rules.
func NewValidator() *Validator {
	return &Validator{
		rules: []Rule{
			&VersionRule{},
			&RootWidgetRule{},
			&UniqueIDsRule{},
			&WidgetTypeRule{},
			&VariableReferenceRule{},
			&GridOverlapRule{},
			&HierarchyCycleRule{},
			&MarkdownContentRule{},
		},
	}
}

// NewValidatorWithRules creates a validator with custom rules.
func NewValidatorWithRules(rules ...Rule) *Validator {
	return &Validator{rules: rules}
}

// Validate runs all rules against the dashboard definition and collects all errors.
func (v *Validator) Validate(definition map[string]interface{}) *ValidationResult {
	result := &ValidationResult{}

	// Parse the definition into a context
	ctx, err := parseContext(definition)
	if err != nil {
		result.AddError("dashboard_definition", err.Error(), "Provide a valid dashboard definition object")
		return result
	}

	// Run all rules and collect errors
	for _, rule := range v.rules {
		ruleResult := rule.Validate(ctx)
		result.Merge(ruleResult)
	}

	return result
}

// ValidateJSON validates a dashboard definition from JSON bytes.
func (v *Validator) ValidateJSON(data []byte) *ValidationResult {
	var definition map[string]interface{}
	if err := json.Unmarshal(data, &definition); err != nil {
		result := &ValidationResult{}
		result.AddError("dashboard_definition", fmt.Sprintf("Invalid JSON: %v", err), "Provide valid JSON")
		return result
	}
	return v.Validate(definition)
}

// parseContext extracts validation context from the definition.
func parseContext(def map[string]interface{}) (*ValidationContext, error) {
	if def == nil {
		return nil, fmt.Errorf("dashboard definition cannot be nil")
	}

	ctx := &ValidationContext{}

	// Extract version
	if v, ok := def["version"]; ok {
		switch vt := v.(type) {
		case float64:
			ctx.Version = int(vt)
		case int:
			ctx.Version = vt
		}
	}

	// Extract widgets - handle both []interface{} (from JSON) and []map[string]interface{} (from code)
	if widgets, ok := def["widgets"]; ok {
		switch wl := widgets.(type) {
		case []interface{}:
			for _, w := range wl {
				if widgetMap, ok := w.(map[string]interface{}); ok {
					ctx.Widgets = append(ctx.Widgets, widgetMap)
				}
			}
		case []map[string]interface{}:
			ctx.Widgets = append(ctx.Widgets, wl...)
		}
	}

	// Extract variables - handle both []interface{} (from JSON) and []map[string]interface{} (from code)
	if vars, ok := def["variables"]; ok {
		switch vl := vars.(type) {
		case []interface{}:
			for _, v := range vl {
				if varMap, ok := v.(map[string]interface{}); ok {
					ctx.Variables = append(ctx.Variables, varMap)
				}
			}
		case []map[string]interface{}:
			ctx.Variables = append(ctx.Variables, vl...)
		}
	}

	return ctx, nil
}

// ValidateDashboard is a convenience function using the default validator.
func ValidateDashboard(definition map[string]interface{}) *ValidationResult {
	return NewValidator().Validate(definition)
}

// ValidateDashboardJSON is a convenience function for JSON input.
func ValidateDashboardJSON(data []byte) *ValidationResult {
	return NewValidator().ValidateJSON(data)
}
