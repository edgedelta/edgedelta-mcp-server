package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type CQLValidationResult struct {
	Valid           bool                `json:"valid"`
	NormalizedQuery string              `json:"normalized_query,omitempty"`
	Errors          []string            `json:"errors,omitempty"`
	Warnings        []string            `json:"warnings,omitempty"`
	Suggestions     []string            `json:"suggestions,omitempty"`
	SyntaxReference string              `json:"syntax_reference,omitempty"`
	Guidance        *ValidationGuidance `json:"guidance,omitempty"`
}

type CQLBuildResult struct {
	Valid           bool                `json:"valid"`
	Query           string              `json:"query,omitempty"`
	ValidatedFields []string            `json:"validated_fields,omitempty"`
	UnknownFields   []string            `json:"unknown_fields,omitempty"`
	Errors          []string            `json:"errors,omitempty"`
	Suggestions     []string            `json:"suggestions,omitempty"`
	Guidance        *ValidationGuidance `json:"guidance,omitempty"`
}

type ValidationGuidance struct {
	ResultStatus string   `json:"result_status"`
	NextSteps    []string `json:"next_steps,omitempty"`
}

var (
	regexPattern       = regexp.MustCompile(`/[^/]+/`)                                    // Matches /pattern/
	middlewildcard     = regexp.MustCompile(`"[^"]*\*[^"*]+\*[^"]*"`)                     // Matches "*mid*dle*"
	invalidWildcard    = regexp.MustCompile(`[^"]\*|\*[^"]`)                              // Wildcards outside quotes
	quotedValuePattern = regexp.MustCompile(`"([^"\\]*(?:\\.[^"\\]*)*)"`)                 // Quoted value
	fieldValuePattern  = regexp.MustCompile(`(@?[a-zA-Z_][a-zA-Z0-9_.-]*)\s*[:=<>!]+\s*`) // field:value or field>value pattern
)

const AttributeLabelPrefix = "@"

// CommonFacetKeys contains known facet keys for each scope.
// Keep this list MINIMAL for progressive discovery.
// LLMs should use facet_options to discover other fields.
var CommonFacetKeys = map[string][]string{
	"log":     {"service.name", "severity_text", "host.name", "ed.tag"},
	"metric":  {"service.name", "name", "host.name", "ed.tag"},
	"trace":   {"service.name", "status.code", "span.kind", "ed.tag"},
	"pattern": {"service.name", "host.name", "ed.tag"},
	"event":   {"event.type", "event.domain", "service.name"},
}

// GetValidateCQLTool creates a tool to validate CQL queries before execution
func GetValidateCQLTool() (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("validate_cql",
			mcp.WithDescription(`Validates a CQL (Common Query Language) query BEFORE executing search.

IMPORTANT: Always use this tool to check query syntax before calling search tools.

CQL Syntax Rules:
- Field equals: field:"value" or field:value (colon operator)
- Multiple values: field:(val1 OR val2) - values ORed within parentheses
- Negation: -field:"value" or NOT field:"value"
- Boolean: term1 AND term2 (space defaults to AND)
- Comparison operators: field > 100, field <= 50 (only <, >, <=, >= supported)
- Wildcards: "*pattern*" or "pattern*" (at string boundaries only)
- Full-text search: just type words without field prefix

Field Types:
- Regular fields: service.name, severity_text, host.name (resource fields)
- Attribute fields: @custom_field (custom attributes with @ prefix)

NOT SUPPORTED:
- Regular expressions (e.g., /pattern/)
- Wildcards in middle of strings (e.g., "err*or")

Returns validation result with errors, warnings, and suggestions for fixes.`),
			mcp.WithString("query",
				mcp.Description("The CQL query to validate"),
				mcp.Required(),
			),
			mcp.WithString("scope",
				mcp.Description("The search scope: 'log', 'metric', 'trace', 'pattern', 'event'"),
				mcp.Required(),
				mcp.Enum("log", "metric", "trace", "pattern", "event"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			query, err := request.RequireString("query")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: query"), nil
			}

			scope, err := request.RequireString("scope")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: scope"), nil
			}

			result := validateCQL(query, scope)
			r, _ := json.Marshal(result)
			return mcp.NewToolResultText(string(r)), nil
		}
}

