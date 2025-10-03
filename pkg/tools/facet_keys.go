package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type FacetKey struct {
	Key string `json:"key"`
}

var LogFacetKeysResource = mcp.NewResource(
	"facet-keys://logs",
	"Log Facet Keys",
	mcp.WithResourceDescription("List of available facet keys for logs."),
	mcp.WithMIMEType("application/json"),
)

var MetricFacetKeysResource = mcp.NewResource(
	"facet-keys://metrics",
	"Metric Facet Keys",
	mcp.WithResourceDescription("List of available facet keys for metrics."),
	mcp.WithMIMEType("application/json"),
)

var EventFacetKeysResource = mcp.NewResource(
	"facet-keys://events",
	"Event Facet Keys",
	mcp.WithResourceDescription("List of available facet keys for events."),
	mcp.WithMIMEType("application/json"),
)

func GetFacetKeys(ctx context.Context, client Client, scope string, opts ...QueryParamOption) ([]FacetKey, error) {
	orgID, token, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	// Build the facet_keys API URL
	facetKeysURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/facet_keys", client.APIURL(), orgID))
	if err != nil {
		return nil, err
	}

	// Set query parameters
	queryParams := url.Values{}
	queryParams.Set("query", "")
	queryParams.Set("lookback", "15m")
	queryParams.Set("scope", scope)
	queryParams.Set("limit", "100")

	// Apply any additional options
	for _, opt := range opts {
		opt(queryParams)
	}

	facetKeysURL.RawQuery = queryParams.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, facetKeysURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create facet keys request: %v", err)
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("X-ED-API-Token", token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %v", err)
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch facet keys, status code %d", resp.StatusCode)
	}

	var facetKeys []FacetKey
	if err := json.NewDecoder(resp.Body).Decode(&facetKeys); err != nil {
		return nil, fmt.Errorf("failed to decode facet keys response: %v", err)
	}

	return facetKeys, nil
}

func LogFacetKeysResourceHandler(client Client) server.ResourceHandlerFunc {
	return func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		facetKeys, err := GetFacetKeys(ctx, client, "log")
		if err != nil {
			return nil, fmt.Errorf("failed to get log facet keys: %w", err)
		}

		result, err := json.Marshal(facetKeys)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal log facet keys: %w", err)
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

func MetricFacetKeysResourceHandler(client Client) server.ResourceHandlerFunc {
	return func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		facetKeys, err := GetFacetKeys(ctx, client, "metric")
		if err != nil {
			return nil, fmt.Errorf("failed to get metric facet keys: %w", err)
		}

		result, err := json.Marshal(facetKeys)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal metric facet keys: %w", err)
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

func EventFacetKeysResourceHandler(client Client) server.ResourceHandlerFunc {
	return func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		facetKeys, err := GetFacetKeys(ctx, client, "event")
		if err != nil {
			return nil, fmt.Errorf("failed to get event facet keys: %w", err)
		}

		result, err := json.Marshal(facetKeys)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal event facet keys: %w", err)
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
