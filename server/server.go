package server

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/tools"

	"github.com/mark3labs/mcp-go/server"
)

var (
	defaultServerConfig = serverConfig{
		apiURL:         "https://api.edgedelta.com",
		serverName:     "edgedelta-mcp-server",
		serverVersion:  "0.0.1",
		apiTokenHeader: "X-ED-API-Token",
		logger:         slog.Default(),
		// HTTP server options
		port:             8080,
		stateless:        true,
		disableStreaming: true,
	}
)

type Server interface {
	Start(ctx context.Context) error
}

type ServerType string

const (
	StdinServerType ServerType = "stdin"
	HTTPServerType  ServerType = "http"
)

func CreateServer(serverType ServerType, orgID, apiToken string, opts ...ServerOption) (Server, error) {
	switch serverType {
	case StdinServerType:
		return NewStdioServer(orgID, apiToken, opts...)
	case HTTPServerType:
		return NewHTTPServer(opts...)
	default:
		return nil, fmt.Errorf("invalid server type: %s", serverType)
	}
}

func AddCustomTools(s *server.MCPServer, client tools.Client) {
	// Discovery and query building tools
	s.AddTool(tools.GetDiscoverSchemaTool(client))
	s.AddTool(tools.GetSearchMetricsTool(client))
	s.AddTool(tools.GetValidateCQLTool())
	s.AddTool(tools.GetBuildCQLTool(client))

	// Pipeline management tools
	s.AddTool(tools.GetPipelinesTool(client))
	s.AddTool(tools.GetPipelineConfigTool(client))
	s.AddTool(tools.GetPipelineHistoryTool(client))
	s.AddTool(tools.DeployPipelineTool(client))
	s.AddTool(tools.AddPipelineSourceTool(client))

	// Facet tools
	s.AddTool(tools.FacetsTool, tools.FacetsToolHandler(client))
	s.AddTool(tools.FacetOptionsTool, tools.FacetOptionsToolHandler(client))

	// Search tools
	s.AddTool(tools.GetLogSearchTool(client))
	s.AddTool(tools.GetTraceTimelineTool(client))
	s.AddTool(tools.GetMetricSearchTool(client))
	s.AddTool(tools.GetEventSearchTool(client))
	s.AddTool(tools.GetLogPatternsTool(client))

	// Dashboard tools
	s.AddTool(tools.GetAllDashboardsTool(client))
	s.AddTool(tools.GetDashboardTool(client))

	// Graph/visualization tools
	s.AddTool(tools.GetLogGraphTool(client))
	s.AddTool(tools.GetMetricGraphTool(client))
	s.AddTool(tools.GetTraceGraphTool(client))
	s.AddTool(tools.GetPatternGraphTool(client))
}

func AddCustomResources(s *server.MCPServer, client tools.Client) {
	// CQL syntax reference
	s.AddResource(tools.CQLReferenceResource, tools.CQLReferenceResourceHandler())

	// Facet resources
	s.AddResourceTemplate(tools.FacetsResource, tools.FacetsResourceHandler(client))
	s.AddResourceTemplate(tools.FacetOptionsResource, tools.FacetOptionsResourceHandler(client))

	// Data resources
	s.AddResource(tools.ServicesResource, tools.ServicesResourceHandler(client))
	s.AddResource(tools.LogFacetKeysResource, tools.LogFacetKeysResourceHandler(client))
	s.AddResource(tools.MetricFacetKeysResource, tools.MetricFacetKeysResourceHandler(client))
	s.AddResource(tools.TraceFacetKeysResource, tools.TraceFacetKeysResourceHandler(client))
	s.AddResource(tools.PatternFacetKeysResource, tools.PatternFacetKeysResourceHandler(client))
	s.AddResource(tools.EventFacetKeysResource, tools.EventFacetKeysResourceHandler(client))
}

// serverConfig holds internal configuration
type serverConfig struct {
	apiURL         string
	serverName     string
	serverVersion  string
	apiTokenHeader string
	logger         *slog.Logger

	// HTTP server options
	port             int
	stateless        bool
	disableStreaming bool
}

// ServerOption configures the MCP server
type ServerOption func(*serverConfig)

// WithAPIURL sets the API URL
func WithAPIURL(url string) ServerOption {
	return func(c *serverConfig) {
		c.apiURL = url
	}
}

// WithServerName sets the server name
func WithServerName(name string) ServerOption {
	return func(c *serverConfig) {
		c.serverName = name
	}
}

// WithServerVersion sets the server version
func WithServerVersion(version string) ServerOption {
	return func(c *serverConfig) {
		c.serverVersion = version
	}
}

// WithAPITokenHeader sets the API token header name
func WithAPITokenHeader(header string) ServerOption {
	return func(c *serverConfig) {
		c.apiTokenHeader = header
	}
}

func WithLogger(logger *slog.Logger) ServerOption {
	return func(c *serverConfig) {
		c.logger = logger
	}
}
