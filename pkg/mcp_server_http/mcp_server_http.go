package mcp_server_http

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/swagger2mcp"

	"github.com/mark3labs/mcp-go/server"
)

// httpServerConfig holds internal configuration
type httpServerConfig struct {
	apiURL         string
	openAPIDocURL  string
	serverName     string
	serverVersion  string
	port           int
	allowedTags    []string
	apiTokenHeader string
	stateless      bool
}

// HTTPServerOption configures the HTTP MCP server
type HTTPServerOption func(*httpServerConfig)

// WithAPIURL sets the API URL
func WithAPIURL(url string) HTTPServerOption {
	return func(c *httpServerConfig) {
		c.apiURL = url
	}
}

// WithOpenAPIDocURL sets the OpenAPI documentation URL
func WithOpenAPIDocURL(url string) HTTPServerOption {
	return func(c *httpServerConfig) {
		c.openAPIDocURL = url
	}
}

// WithServerName sets the server name
func WithServerName(name string) HTTPServerOption {
	return func(c *httpServerConfig) {
		c.serverName = name
	}
}

// WithServerVersion sets the server version
func WithServerVersion(version string) HTTPServerOption {
	return func(c *httpServerConfig) {
		c.serverVersion = version
	}
}

// WithPort sets the HTTP server port
func WithPort(port int) HTTPServerOption {
	return func(c *httpServerConfig) {
		c.port = port
	}
}

// WithAllowedTags sets the allowed tags for filtering
func WithAllowedTags(tags []string) HTTPServerOption {
	return func(c *httpServerConfig) {
		c.allowedTags = tags
	}
}

// WithAPITokenHeader sets the API token header name
func WithAPITokenHeader(header string) HTTPServerOption {
	return func(c *httpServerConfig) {
		c.apiTokenHeader = header
	}
}

// WithStateless sets whether the server should be stateless
func WithStateless(stateless bool) HTTPServerOption {
	return func(c *httpServerConfig) {
		c.stateless = stateless
	}
}

type authedTransport struct {
	roundTripper   http.RoundTripper
	apiTokenHeader string
}

type APITokenKey string

var apiTokenKey APITokenKey = "apiToken"

func TokenKeyFromContext(ctx context.Context) (string, bool) {
	value := ctx.Value(apiTokenKey)
	if value == nil {
		return "", false
	}

	token, ok := value.(string)
	if !ok {
		return "", false
	}

	return token, true
}

func SetTokenInContext(ctx context.Context, apiToken string) context.Context {
	return context.WithValue(ctx, apiTokenKey, apiToken)
}

func (t *authedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if token, ok := TokenKeyFromContext(req.Context()); ok {
		req.Header.Set(t.apiTokenHeader, token)
	}
	return t.roundTripper.RoundTrip(req)
}

// MCPHTTPServer wraps the HTTP server and its dependencies
type MCPHTTPServer struct {
	httpServer *server.StreamableHTTPServer
	config     *httpServerConfig
}

// NewEdgeDeltaMCPHTTPServer creates a new Edge Delta MCP HTTP server
func NewEdgeDeltaMCPHTTPServer(opts ...HTTPServerOption) (*MCPHTTPServer, error) {
	// Set defaults
	config := &httpServerConfig{
		apiURL:         "https://api.staging.edgedelta.com",
		openAPIDocURL:  "https://api.staging.edgedelta.com/swagger/doc.json",
		serverName:     "edgedelta-mcp-server",
		serverVersion:  "0.0.1",
		port:           8080,
		allowedTags:    []string{"AI"},
		apiTokenHeader: "X-ED-API-Token",
		stateless:      true,
	}

	// Apply options
	for _, opt := range opts {
		opt(config)
	}

	httpClient := &http.Client{
		Transport: &authedTransport{
			roundTripper:   http.DefaultTransport,
			apiTokenHeader: config.apiTokenHeader,
		},
	}

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
		config:     config,
	}, nil
}

// Start starts the HTTP server and blocks until shutdown
func (m *MCPHTTPServer) Start() error {
	addr := fmt.Sprintf(":%d", m.config.port)
	log.Printf("Starting MCP server on %s", addr)
	return m.httpServer.Start(addr)
}

// Port returns the configured port
func (m *MCPHTTPServer) Port() int {
	return m.config.port
}
