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

type FacetKeysResourceResponse struct {
	Scope      string     `json:"scope"`
	FacetKeys  []FacetKey `json:"facet_keys"`
	UsageNotes string     `json:"usage_notes"`
}

var LogFacetKeysResource = mcp.NewResource(
	"facet-keys://logs",
	"Log Facet Keys",
	mcp.WithResourceDescription(`Available field names for filtering logs. Use in CQL queries with syntax: field:"value".
Common fields: service.name, severity_text, host.name, ed.tag, k8s.pod.name.
Logs support full-text search (terms without field: prefix). Use facet_options to get values for any field.`),
	mcp.WithMIMEType("application/json"),
)

var MetricFacetKeysResource = mcp.NewResource(
	"facet-keys://metrics",
	"Metric Facet Keys",
	mcp.WithResourceDescription(`Available field names for filtering metrics. Use in CQL queries with syntax: field:"value".
Common fields: name (metric name), service.name, host.name, ed.tag.
IMPORTANT: Metrics do NOT support full-text search - always use field:"value" syntax.
Use search_metrics tool for fuzzy metric name discovery.`),
	mcp.WithMIMEType("application/json"),
)

var TraceFacetKeysResource = mcp.NewResource(
	"facet-keys://traces",
	"Trace Facet Keys",
	mcp.WithResourceDescription(`Available field names for filtering traces. Use in CQL queries with syntax: field:"value".
Common fields: service.name, status.code, span.kind, ed.tag.
IMPORTANT: Traces do NOT support full-text search - always use field:"value" syntax.
Use facet_options to get values for any field.`),
	mcp.WithMIMEType("application/json"),
)

var PatternFacetKeysResource = mcp.NewResource(
	"facet-keys://patterns",
	"Pattern Facet Keys",
	mcp.WithResourceDescription(`Available field names for filtering log patterns. Use in CQL queries with syntax: field:"value".
Common fields: service.name, host.name, ed.tag.
Patterns support full-text search (terms without field: prefix).
Use get_log_patterns with negative:true for error/warning patterns.`),
	mcp.WithMIMEType("application/json"),
)

var EventFacetKeysResource = mcp.NewResource(
	"facet-keys://events",
	"Event Facet Keys",
	mcp.WithResourceDescription(`Available field names for filtering events (alerts, anomalies). Use in CQL queries with syntax: field:"value".
Common fields: event.type, event.domain, service.name, ed.monitor.type.
Events support full-text search (terms without field: prefix).
Event types include: pattern_anomaly, metric_threshold, log_threshold.`),
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

		response := FacetKeysResourceResponse{
			Scope:     "log",
			FacetKeys: facetKeys,
			UsageNotes: `Logs support full-text search. Use facet_options to get values for any field.
See cql://syntax resource for complete query syntax reference.`,
		}

		result, err := json.Marshal(response)
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

		response := FacetKeysResourceResponse{
			Scope:     "metric",
			FacetKeys: facetKeys,
			UsageNotes: `IMPORTANT: Metrics do NOT support full-text search - always use field:"value".
Use search_metrics for fuzzy metric name discovery.
See cql://syntax resource for complete query syntax reference.`,
		}

		result, err := json.Marshal(response)
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

func TraceFacetKeysResourceHandler(client Client) server.ResourceHandlerFunc {
	return func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		facetKeys, err := GetFacetKeys(ctx, client, "trace")
		if err != nil {
			return nil, fmt.Errorf("failed to get trace facet keys: %w", err)
		}

		response := FacetKeysResourceResponse{
			Scope:     "trace",
			FacetKeys: facetKeys,
			UsageNotes: `IMPORTANT: Traces do NOT support full-text search - always use field:"value".
Common trace fields: service.name, status.code, span.kind.
See cql://syntax resource for complete query syntax reference.`,
		}

		result, err := json.Marshal(response)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal trace facet keys: %w", err)
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

func PatternFacetKeysResourceHandler(client Client) server.ResourceHandlerFunc {
	return func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		facetKeys, err := GetFacetKeys(ctx, client, "pattern")
		if err != nil {
			return nil, fmt.Errorf("failed to get pattern facet keys: %w", err)
		}

		response := FacetKeysResourceResponse{
			Scope:     "pattern",
			FacetKeys: facetKeys,
			UsageNotes: `Patterns support full-text search. Use get_log_patterns with negative:true for error patterns.
See cql://syntax resource for complete query syntax reference.`,
		}

		result, err := json.Marshal(response)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal pattern facet keys: %w", err)
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

		response := FacetKeysResourceResponse{
			Scope:     "event",
			FacetKeys: facetKeys,
			UsageNotes: `Events support full-text search (terms without field: prefix).
Common fields: event.type, event.domain, service.name, ed.monitor.type.
See cql://syntax resource for complete query syntax reference.`,
		}

		result, err := json.Marshal(response)
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
