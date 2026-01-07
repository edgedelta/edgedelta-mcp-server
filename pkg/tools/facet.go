package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/params"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type Facet struct {
	Name    string        `json:"name"`
	Path    string        `json:"path,omitempty"`
	Scope   string        `json:"scope,omitempty"`
	Options []FacetOption `json:"options,omitempty"`
}

type FacetOption struct {
	Name  string `json:"name"`
	Count int    `json:"count,omitempty"`
}

type FacetsResponse struct {
	Builtin     []Facet `json:"builtin"`
	UserDefined []Facet `json:"userDefined"`
}

type FacetsToolResponse struct {
	Scope    string         `json:"scope"`
	Facets   []Facet        `json:"facets"`
	Guidance *FacetGuidance `json:"guidance,omitempty"`
}

type FacetGuidance struct {
	ResultStatus string   `json:"result_status"`
	NextSteps    []string `json:"next_steps,omitempty"`
	Suggestions  []string `json:"suggestions,omitempty"`
}

type FacetOptionsResponse struct {
	FacetPath   string         `json:"facet_path"`
	Scope       string         `json:"scope"`
	TotalValues int            `json:"total_values"`
	Options     []FacetOption  `json:"options"`
	Guidance    *FacetGuidance `json:"guidance,omitempty"`
}

var FacetsTool = mcp.NewTool("facets",
	mcp.WithDescription(`Retrieves all available field names (facets) for filtering in the given scope.

WHEN TO USE:
- Use discover_schema instead for most cases - it provides facet_keys plus sample values and CQL syntax
- Use this tool only if you need a complete list of field names without values

This tool returns field NAMES only, not their values.
To get VALUES for a field, use facet_options tool.

CQL SYNTAX REMINDER:
- Field filter: field:"value" (use colon, NOT = or ==)
- Attribute fields: @field:"value" (prefix with @)
- Operators: AND, OR, NOT, - (negation prefix)
- Comparison: field > 100, field <= 50
- Grouping: field:("val1" OR "val2")

Example workflow:
1. facets(scope:"log") → returns field names like ["service.name", "host.name", ...]
2. facet_options(scope:"log", facet_path:"service.name") → returns values like ["api", "web", ...]
3. Use values in CQL query: service.name:"api"`),
	mcp.WithString("scope",
		mcp.Description("The scope to retrieve facets for. Available scopes: 'log', 'metric', 'trace', 'pattern', 'event'"),
		mcp.Required(),
		mcp.Enum("log", "metric", "trace", "pattern", "event"),
	),
	mcp.WithReadOnlyHintAnnotation(true),
	mcp.WithIdempotentHintAnnotation(false),
	mcp.WithDestructiveHintAnnotation(false),
	mcp.WithOpenWorldHintAnnotation(false),
)

var FacetsResource = mcp.NewResourceTemplate(
	"facets://{scope}",
	"Facets",
	mcp.WithTemplateDescription("Facets for the given scope."),
	mcp.WithTemplateMIMEType("application/json"),
)

var FacetOptionsTool = mcp.NewTool("facet_options",
	mcp.WithDescription(`Retrieves all available values for a specific field (facet) in the given scope.

WHEN TO USE:
- After discover_schema returns facet_keys, use this to get VALUES for any field
- discover_schema only pre-fetches values for a few common fields (service.name, severity_text, etc.)
- For ALL OTHER fields in facet_keys, call facet_options to get their values
- Use before constructing queries to ensure values exist in your data

CQL SYNTAX REMINDER:
- Use the exact value returned in your query: field:"exact_value"
- For OR conditions: field:("value1" OR "value2")
- For negation: -field:"value" or NOT field:"value"
- Attribute fields use @ prefix: @custom.field:"value"

SCOPE-SPECIFIC NOTES:
- log, pattern, event: Support full-text search (terms without field: prefix)
- metric, trace: Do NOT support full-text search (always use field:"value")

Example workflow:
1. discover_schema returns facet_keys: ["service.name", "host.name", "k8s.pod.name", "custom.field"]
2. common_fields only has values for: service.name, host.name
3. To get values for k8s.pod.name or custom.field → call facet_options

Usage: facet_options(scope:"log", facet_path:"k8s.pod.name") returns all pod names
Then use in query: k8s.pod.name:"my-pod-abc123"`),
	mcp.WithString("facet_path",
		mcp.Description("The facet path to retrieve options for."),
		mcp.Required(),
	),
	mcp.WithString("scope",
		mcp.Description("The scope to retrieve facet options for. Available scopes: 'log', 'metric', 'trace', 'pattern', 'event'"),
		mcp.Required(),
		mcp.Enum("log", "metric", "trace", "pattern", "event"),
	),
	mcp.WithString("limit",
		mcp.Description("The maximum number of facet options to return. Default is 100."),
		mcp.DefaultString("100"),
	),
	mcp.WithReadOnlyHintAnnotation(true),
	mcp.WithIdempotentHintAnnotation(false),
	mcp.WithDestructiveHintAnnotation(false),
	mcp.WithOpenWorldHintAnnotation(false),
)

var FacetOptionsResource = mcp.NewResourceTemplate(
	"facet_options://{scope}/{facet}",
	"Facet Options",
	mcp.WithTemplateDescription("Facet options for the given scope and facet."),
	mcp.WithTemplateMIMEType("application/json"),
)

