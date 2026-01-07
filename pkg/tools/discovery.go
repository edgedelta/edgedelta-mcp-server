package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type SchemaDiscovery struct {
	Scope            string              `json:"scope"`
	FacetKeys        []FacetKey          `json:"facet_keys"`
	CommonFields     map[string][]string `json:"common_fields,omitempty"`
	CommonFieldsNote string              `json:"common_fields_note,omitempty"`
	QuerySyntax      QuerySyntaxRef      `json:"query_syntax"`
	AttributeSyntax  map[string]string   `json:"attribute_syntax,omitempty"`
	SampleQueries    []string            `json:"sample_queries"`
	Guidance         *DiscoveryGuidance  `json:"guidance,omitempty"`
}

type DiscoveryGuidance struct {
	ResultStatus string   `json:"result_status"`
	NextSteps    []string `json:"next_steps,omitempty"`
	Suggestions  []string `json:"suggestions,omitempty"`
}

// for quick reference for CQL syntax
type QuerySyntaxRef struct {
	Operators       map[string]string `json:"operators"`
	FieldFilter     string            `json:"field_filter"`
	Grouping        string            `json:"grouping"`
	Wildcard        string            `json:"wildcard"`
	Comparison      string            `json:"comparison"`
	FullTextSearch  string            `json:"full_text_search"`
	NotSupported    []string          `json:"not_supported"`
	SyntaxReference string            `json:"syntax_reference"`
}

type MetricSearchResult struct {
	Pattern    string             `json:"pattern"`
	Matches    []MetricMatch      `json:"matches"`
	TotalFound int                `json:"total_found"`
	Usage      string             `json:"usage"`
	Guidance   *DiscoveryGuidance `json:"guidance,omitempty"`
}

type MetricMatch struct {
	Name  string  `json:"name"`
	Count int     `json:"count,omitempty"`
	Score float64 `json:"score,omitempty"`
}

var defaultQuerySyntax = QuerySyntaxRef{
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
	FieldFilter:     "field:\"value\" or field:value (colon for equality)",
	Grouping:        "field:(val1 OR val2) - parentheses for OR groups",
	Wildcard:        "\"*pattern*\" or \"pattern*\" (at string boundaries only)",
	Comparison:      "field > 100, field <= 50 (only <, >, <=, >= supported)",
	FullTextSearch:  "Supported ONLY for log, pattern, event scopes. NOT supported for metric and trace. Example: error OR exception (without field: prefix)",
	NotSupported:    []string{"Regular expressions (/pattern/)", "Wildcards in middle of string", "!= and = operators (use : for equality, - for negation)", "Full-text search in metric and trace scopes"},
	SyntaxReference: "https://docs.edgedelta.com/search-logs/#search-syntax",
}

var attributeSyntaxNotes = map[string]string{
	"@prefix":        "Use @field_name for attribute fields (custom fields stored in attributes)",
	"resource_field": "Fields without @ prefix are resource fields or top-level fields",
	"example":        "@custom_field:\"value\" searches in attributes, field:\"value\" searches in resources",
}

var sampleQueriesByScope = map[string][]string{
	// Log scope: supports full-text search
	"log": {
		"service.name:\"api\"",
		"severity_text:\"ERROR\" AND service.name:\"web\"",
		"service.name:(\"api\" OR \"worker\") AND -severity_text:\"DEBUG\"",
		"ed.tag:\"prod\" AND severity_text:(\"ERROR\" OR \"WARN\")",
		"@response.code > 400",
		"error OR exception", // Full-text search supported
	},
	// Metric scope: NO full-text search - must use field:value syntax
	"metric": {
		"service.name:\"api\"",
		"name:\"http.request.duration\"",
		"ed.tag:\"prod\" AND service.name:\"api\"",
		"host.name:\"server1\"",
		// Note: Full-text search NOT supported for metrics
	},
	// Trace scope: NO full-text search - must use field:value syntax
	"trace": {
		"service.name:\"api\"",
		"status.code:\"ERROR\"",
		"span.kind:\"server\"",
		"ed.tag:\"prod\" AND status.code:\"ERROR\"",
		// Note: Full-text search NOT supported for traces
	},
	// Pattern scope: supports full-text search
	// Note: sentiment filtering is done via HTTP parameter (negative=true), not CQL
	"pattern": {
		"service.name:\"api\"",
		"ed.tag:\"prod\" AND host.name:\"server1\"",
		"error OR timeout", // Full-text search supported
	},
	// Event scope: supports full-text search
	"event": {
		"event.type:\"pattern_anomaly\"",
		"event.domain:\"Monitor Alerts\"",
		"event.type:\"metric_threshold\"",
		"anomaly OR alert", // Full-text search supported
	},
}

