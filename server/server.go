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
		openAPIDocURL:  "https://api.edgedelta.com/swagger/doc.json",
		serverName:     "edgedelta-mcp-server",
		serverVersion:  "0.0.1",
		allowedTags:    []string{"AI"},
		apiTokenHeader: "X-ED-API-Token",
		logger:         slog.Default(),
		// HTTP server options
		port:      8080,
		stateless: true,
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

func CreateServer(serverType ServerType, apiToken string, opts ...ServerOption) (Server, error) {
	switch serverType {
	case StdinServerType:
		return NewStdinServer(apiToken, opts...)
	case HTTPServerType:
		return NewHTTPServer(opts...)
	default:
		return nil, fmt.Errorf("invalid server type: %s", serverType)
	}
}

func AddCustomTools(s *server.MCPServer) {
	client := tools.NewHTTPlient()
	s.AddTool(tools.GetPipelinesTool(client))
}

// serverConfig holds internal configuration
type serverConfig struct {
	apiURL         string
	openAPIDocURL  string
	serverName     string
	serverVersion  string
	allowedTags    []string
	apiTokenHeader string
	logger         *slog.Logger

	// HTTP server options
	port      int
	stateless bool
}

// ServerOption configures the MCP server
type ServerOption func(*serverConfig)

// WithAPIURL sets the API URL
func WithAPIURL(url string) ServerOption {
	return func(c *serverConfig) {
		c.apiURL = url
	}
}

// WithOpenAPIDocURL sets the OpenAPI documentation URL
func WithOpenAPIDocURL(url string) ServerOption {
	return func(c *serverConfig) {
		c.openAPIDocURL = url
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

// WithAllowedTags sets the allowed tags for filtering
func WithAllowedTags(tags []string) ServerOption {
	return func(c *serverConfig) {
		c.allowedTags = tags
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