func FacetsToolHandler(client Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		scope, err := request.RequireString("scope")
		if err != nil {
			return mcp.NewToolResultError("missing required parameter: scope"), err
		}

		result, err := GetFacets(ctx, client, WithScope(scope))
		if err != nil {
			return nil, fmt.Errorf("failed to get facets, err: %w", err)
		}

		// Wrap result with guidance
		response := FacetsToolResponse{
			Scope:  scope,
			Facets: result,
			Guidance: &FacetGuidance{
				ResultStatus: "success",
				NextSteps: []string{
					"Use facet_options tool to get available VALUES for any field listed above.",
					fmt.Sprintf("Example: facet_options(scope:\"%s\", facet_path:\"<field_name>\") to see values.", scope),
					"Use these field names in your CQL queries: field:\"value\"",
				},
			},
		}

		r, err := json.Marshal(response)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal response, err: %w", err)
		}

		return mcp.NewToolResultText(string(r)), nil
	}
}

func FacetsResourceHandler(client Client) server.ResourceTemplateHandlerFunc {
	return func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		scope, err := extractScopeFromURI(request.Params.URI)
		if err != nil {
			return nil, fmt.Errorf("failed to extract scope from URI: %w", err)
		}

		result, err := GetFacets(ctx, client, WithScope(scope))
		if err != nil {
			return nil, fmt.Errorf("failed to get facets, err: %w", err)
		}

		r, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal response, err: %w", err)
		}
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      request.Params.URI,
				MIMEType: "application/json",
				Text:     string(r),
			},
		}, nil
	}
}

func FacetOptionsToolHandler(client Client) server.ToolHandlerFunc {
	return func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		facet, err := request.RequireString("facet_path")
		if err != nil {
			return mcp.NewToolResultError("missing required parameter: facet_path"), err
		}

		scope, err := request.RequireString("scope")
		if err != nil {
			return mcp.NewToolResultError("missing required parameter: scope"), err
		}

		limit, err := params.Optional[string](request, "limit")
		if err != nil {
			return mcp.NewToolResultError("invalid parameter: limit"), err
		}

		result, err := GetFacetOptions(ctx, client, WithScope(scope), WithFacet(facet), WithLimit(limit))
		if err != nil {
			return nil, fmt.Errorf("failed to get facet options, err: %w", err)
		}

		// Wrap result with guidance
		var options []FacetOption
		if result != nil {
			options = result.Options
		}

		response := FacetOptionsResponse{
			FacetPath:   facet,
			Scope:       scope,
			TotalValues: len(options),
			Options:     options,
		}

		if len(options) == 0 {
			response.Guidance = &FacetGuidance{
				ResultStatus: "empty",
				NextSteps: []string{
					fmt.Sprintf("No values found for field '%s' in scope '%s'.", facet, scope),
					"This field may not have data in the current time range.",
				},
				Suggestions: []string{
					"Use discover_schema to see all available fields for this scope.",
					"Try a different field name from facet_keys.",
				},
			}
		} else {
			response.Guidance = &FacetGuidance{
				ResultStatus: "success",
				NextSteps: []string{
					fmt.Sprintf("Use these values in your CQL query: %s:\"<value>\"", facet),
					"Use validate_cql to check your query syntax before executing.",
					fmt.Sprintf("Example: %s:\"%s\"", facet, options[0].Name),
				},
			}
		}

		r, err := json.Marshal(response)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal response, err: %w", err)
		}

		return mcp.NewToolResultText(string(r)), nil
	}
}

func FacetOptionsResourceHandler(client Client) server.ResourceTemplateHandlerFunc {
	return func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		scope, facet, err := extractScopeFacetFromURI(request.Params.URI)
		if err != nil {
			return nil, fmt.Errorf("failed to extract facet options from URI: %w", err)
		}
		result, err := GetFacetOptions(ctx, client, WithScope(scope), WithFacet(facet), WithLimit("100"))
		if err != nil {
			return nil, fmt.Errorf("failed to get facet options, err: %w", err)
		}
		r, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal response, err: %w", err)
		}
		return []mcp.ResourceContents{
			mcp.TextResourceContents{
				URI:      request.Params.URI,
				MIMEType: "application/json",
				Text:     string(r),
			},
		}, nil
	}
}

func WithScope(scope string) QueryParamOption {
	return func(v url.Values) {
		if scope != "" {
			v.Add("scope", scope)
		}
	}
}

func WithFacet(facet string) QueryParamOption {
	return func(v url.Values) {
		if facet != "" {
			v.Add("facet_path", facet)
		}
	}
}

func extractScopeFromURI(uri string) (string, error) {
	re := regexp.MustCompile(`^facets://([^/]+)$`)
	matches := re.FindStringSubmatch(uri)
	if len(matches) == 2 {
		return matches[1], nil
	}
	return "", fmt.Errorf("invalid format")
}

func extractScopeFacetFromURI(uri string) (string, string, error) {
	re := regexp.MustCompile(`^facet_options://([^/]+)/([^/]+)$`)
	matches := re.FindStringSubmatch(uri)
	if len(matches) == 3 {
		scope := matches[1]
		facet := matches[2]
		return scope, facet, nil
	}
	return "", "", fmt.Errorf("invalid format")
}
