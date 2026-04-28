package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"gopkg.in/yaml.v3"
)

const (
	ingestionPipelineTag   = "AI-Connector-Telemetry-Pipeline"
	httpIngestionInputType = "http_ingestion_input"
)

type ingestionEndpointOut struct {
	Protocol    string `json:"protocol"`
	DataType    string `json:"data_type"`
	URL         string `json:"url"`
	SampleData  string `json:"sample_data,omitempty"`
	TestCommand string `json:"test_command,omitempty"`
}

type ingestionPipelineOut struct {
	ConfID    string                 `json:"conf_id"`
	Name      string                 `json:"name"`
	NodeName  string                 `json:"node_name"`
	Default   bool                   `json:"default"`
	Endpoints []ingestionEndpointOut `json:"endpoints"`
	Error     string                 `json:"error,omitempty"`
}

type getIngestionEndpointOut struct {
	Pipelines []ingestionPipelineOut `json:"pipelines"`
}

// GetIngestionEndpointTool returns Edge Delta ingestion URLs for every
// IngestionPipeline conf the caller can see. The pipeline tagged
// "AI-Connector-Telemetry-Pipeline" — auto-provisioned for every org — is
// flagged default=true so the LLM can prefer it when no other signal
// exists. Tokens are fetched server-side; the caller never provides one.
func GetIngestionEndpointTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_ingestion_endpoint",
			mcp.WithTitleAnnotation("Get Ingestion Endpoint"),
			mcp.WithDescription(`Returns Edge Delta ingestion endpoints for every ingestion pipeline in the org (logs, metrics, traces, events) with the stream token embedded as a ?token=... query parameter. Each pipeline includes default=true if it is the auto-provisioned default ("AI-Connector-Telemetry-Pipeline"); prefer the default unless the user has named a specific pipeline. Each endpoint carries a "protocol" field ("http" today; "grpc" may be added later). POST raw payload bodies to HTTP URLs to send telemetry to Edge Delta.`),
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

			ingestionConfs := filterIngestionConfs(confs)
			if len(ingestionConfs) == 0 {
				return mcp.NewToolResultError(fmt.Sprintf(
					"no ingestion pipelines (fleet_type=%q) visible to this caller. /confs returned %d entries: %s",
					IngestionPipelineFleetType, len(confs), summarizeConfs(confs),
				)), nil
			}

			endpoints, err := GetIngestionEndpoints(ctx, client)
			if err != nil {
				return nil, fmt.Errorf("failed to get ingestion endpoints: %w", err)
			}
			if endpoints.HTTPS == nil {
				return mcp.NewToolResultError("backend did not return HTTPS ingestion endpoints"), nil
			}
			base, err := url.Parse(endpoints.HTTPS.BaseURL)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("invalid base_url %q: %v", endpoints.HTTPS.BaseURL, err)), nil
			}

			var out getIngestionEndpointOut
			for _, c := range ingestionConfs {
				out.Pipelines = append(out.Pipelines, resolvePipeline(ctx, client, c, base, endpoints.HTTPS)...)
			}

			b, err := json.Marshal(out)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal response: %w", err)
			}
			return mcp.NewToolResultText(string(b)), nil
		}
}

// resolvePipeline expands a single ingestion-pipeline conf into one entry per
// HTTP ingest node it declares. If the conf detail can't be fetched or the
// YAML doesn't expose any HTTP ingest node, a single placeholder entry with
// an Error field is returned so the LLM has visibility into the failure.
func resolvePipeline(
	ctx context.Context,
	client Client,
	c *ConfSummary,
	base *url.URL,
	httpsCfg *HTTPSIngestionEndpoints,
) []ingestionPipelineOut {
	isDefault := c.Tag == ingestionPipelineTag

	detail, err := GetConf(ctx, client, c.ID)
	if err != nil {
		return []ingestionPipelineOut{{
			ConfID:  c.ID,
			Name:    c.Tag,
			Default: isDefault,
			Error:   fmt.Sprintf("failed to fetch pipeline content: %v", err),
		}}
	}

	nodeNames := findHTTPIngestNodeNames(detail.Content)
	if len(nodeNames) == 0 {
		return []ingestionPipelineOut{{
			ConfID:  c.ID,
			Name:    c.Tag,
			Default: isDefault,
			Error:   fmt.Sprintf("no %q nodes in this pipeline", httpIngestionInputType),
		}}
	}

	out := make([]ingestionPipelineOut, 0, len(nodeNames))
	for _, nodeName := range nodeNames {
		entry := ingestionPipelineOut{
			ConfID:   c.ID,
			Name:     c.Tag,
			NodeName: nodeName,
			Default:  isDefault,
		}
		tokenResp, err := GetIngestionToken(ctx, client, c.ID, nodeName)
		if err != nil {
			entry.Error = fmt.Sprintf("failed to fetch token: %v", err)
			out = append(out, entry)
			continue
		}
		if tokenResp.RawToken == "" {
			entry.Error = "backend returned empty ingestion token"
			out = append(out, entry)
			continue
		}
		entry.Endpoints = buildEndpoints(base, httpsCfg, tokenResp.RawToken)
		out = append(out, entry)
	}
	return out
}

func buildEndpoints(base *url.URL, cfg *HTTPSIngestionEndpoints, rawToken string) []ingestionEndpointOut {
	endpoints := make([]ingestionEndpointOut, 0, len(cfg.PathForDataType))
	for dataType, path := range cfg.PathForDataType {
		u := *base
		u.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(path, "/")
		q := u.Query()
		q.Set("token", rawToken)
		u.RawQuery = q.Encode()

		item := ingestionEndpointOut{Protocol: "http", DataType: dataType, URL: u.String()}
		if cfg.SampleData != nil {
			item.SampleData = cfg.SampleData[dataType]
		}
		if cmd, ok := cfg.TestCommands[dataType]; ok {
			item.TestCommand = strings.ReplaceAll(cmd, "{TOKEN}", rawToken)
		}
		endpoints = append(endpoints, item)
	}
	return endpoints
}

func filterIngestionConfs(confs []*ConfSummary) []*ConfSummary {
	out := make([]*ConfSummary, 0, len(confs))
	for _, c := range confs {
		if c == nil || c.FleetType != IngestionPipelineFleetType {
			continue
		}
		out = append(out, c)
	}
	return out
}

// pipelineYAML matches just the bits of a pipeline config we care about.
type pipelineYAML struct {
	Nodes []struct {
		Name string `yaml:"name"`
		Type string `yaml:"type"`
	} `yaml:"nodes"`
}

func findHTTPIngestNodeNames(content string) []string {
	if content == "" {
		return nil
	}
	var cfg pipelineYAML
	if err := yaml.Unmarshal([]byte(content), &cfg); err != nil {
		return nil
	}
	var names []string
	for _, n := range cfg.Nodes {
		if n.Type == httpIngestionInputType && n.Name != "" {
			names = append(names, n.Name)
		}
	}
	return names
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
