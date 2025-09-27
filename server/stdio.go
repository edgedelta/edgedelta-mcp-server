package server

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/tools"

	"github.com/mark3labs/mcp-go/server"
)

// MCPServer wraps the server and its dependencies
type MCPServer struct {
	server      *server.MCPServer
	stdioServer *server.StdioServer
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

	s := server.NewMCPServer(config.serverName, config.serverVersion)

	AddCustomTools(s, httpClient)
	AddCustomResources(s, httpClient)

	stdioServer := server.NewStdioServer(s)
	stdioServer.SetContextFunc(func(ctx context.Context) context.Context {
		ctx = context.WithValue(ctx, tools.OrgIDKey, orgID)
		ctx = context.WithValue(ctx, tools.TokenKey, apiToken)
		return ctx
	})

	return &MCPServer{
		server:      s,
		stdioServer: stdioServer,
		config:      &config,
	}, nil
}

// Run starts the MCP server and blocks until shutdown
func (m *MCPServer) Start(ctx context.Context) error {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	errC := make(chan error, 1)
	go func() {
		in, out := io.Reader(os.Stdin), io.Writer(os.Stdout)
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
