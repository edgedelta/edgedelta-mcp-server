package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// These mirror backend constants for the default auto-provisioned ingestion
// pipeline every org is given on creation:
//   - backend/provision/ingestpipeline/provisioner.go  (defaultIngestPipelineName)
//   - deploy/modules/knowledge/base_config_ingestion_pipeline.yml  (node name)
const (
	ingestionPipelineTag  = "AI-Connector-Telemetry-Pipeline"
	httpIngestionNodeName = "http_ingestion_input"
)

type ingestionEndpointOut struct {
	Protocol    string `json:"protocol"`
	DataType    string `json:"data_type"`
	URL         string `json:"url"`
	SampleData  string `json:"sample_data,omitempty"`
	TestCommand string `json:"test_command,omitempty"`
}

type getIngestionEndpointOut struct {
	Endpoints []ingestionEndpointOut `json:"endpoints"`
}

// GetIngestionEndpointTool returns Edge Delta HTTP ingestion URLs with the
// stream token pre-populated as a `?token=...` query parameter. Takes no args:
// it resolves the org's auto-provisioned ingestion pipeline, its HTTP ingest
// node, and fetches the stream token — all server-side.
func GetIngestionEndpointTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_ingestion_endpoint",
			mcp.WithTitleAnnotation("Get Ingestion Endpoint"),
			mcp.WithDescription(`Returns Edge Delta ingestion endpoints (logs, metrics, traces, events) with the stream token embedded as a ?token=... query parameter. Each endpoint includes a "protocol" field ("http" today; "grpc" may be added in the future) so callers can pick the right transport. Takes no arguments — the org's auto-provisioned ingestion pipeline and its ingest node are resolved server-side. POST raw payload bodies to HTTP URLs to send telemetry to Edge Delta.`),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, _ mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			confs, err := ListConfs(ctx, client)
			if err != nil {
				return nil, fmt.Errorf("failed to list configurations: %w", err)
			}

			confID := findDefaultIngestionConfID(confs)
			if confID == "" {
				return mcp.NewToolResultError(fmt.Sprintf(
					"could not find default ingestion pipeline (fleet_type=%q, tag=%q) for this org. /confs returned %d entries: %s",
					IngestionPipelineFleetType, ingestionPipelineTag, len(confs), summarizeConfs(confs),
				)), nil
			}

			endpoints, err := GetIngestionEndpoints(ctx, client)
			if err != nil {
				return nil, fmt.Errorf("failed to get ingestion endpoints: %w", err)
			}
			if endpoints.HTTPS == nil {
				return mcp.NewToolResultError("backend did not return HTTPS ingestion endpoints"), nil
			}

			tokenResp, err := GetIngestionToken(ctx, client, confID, httpIngestionNodeName)
			if err != nil {
				return nil, fmt.Errorf("failed to get ingestion token: %w", err)
			}
			if tokenResp.RawToken == "" {
				return mcp.NewToolResultError("backend returned empty ingestion token"), nil
			}

			base, err := url.Parse(endpoints.HTTPS.BaseURL)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid base_url %q: %v", endpoints.HTTPS.BaseURL, err)), nil
			}

			var out getIngestionEndpointOut
			for dataType, path := range endpoints.HTTPS.PathForDataType {
				u := *base
				u.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(path, "/")
				q := u.Query()
				q.Set("token", tokenResp.RawToken)
				u.RawQuery = q.Encode()

				item := ingestionEndpointOut{Protocol: "http", DataType: dataType, URL: u.String()}
				if endpoints.HTTPS.SampleData != nil {
					item.SampleData = endpoints.HTTPS.SampleData[dataType]
				}
				if cmd, ok := endpoints.HTTPS.TestCommands[dataType]; ok {
					item.TestCommand = strings.ReplaceAll(cmd, "{TOKEN}", tokenResp.RawToken)
				}
				out.Endpoints = append(out.Endpoints, item)
			}

			b, err := json.Marshal(out)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal response: %w", err)
			}
			return mcp.NewToolResultText(string(b)), nil
		}
}

// summarizeConfs returns a short debug string of fleet_type/tag pairs for each conf.
func summarizeConfs(confs []*ConfSummary) string {
	if len(confs) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(confs))
	for _, c := range confs {
		if c == nil {
			continue
		}
		parts = append(parts, fmt.Sprintf("{id=%s fleet_type=%q tag=%q}", c.ID, c.FleetType, c.Tag))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// findDefaultIngestionConfID picks the conf ID for the org's auto-provisioned
// ingestion pipeline. Prefers an exact tag match; falls back to the first
// ingestion-pipeline conf if none match the tag (covers older provisions).
func findDefaultIngestionConfID(confs []*ConfSummary) string {
	var fallback string
	for _, c := range confs {
		if c == nil || c.FleetType != IngestionPipelineFleetType {
			continue
		}
		if c.Tag == ingestionPipelineTag {
			return c.ID
		}
		if fallback == "" {
			fallback = c.ID
		}
	}
	return fallback
}