// GetDiscoverSchemaTool creates a tool to discover available schema for building queries
func GetDiscoverSchemaTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("discover_schema",
			mcp.WithTitleAnnotation("Discover Schema"),
			mcp.WithDescription(`Discovers available data schema for building queries.

IMPORTANT: Call this tool FIRST before constructing any search queries.

This tool returns:
- facet_keys: ALL available field names you can filter on
- common_fields: Sample values for a FEW common fields only (service.name, severity_text, etc.)
- CQL syntax reference and examples
- Sample queries that demonstrate proper syntax

IMPORTANT: common_fields is a SUBSET - it does NOT contain values for all fields.
To get values for fields NOT in common_fields, call facet_options tool.

Use the returned information to:
1. Know which fields are available for filtering (facet_keys)
2. See sample values for common fields (common_fields)
3. Call facet_options for any other field values you need
4. Construct valid CQL queries

After calling this tool, use validate_cql or build_cql to construct your query.`),
			mcp.WithString("scope",
				mcp.Description("Search scope: 'log', 'metric', 'trace', 'pattern', 'event'"),
				mcp.Required(),
				mcp.Enum("log", "metric", "trace", "pattern", "event"),
			),
			mcp.WithBoolean("include_sample_values",
				mcp.Description("If true, fetch sample values for common fields. Default: true"),
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

			includeSamples := request.GetBool("include_sample_values", true)

			result := SchemaDiscovery{
				Scope:            scope,
				QuerySyntax:      defaultQuerySyntax,
				AttributeSyntax:  attributeSyntaxNotes,
				CommonFields:     make(map[string][]string),
				CommonFieldsNote: "IMPORTANT: common_fields contains sample values for only a FEW fields. For values of OTHER fields listed in facet_keys, you MUST call facet_options tool.",
			}

			if queries, ok := sampleQueriesByScope[scope]; ok {
				result.SampleQueries = queries
			}

			facetKeys, err := GetFacetKeys(ctx, client, scope)
			if err != nil {
				// Return partial result with error info
				result.FacetKeys = []FacetKey{}
				result.Guidance = &DiscoveryGuidance{
					ResultStatus: "partial",
					NextSteps: []string{
						fmt.Sprintf("Failed to fetch facet keys: %v. Try using facets tool directly.", err),
					},
				}
			} else {
				result.FacetKeys = facetKeys
			}

			// Fetch sample values for common fields
			if includeSamples {
				// service.name is always common
				services, err := GetServices(ctx, client)
				if err == nil && len(services) > 0 {
					serviceNames := make([]string, 0, len(services))
					for _, svc := range services {
						serviceNames = append(serviceNames, svc.Name)
					}
					result.CommonFields["service.name"] = serviceNames
				}

				commonFacets := getCommonFacetsForScope(scope)
				for _, facet := range commonFacets {
					if facet == "service.name" {
						continue
					}
					facetResult, err := GetFacetOptions(ctx, client, WithScope(scope), WithFacet(facet), WithLimit("10"))
					if err == nil && facetResult != nil && len(facetResult.Options) > 0 {
						values := make([]string, 0, len(facetResult.Options))
						for _, opt := range facetResult.Options {
							values = append(values, opt.Name)
						}
						result.CommonFields[facet] = values
					}
				}
			}

			if result.Guidance == nil {
				result.Guidance = &DiscoveryGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"Use validate_cql tool to check your query syntax before executing",
						"Use build_cql tool to construct queries from structured parameters",
						"Use facet_options tool to get values for fields NOT listed in common_fields above",
						"Note: common_fields only contains a subset of fields - use facet_options for other fields from facet_keys",
					},
				}
			}

			r, _ := json.Marshal(result)
			return mcp.NewToolResultText(string(r)), nil
		}
}