// GetBuildCQLTool creates a tool to build valid CQL queries from structured parameters
func GetBuildCQLTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("build_cql",
			mcp.WithDescription(`Constructs a valid CQL query from structured filter parameters.

Use this tool instead of manually writing CQL strings to avoid syntax errors.

This tool:
1. Validates field names against known facet keys
2. Constructs proper CQL syntax with correct quoting
3. Handles AND/OR/NOT logic automatically
4. Returns the query ready for use in search tools

Filter format (JSON object):
{
  "field_name": "exact_value",              // Equals: field:"value"
  "field_name": ["val1", "val2"],           // OR condition: field:("val1" OR "val2")
  "field_name": {"not": "value"},           // Negation: -field:"value"
  "field_name": {"gt": 100},                // Greater than: field > 100
  "field_name": {"lt": 100},                // Less than: field < 100
  "field_name": {"gte": 100},               // Greater or equal: field >= 100
  "field_name": {"lte": 100},               // Less or equal: field <= 100
  "field_name": {"wildcard": "*error*"}     // Wildcard: field:"*error*"
}

Field Types:
- Use regular field names for resource fields: service.name, severity_text, host.name
- Use @prefix for attribute fields: @custom_field, @response.code

Example:
Input: {"service.name": "api", "severity_text": ["ERROR", "WARN"]}
Output: service.name:"api" AND severity_text:("ERROR" OR "WARN")`),
			mcp.WithString("scope",
				mcp.Description("Search scope: 'log', 'metric', 'trace', 'pattern', 'event'"),
				mcp.Required(),
				mcp.Enum("log", "metric", "trace", "pattern", "event"),
			),
			mcp.WithObject("filters",
				mcp.Description("Filter conditions as JSON object"),
				mcp.Required(),
			),
			mcp.WithBoolean("check_values",
				mcp.Description("If true, suggests calling facet_options to verify field values exist. Default: true"),
				mcp.DefaultBool(true),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			scope, err := request.RequireString("scope")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: scope"), nil
			}

			args := request.GetArguments()
			filtersRaw, exists := args["filters"]
			if !exists || filtersRaw == nil {
				return mcp.NewToolResultError("missing required parameter: filters"), nil
			}

			filters, ok := filtersRaw.(map[string]any)
			if !ok {
				return mcp.NewToolResultError("filters must be a JSON object"), nil
			}

			result := buildCQL(scope, filters)
			r, _ := json.Marshal(result)
			return mcp.NewToolResultText(string(r)), nil
		}
}

