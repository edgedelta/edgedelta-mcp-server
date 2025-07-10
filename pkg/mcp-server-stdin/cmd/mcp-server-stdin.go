package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/swagger2mcp"

	"github.com/mark3labs/mcp-go/server"
)

// serverConfig holds internal configuration
type serverConfig struct {
	apiURL         string
	openAPIDocURL  string
	serverName     string
	serverVersion  string
	allowedTags    []string
	apiTokenHeader string
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

// MCPServer wraps the server and its dependencies
type MCPServer struct {
	server      *server.MCPServer
	stdioServer *server.StdioServer
	config      *serverConfig
}

// NewEdgeDeltaMCPStdinServer creates a new Edge Delta MCP server for stdin/stdout
func NewEdgeDeltaMCPStdinServer(apiToken string, opts ...ServerOption) (*MCPServer, error) {
	if apiToken == "" {
		return nil, fmt.Errorf("API token is required")
	}

	// Set defaults
	config := &serverConfig{
		apiURL:         "https://api.staging.edgedelta.com",
		openAPIDocURL:  "https://api.staging.edgedelta.com/swagger/doc.json",
		serverName:     "edgedelta-mcp-server",
		serverVersion:  "0.0.1",
		allowedTags:    []string{"AI"},
		apiTokenHeader: "X-ED-API-Token",
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

	stdioServer := server.NewStdioServer(s)
	stdioServer.SetContextFunc(func(ctx context.Context) context.Context {
		return SetTokenInContext(ctx, apiToken)
	})

	return &MCPServer{
		server:      s,
		stdioServer: stdioServer,
		config:      config,
	}, nil
}

// Run starts the MCP server and blocks until shutdown
func (m *MCPServer) Run(ctx context.Context) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errC := make(chan error, 1)
	go func() {
		in, out := io.Reader(os.Stdin), io.Writer(os.Stdout)
		errC <- m.stdioServer.Listen(ctx, in, out)
	}()

	fmt.Fprintf(os.Stderr, "Edge Delta MCP Server running on stdio\n")

	select {
	case <-ctx.Done():
		fmt.Println("Shutting down...")
		return nil
	case err := <-errC:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	}
}
