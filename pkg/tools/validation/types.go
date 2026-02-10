package validation

// ValidationError represents a single validation error with suggestion for correction.
type ValidationError struct {
	Parameter  string `json:"parameter"`
	Message    string `json:"message"`
	Suggestion string `json:"suggestion,omitempty"`
}

// ValidationResult holds all validation errors collected during validation.
type ValidationResult struct {
	Errors []ValidationError `json:"errors,omitempty"`
}

// IsValid returns true if no validation errors were found.
func (r *ValidationResult) IsValid() bool {
	return len(r.Errors) == 0
}

// AddError appends a validation error to the result.
func (r *ValidationResult) AddError(param, message, suggestion string) {
	r.Errors = append(r.Errors, ValidationError{
		Parameter:  param,
		Message:    message,
		Suggestion: suggestion,
	})
}

// Merge combines errors from another result.
func (r *ValidationResult) Merge(other *ValidationResult) {
	if other != nil {
		r.Errors = append(r.Errors, other.Errors...)
	}
}

// ValidationContext holds the dashboard definition being validated.
type ValidationContext struct {
	Version   int                      `json:"version"`
	Widgets   []map[string]interface{} `json:"widgets"`
	Variables []map[string]interface{} `json:"variables,omitempty"`
}

// GridArea represents a widget's position in a grid.
type GridArea struct {
	Row        int
	Column     int
	RowSpan    int
	ColumnSpan int
	WidgetID   interface{} // Can be int or string
}

// Rule is the interface that all validation rules must implement.
type Rule interface {
	// Name returns the rule identifier for error reporting.
	Name() string
	// Validate checks the context and returns any errors found.
	Validate(ctx *ValidationContext) *ValidationResult
}
