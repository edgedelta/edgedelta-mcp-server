package validation

import "fmt"

// VariableReferenceRule ensures variable-control widgets reference valid variables.
type VariableReferenceRule struct{}

func (r *VariableReferenceRule) Name() string { return "variable_reference" }

func (r *VariableReferenceRule) Validate(ctx *DashboardContext) *ValidationResult {
	result := &ValidationResult{}

	// Build set of valid variable IDs
	validVarIDs := make(map[interface{}]bool)
	for _, v := range ctx.Variables {
		if id, ok := v["id"]; ok {
			validVarIDs[id] = true
			// Also check for float64 version (JSON unmarshals numbers as float64)
			if intID, ok := id.(int); ok {
				validVarIDs[float64(intID)] = true
			}
		}
	}

	// Check variable-control widgets reference valid variables
	for _, w := range ctx.Widgets {
		widgetType, _ := w["type"].(string)
		if widgetType != "variable-control" {
			continue
		}

		varID, hasVarID := w["variableId"]
		if !hasVarID {
			// Also check camelCase version
			varID, hasVarID = w["variable_id"]
		}

		if !hasVarID {
			id := w["id"]
			result.AddError(
				fmt.Sprintf("widget[%v].variableId", id),
				"Variable-control widget must reference a variable",
				"Add variableId field pointing to a valid variable",
			)
			continue
		}

		if !validVarIDs[varID] {
			id := w["id"]
			result.AddError(
				fmt.Sprintf("widget[%v].variableId", id),
				fmt.Sprintf("Variable-control references unknown variable: %v", varID),
				"Reference a variable ID defined in the variables array",
			)
		}
	}

	return result
}

// TargetIDReferenceRule ensures widgets with targetId reference valid widget IDs.
type TargetIDReferenceRule struct{}

func (r *TargetIDReferenceRule) Name() string { return "target_id_reference" }

func (r *TargetIDReferenceRule) Validate(ctx *DashboardContext) *ValidationResult {
	result := &ValidationResult{}

	// Build set of valid widget IDs
	validWidgetIDs := make(map[interface{}]bool)
	for _, w := range ctx.Widgets {
		if id, ok := w["id"]; ok {
			validWidgetIDs[id] = true
		}
	}

	// Check widgets with position.targetId reference valid widgets
	for _, w := range ctx.Widgets {
		pos, ok := w["position"].(map[string]interface{})
		if !ok {
			continue
		}

		targetID, hasTargetID := pos["targetId"]
		if !hasTargetID {
			continue
		}

		if !validWidgetIDs[targetID] {
			id := w["id"]
			result.AddError(
				fmt.Sprintf("widget[%v].position.targetId", id),
				fmt.Sprintf("Widget references unknown target widget: %v", targetID),
				"Reference a valid widget ID in targetId",
			)
		}
	}

	return result
}