// GetSearchMetricsTool creates a tool for fuzzy metric name search
func GetSearchMetricsTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("search_metrics",
			mcp.WithTitleAnnotation("Search Metrics"),
			mcp.WithDescription(`Searches for metric names using fuzzy/partial matching.

Use this tool BEFORE get_metric_search or get_metric_graph when you don't know the exact metric name.

Examples:
- search_metrics(pattern: "cpu") -> finds "system.cpu.usage", "container.cpu.percent"
- search_metrics(pattern: "request duration") -> finds "http.request.duration"
- search_metrics(pattern: "error") -> finds metrics containing "error"

Returns matching metric names ranked by relevance.
Use the exact metric name from results in get_metric_search or get_metric_graph.`),
			mcp.WithString("pattern",
				mcp.Description("Partial metric name or keywords to search for. Case-insensitive."),
				mcp.Required(),
			),
			mcp.WithNumber("limit",
				mcp.Description("Maximum number of results. Default: 20"),
				mcp.DefaultNumber(20),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			pattern, err := request.RequireString("pattern")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: pattern"), nil
			}

			limit := request.GetInt("limit", 20)
			if limit <= 0 {
				limit = 20
			}
			if limit > 100 {
				limit = 100
			}

			metricFacet, err := GetFacetOptions(ctx, client, WithScope("metric"), WithFacet("name"), WithLimit("500"))
			if err != nil {
				return nil, fmt.Errorf("failed to fetch metric names: %w", err)
			}

			var metricOptions []FacetOption
			if metricFacet != nil {
				metricOptions = metricFacet.Options
			}

			// Fuzzy match
			matches := fuzzyMatchMetrics(pattern, metricOptions, limit)

			result := MetricSearchResult{
				Pattern:    pattern,
				Matches:    matches,
				TotalFound: len(matches),
			}

			if len(matches) > 0 {
				result.Usage = fmt.Sprintf("Use the exact metric name in get_metric_search or get_metric_graph: metric_name:\"%s\"", matches[0].Name)
				result.Guidance = &DiscoveryGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						fmt.Sprintf("Use the exact metric name from results in get_metric_search or get_metric_graph: name:\"%s\"", matches[0].Name),
						"Use facet_options to get available values for filtering (e.g., service.name values).",
					},
				}
			} else {
				result.Guidance = &DiscoveryGuidance{
					ResultStatus: "empty",
					NextSteps: []string{
						"No metrics found matching your pattern.",
					},
					Suggestions: []string{
						"Try a different search term or check if metrics are being collected.",
						"Use facet_options with scope:'metric' and facet_path:'name' to see all available metrics.",
					},
				}
			}

			r, _ := json.Marshal(result)
			return mcp.NewToolResultText(string(r)), nil
		}
}

func getCommonFacetsForScope(scope string) []string {
	if keys, ok := CommonFacetKeys[scope]; ok {
		return keys
	}
	return []string{"service.name"}
}

func fuzzyMatchMetrics(pattern string, options []FacetOption, limit int) []MetricMatch {
	pattern = strings.ToLower(pattern)
	patterns := strings.Fields(pattern)

	var matches []MetricMatch

	for _, opt := range options {
		name := strings.ToLower(opt.Name)
		score := 0.0

		allMatch := true
		for _, p := range patterns {
			if strings.Contains(name, p) {
				score += 1.0
				// Bonus for exact segment match
				if strings.Contains(name, "."+p+".") || strings.HasPrefix(name, p+".") || strings.HasSuffix(name, "."+p) {
					score += 0.5
				}
			} else {
				allMatch = false
			}
		}

		if score > 0 {
			// Bonus if all patterns match
			if allMatch {
				score += 1.0
			}
			// Bonus for shorter names
			score += 1.0 / float64(len(name))

			matches = append(matches, MetricMatch{
				Name:  opt.Name,
				Count: opt.Count,
				Score: score,
			})
		}
	}

	for i := 0; i < len(matches)-1; i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[j].Score > matches[i].Score {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}

	if len(matches) > limit {
		matches = matches[:limit]
	}

	return matches
}