func validateCQL(query, scope string) CQLValidationResult {
	result := CQLValidationResult{
		Valid:           true,
		NormalizedQuery: strings.TrimSpace(query),
		SyntaxReference: "https://docs.edgedelta.com/search-logs/#search-syntax",
	}

	// Check for empty query
	if strings.TrimSpace(query) == "" {
		result.Warnings = append(result.Warnings, "Empty query will match all records. Use '*' explicitly if intended.")
		return result
	}

	// Check for regex patterns
	if regexPattern.MatchString(query) {
		result.Valid = false
		result.Errors = append(result.Errors, "Regular expressions (e.g., /pattern/) are not supported in CQL.")
		result.Suggestions = append(result.Suggestions, "Use wildcards instead: \"*pattern*\" (only at string boundaries)")
	}

	// Check for invalid wildcard usage
	if invalidWildcard.MatchString(query) {
		result.Valid = false
		result.Errors = append(result.Errors, "Wildcards (*) must be inside quoted strings.")
		result.Suggestions = append(result.Suggestions, "Wrap the value in quotes: field:\"*value*\"")
	}

	// Check for middle wildcards
	if middlewildcard.MatchString(query) {
		result.Warnings = append(result.Warnings, "Wildcards work best at string boundaries (*value or value*), middle wildcards may not work as expected.")
	}

	// Check for @ prefix usage (attribute fields)
	if strings.Contains(query, "@") {
		result.Suggestions = append(result.Suggestions, "Fields with @ prefix are attribute fields (custom fields). Without @ prefix, fields are resource fields or top-level fields.")
	}

	// Check for common syntax mistakes
	if strings.Contains(query, "==") {
		result.Valid = false
		result.Errors = append(result.Errors, "Use single colon (:) for field matching, not ==")
		result.Suggestions = append(result.Suggestions, "Replace field==value with field:\"value\"")
	}

	if strings.Contains(query, "!=") {
		result.Warnings = append(result.Warnings, "For negation, use -field:\"value\" or NOT field:\"value\" instead of !=")
	}

	// Check for full-text search
	if scope == "metric" || scope == "trace" {
		if hasFullTextSearch(query) {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("Full-text search (queries without field: prefix) is NOT supported for %s scope.", scope))
			result.Suggestions = append(result.Suggestions, "Use field:\"value\" syntax for all terms. Example: service.name:\"api\" instead of just \"api\"")
		}
	}

	// Validate field names against known facets for the scope
	knownFields := CommonFacetKeys[scope]
	if len(knownFields) > 0 {
		matches := fieldValuePattern.FindAllStringSubmatch(query, -1)
		for _, match := range matches {
			if len(match) >= 2 {
				fieldName := match[1]
				isKnown := false
				for _, known := range knownFields {
					if strings.EqualFold(fieldName, known) {
						isKnown = true
						break
					}
				}
				if !isKnown && !strings.HasPrefix(fieldName, "@") {
					result.Warnings = append(result.Warnings,
						fmt.Sprintf("Field '%s' is not a commonly known facet for scope '%s'. Use facet_options to verify this field exists.", fieldName, scope))
				}
			}
		}
	}

	normalized := strings.TrimSpace(query)
	normalized = strings.ReplaceAll(normalized, "  ", " ")
	result.NormalizedQuery = normalized

	// wrap with guidance
	if result.Valid {
		result.Guidance = &ValidationGuidance{
			ResultStatus: "valid",
			NextSteps: []string{
				fmt.Sprintf("Query is valid. Use it in get_%s_search or get_%s_graph tool.", getScopeSearchType(scope), getScopeSearchType(scope)),
				"If you get empty results, use facet_options to verify field values exist in your data.",
			},
		}
	} else {
		result.Guidance = &ValidationGuidance{
			ResultStatus: "invalid",
			NextSteps: []string{
				"Fix the errors above and validate again.",
				"Use build_cql tool to construct queries from structured parameters to avoid syntax errors.",
			},
		}
	}

	return result
}

