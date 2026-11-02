package server

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/events"
	"github.com/edgedelta/edgedelta-mcp-server/pkg/tools"

	"github.com/mark3labs/mcp-go/server"
)

// MCPServer wraps the server and its dependencies
type MCPServer struct {
	server      *server.MCPServer
	stdioServer *server.StdioServer
	events      *events.Handler
	orgID       string
	apiToken    string
	config      *serverConfig
}

// NewStdioServer creates a new Edge Delta MCP server for stdin/stdout
func NewStdioServer(orgID, apiToken string, opts ...ServerOption) (*MCPServer, error) {
	if orgID == "" {
		return nil, fmt.Errorf("ED_ORG_ID not set")
	}
	if apiToken == "" {
		return nil, fmt.Errorf("ED_API_TOKEN not set")
	}

	// Set defaults
	config := defaultServerConfig

	// Apply options
	for _, opt := range opts {
		opt(&config)
	}

	httpClient := tools.NewHTTPClient(config.apiURL, config.apiTokenHeader)

	s := newMCPServer(config)

	AddCustomTools(s, httpClient)
	AddCustomResources(s, httpClient)

	m := &MCPServer{
		server:   s,
		events:   config.eventsHandler(httpClient),
		orgID:    orgID,
		apiToken: apiToken,
		config:   &config,
	}
	m.stdioServer = server.NewStdioServer(s)
	m.stdioServer.SetContextFunc(m.withAuth)
	return m, nil
}

func (m *MCPServer) withAuth(ctx context.Context) context.Context {
	ctx = context.WithValue(ctx, tools.OrgIDKey, m.orgID)
	return context.WithValue(ctx, tools.EDTokenKey, m.apiToken)
}

// Run starts the MCP server and blocks until shutdown
func (m *MCPServer) Start(ctx context.Context) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errC := make(chan error, 2)
	in, next := io.Pipe()
	out := &lineWriter{w: os.Stdout}
	go func() {
		if err := filterEvents(m.withAuth(ctx), m.events, os.Stdin, out, next); err != nil {
			errC <- err
		}
	}()
	go func() {
		errC <- m.stdioServer.Listen(ctx, in, out)
	}()

	m.config.logger.Info("Edge Delta MCP Server running on stdio")

	select {
	case <-ctx.Done():
		m.config.logger.Info("Shutting down...")
		return nil
	case err := <-errC:
		if err != nil {
			return fmt.Errorf("server error: %w", err)
		}
		return nil
	}
}
