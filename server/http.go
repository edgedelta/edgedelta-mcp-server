package server

import (
	"context"
	"fmt"
	"net/http"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/swagger2mcp"
	"github.com/edgedelta/edgedelta-mcp-server/pkg/tools"

	"github.com/mark3labs/mcp-go/server"
)

// WithPort sets the HTTP server port
func WithPort(port int) ServerOption {
	return func(c *serverConfig) {
		c.port = port
	}
}

// WithStateless sets whether the server should be stateless
func WithStateless(stateless bool) ServerOption {
	return func(c *serverConfig) {
		c.stateless = stateless
	}
}

// MCPHTTPServer wraps the HTTP server and its dependencies
type MCPHTTPServer struct {
	httpServer *server.StreamableHTTPServer
	config     *serverConfig
}

// New creates a new Edge Delta MCP HTTP server
func NewHTTPServer(opts ...ServerOption) (Server, error) {
	// Set defaults
	config := defaultServerConfig

	// Apply options
	for _, opt := range opts {
		opt(&config)
	}

	httpClient := tools.NewHTTPClient(config.apiTokenHeader)

	toolToHandlers, err := swagger2mcp.NewToolsFromURL(
		config.openAPIDocURL,
		config.apiURL,
		httpClient,
		swagger2mcp.WithAllowedTags(config.allowedTags),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create tools from URL: %w", err)
	}

	s := server.NewMCPServer(config.serverName, config.serverVersion)

	for _, toolToHandler := range toolToHandlers {
		s.AddTool(toolToHandler.Tool, toolToHandler.Handler)
	}
	AddCustomTools(s, httpClient)
	AddCustomResources(s, httpClient)

	// Create auth middleware that uses the configured header
	authMiddleware := func(ctx context.Context, r *http.Request) context.Context {
		apiToken := r.Header.Get(config.apiTokenHeader)
		if apiToken != "" {
			return SetTokenInContext(ctx, apiToken)
		}
		return ctx
	}

	httpServer := server.NewStreamableHTTPServer(
		s,
		server.WithHTTPContextFunc(authMiddleware),
		server.WithStateLess(config.stateless),
	)

	return &MCPHTTPServer{
		httpServer: httpServer,
		config:     &config,
	}, nil
}

// Start starts the HTTP server and blocks until shutdown
func (m *MCPHTTPServer) Start(_ context.Context) error {
	addr := fmt.Sprintf(":%d", m.config.port)
	m.config.logger.Info("Starting MCP server", "addr", addr)
	return m.httpServer.Start(addr)
}

// Port returns the configured port
func (m *MCPHTTPServer) Port() int {
	return m.config.port
}

func SetTokenInContext(ctx context.Context, apiToken string) context.Context {
	return context.WithValue(ctx, tools.APITokenKey, apiToken)
}
