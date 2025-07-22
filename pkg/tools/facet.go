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

var FacetsTool = mcp.NewTool("facets",
	mcp.WithDescription("Retrieves facets for the given scope. This can be used to filter search results."),
	mcp.WithString("scope",
		mcp.Description("The scope to retrieve facets for. Available scopes: 'log', 'metric', 'trace'"),
		mcp.Required(),
		mcp.Enum("log", "metric", "trace"),
	),
)

var FacetsResource = mcp.NewResourceTemplate(
	"facets://{scope}",
	"Facets",
	mcp.WithTemplateDescription("Facets for the given scope."),
	mcp.WithTemplateMIMEType("application/json"),
)

var FacetOptionsTool = mcp.NewTool("facet_options",
	mcp.WithDescription("Retrieves facet options for the facet in the scope. This can be used to filter search in logs, metrics, and traces with syntax <facet_path>:<facet_option>"),
	mcp.WithString("facet_path",
		mcp.Description("The facet path to retrieve options for."),
		mcp.Required(),
	),
	mcp.WithString("scope",
		mcp.Description("The scope to retrieve facet options for. Available scopes: 'log', 'metric', 'trace'"),
		mcp.Required(),
		mcp.Enum("log", "metric", "trace"),
	),
	mcp.WithString("limit",
		mcp.Description("The maximum number of facet options to return. Default is 100."),
		mcp.DefaultString("100"),
	),
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
		r, err := json.Marshal(result)
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
		r, err := json.Marshal(result)
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
