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

type Service struct {
	Name string `json:"name"`
}

type ServicesResourceResponse struct {
	Services   []Service `json:"services"`
	UsageNotes string    `json:"usage_notes"`
}

type GraphRecord struct {
	Values    []string `json:"values"`
	Aggregate struct {
		Value int `json:"value"`
	} `json:"aggregate"`
}

type GraphResponse struct {
	From    string        `json:"from"`
	To      string        `json:"to"`
	Window  string        `json:"window"`
	Records []GraphRecord `json:"records"`
	Keys    []string      `json:"keys"`
}

var ServicesResource = mcp.NewResource(
	"services://list",
	"Services",
	mcp.WithResourceDescription(`List of available service names in the organization.
Services can be used to filter logs, metrics, traces, patterns, and events using the service.name field.`),
	mcp.WithMIMEType("application/json"),
)

func GetServices(ctx context.Context, client Client, opts ...QueryParamOption) ([]Service, error) {
	orgID, token, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	// Build the graph API URL with query parameters
	graphURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/logs/log_search/graph", client.APIURL(), orgID))
	if err != nil {
		return nil, err
	}

	// Set query parameters for the graph endpoint
	queryParams := url.Values{}
	queryParams.Set("order", "desc")
	queryParams.Set("scope", "log")
	queryParams.Set("lookback", "15m")
	queryParams.Set("limit", "100")
	queryParams.Set("graph_type", "table")
	queryParams.Set("query", "{*} by {service.name}")
	queryParams.Set("time_range_adjustment", "noop")
	queryParams.Set("window", "15s")

	// Apply any additional options
	for _, opt := range opts {
		opt(queryParams)
	}

	graphURL.RawQuery = queryParams.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, graphURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create services request: %v", err)
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("X-ED-API-Token", token)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %v", err)
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch services, status code %d", resp.StatusCode)
	}

	var graphResponse GraphResponse
	if err := json.NewDecoder(resp.Body).Decode(&graphResponse); err != nil {
		return nil, fmt.Errorf("failed to decode graph response: %v", err)
	}

	// Convert graph records to services
	services := make([]Service, 0, len(graphResponse.Records))
	for _, record := range graphResponse.Records {
		if len(record.Values) > 0 && record.Values[0] != "" {
			services = append(services, Service{
				Name: record.Values[0],
			})
		}
	}

	return services, nil
}

func ServicesResourceHandler(client Client) server.ResourceHandlerFunc {
	return func(ctx context.Context, request mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
		services, err := GetServices(ctx, client)
		if err != nil {
			return nil, fmt.Errorf("failed to get services: %w", err)
		}

		response := ServicesResourceResponse{
			Services: services,
			UsageNotes: `Use facet_options tool to verify a service name if not in this list.
Use discover_schema or build_cql tool for CQL syntax guidance.`,
		}

		result, err := json.Marshal(response)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal services: %w", err)
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
