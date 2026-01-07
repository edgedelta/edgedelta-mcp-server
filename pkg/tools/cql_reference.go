package tools

import (
	"context"
	"encoding/json"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type CQLSyntaxReference struct {
	Title           string            `json:"title"`
	Description     string            `json:"description"`
	Operators       map[string]string `json:"operators"`
	FieldSyntax     FieldSyntaxGuide  `json:"field_syntax"`
	ScopeRules      map[string]string `json:"scope_rules"`
	NotSupported    []string          `json:"not_supported"`
	Examples        []CQLExample      `json:"examples"`
	SyntaxReference string            `json:"syntax_reference"`
}

type FieldSyntaxGuide struct {
	FieldFilter    string `json:"field_filter"`
	AttributeField string `json:"attribute_field"`
	Grouping       string `json:"grouping"`
	Wildcard       string `json:"wildcard"`
	Comparison     string `json:"comparison"`
	FullTextSearch string `json:"full_text_search"`
}

type CQLExample struct {
	Description string `json:"description"`
	Query       string `json:"query"`
	Scope       string `json:"scope,omitempty"`
}

var cqlSyntaxReference = CQLSyntaxReference{
	Title:       "CQL (Common Query Language) Syntax Reference",
	Description: "Use this reference when building queries for logs, metrics, traces, patterns, and events.",
	Operators: map[string]string{
		"AND": "Both conditions must match (default for space-separated terms)",
		"OR":  "Either condition can match",
		"NOT": "Exclude matching results (or use - prefix)",
		"-":   "Negation prefix (e.g., -field:value)",
		">":   "Greater than comparison (e.g., field > 100)",
		"<":   "Less than comparison (e.g., field < 100)",
		">=":  "Greater than or equal (e.g., field >= 100)",
		"<=":  "Less than or equal (e.g., field <= 100)",
	},
	FieldSyntax: FieldSyntaxGuide{
		FieldFilter:    "field:\"value\" - Use colon (:) for equality, NOT = or ==",
		AttributeField: "@field:\"value\" - Use @ prefix for attribute fields (custom fields stored in attributes)",
		Grouping:       "field:(\"val1\" OR \"val2\") - Parentheses for OR groups",
		Wildcard:       "\"*pattern*\" or \"pattern*\" - Wildcards at string boundaries only",
		Comparison:     "field > 100, field <= 50 - Numeric comparisons with <, >, <=, >=",
		FullTextSearch: "error OR exception - Terms without field: prefix (log, pattern, event scopes only)",
	},
	ScopeRules: map[string]string{
		"log":     "Supports full-text search. Common fields: service.name, severity_text, host.name, ed.tag",
		"metric":  "NO full-text search - always use field:\"value\". Use search_metrics for fuzzy metric name discovery. Common fields: name, service.name, host.name",
		"trace":   "NO full-text search - always use field:\"value\". Common fields: service.name, status.code, span.kind",
		"pattern": "Supports full-text search. Use get_log_patterns with negative:true for error patterns. Common fields: service.name, host.name",
		"event":   "Supports full-text search. Common fields: event.type, event.domain, service.name, ed.monitor.type",
	},
	NotSupported: []string{
		"Regular expressions (/pattern/)",
		"Wildcards in middle of string (use \"*pattern*\" at boundaries only)",
		"!= operator (use -field:\"value\" or NOT field:\"value\" for negation)",
		"= or == operators (use : colon for equality)",
		"Full-text search in metric and trace scopes",
	},
	Examples: []CQLExample{
		{Description: "Filter by service name", Query: "service.name:\"api\"", Scope: "all"},
		{Description: "Error logs from a service", Query: "service.name:\"api\" AND severity_text:\"ERROR\"", Scope: "log"},
		{Description: "Multiple services", Query: "service.name:(\"api\" OR \"worker\")", Scope: "all"},
		{Description: "Exclude debug logs", Query: "-severity_text:\"DEBUG\"", Scope: "log"},
		{Description: "Attribute field comparison", Query: "@response.code > 400", Scope: "log"},
		{Description: "Metric by name", Query: "name:\"http.server.request.duration\"", Scope: "metric"},
		{Description: "Failed traces", Query: "status.code:\"ERROR\" AND span.kind:\"SERVER\"", Scope: "trace"},
		{Description: "Full-text search", Query: "error OR timeout", Scope: "log,pattern,event"},
		{Description: "Monitor alerts", Query: "event.domain:\"Monitor Alerts\"", Scope: "event"},
	},
	SyntaxReference: "https://docs.edgedelta.com/search-logs/#search-syntax",
}

var CQLReferenceResource = mcp.NewResource(
	"cql://syntax",
	"CQL Syntax Reference",
	mcp.WithResourceDescription(`Centralized reference for CQL (Common Query Language) syntax.
Use this resource to understand how to build valid queries for logs, metrics, traces, patterns, and events.
Includes operators, field syntax, scope-specific rules, and examples.`),
	mcp.WithMIMEType("application/json"),
)

func CQLReferenceResourceHandler() server.ResourceHandlerFunc {
	return func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		result, err := json.Marshal(cqlSyntaxReference)
		if err != nil {
			return nil, err
		}

		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      request.Params.URI,
				MIMEType: "application/json",
				Text:     string(result),
			},
		}, nil
	}
}