func buildCQL(scope string, filters map[string]any) CQLBuildResult {
	result := CQLBuildResult{
		Valid:           true,
		ValidatedFields: []string{},
		UnknownFields:   []string{},
	}

	if len(filters) == 0 {
		result.Query = "*"
		result.Suggestions = append(result.Suggestions, "Empty filters will match all records")
		return result
	}

	knownFields := CommonFacetKeys[scope]
	var queryParts []string

	for field, value := range filters {
		isKnown := false
		for _, known := range knownFields {
			if strings.EqualFold(field, known) {
				isKnown = true
				break
			}
		}

		if isKnown {
			result.ValidatedFields = append(result.ValidatedFields, field)
		} else {
			result.UnknownFields = append(result.UnknownFields, field)
		}

		switch v := value.(type) {
		case string:
			queryParts = append(queryParts, fmt.Sprintf("%s:\"%s\"", field, escapeValue(v)))

		case []any:
			// OR condition
			var orParts []string
			for _, item := range v {
				if str, ok := item.(string); ok {
					orParts = append(orParts, fmt.Sprintf("\"%s\"", escapeValue(str)))
				}
			}
			if len(orParts) > 0 {
				queryParts = append(queryParts, fmt.Sprintf("%s:(%s)", field, strings.Join(orParts, " OR ")))
			}

		case map[string]any:
			// Special operators
			if notVal, ok := v["not"]; ok {
				if str, ok := notVal.(string); ok {
					queryParts = append(queryParts, fmt.Sprintf("-%s:\"%s\"", field, escapeValue(str)))
				}
			}
			if gtVal, ok := v["gt"]; ok {
				queryParts = append(queryParts, fmt.Sprintf("%s > %v", field, gtVal))
			}
			if ltVal, ok := v["lt"]; ok {
				queryParts = append(queryParts, fmt.Sprintf("%s < %v", field, ltVal))
			}
			if gteVal, ok := v["gte"]; ok {
				queryParts = append(queryParts, fmt.Sprintf("%s >= %v", field, gteVal))
			}
			if lteVal, ok := v["lte"]; ok {
				queryParts = append(queryParts, fmt.Sprintf("%s <= %v", field, lteVal))
			}
			if wildcardVal, ok := v["wildcard"]; ok {
				if str, ok := wildcardVal.(string); ok {
					queryParts = append(queryParts, fmt.Sprintf("%s:\"%s\"", field, str))
				}
			}

		default:
			queryParts = append(queryParts, fmt.Sprintf("%s:\"%v\"", field, value))
		}
	}

	result.Query = strings.Join(queryParts, " AND ")

	// Wrap with guidance and suggestions
	if len(result.UnknownFields) > 0 {
		result.Suggestions = append(result.Suggestions,
			fmt.Sprintf("Unknown fields detected: %v. Use facet_options to verify field names exist for scope '%s'.",
				result.UnknownFields, scope))
	}

	result.Guidance = &ValidationGuidance{
		ResultStatus: "success",
		NextSteps: []string{
			fmt.Sprintf("Use the query in get_%s_search or get_%s_graph tool.", getScopeSearchType(scope), getScopeSearchType(scope)),
			"Use facet_options to verify the field values you're filtering on actually exist in your data.",
		},
	}

	return result
}

func escapeValue(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	return s
}

func getScopeSearchType(scope string) string {
	switch scope {
	case "log":
		return "log"
	case "metric":
		return "metric"
	case "trace":
		return "trace"
	case "pattern":
		return "log_patterns"
	case "event":
		return "event"
	default:
		return scope
	}
}

func hasFullTextSearch(query string) bool {
	if query == "" || query == "*" {
		return false
	}

	// Replace quoted strings with placeholder
	cleaned := quotedValuePattern.ReplaceAllString(query, "QUOTED")

	// Remove field:value patterns
	cleaned = fieldValuePattern.ReplaceAllString(cleaned, "")

	// Remove operators and parentheses
	cleaned = strings.ReplaceAll(cleaned, "AND", " ")
	cleaned = strings.ReplaceAll(cleaned, "OR", " ")
	cleaned = strings.ReplaceAll(cleaned, "NOT", " ")
	cleaned = strings.ReplaceAll(cleaned, "(", " ")
	cleaned = strings.ReplaceAll(cleaned, ")", " ")
	cleaned = strings.ReplaceAll(cleaned, "-", " ")
	cleaned = strings.ReplaceAll(cleaned, "*", " ")
	cleaned = strings.ReplaceAll(cleaned, "QUOTED", " ")

	// Check if there are remaining non-whitespace terms
	cleaned = strings.TrimSpace(cleaned)
	if cleaned == "" {
		return false
	}

	terms := strings.Fields(cleaned)
	for _, term := range terms {
		// Skip if an operator or comparison
		if term == ">" || term == "<" || term == ">=" || term == "<=" {
			continue
		}
		// Skip numbers
		if _, err := fmt.Sscanf(term, "%f", new(float64)); err == nil {
			continue
		}

		if len(term) > 0 {
			return true
		}
	}

	return false
}
