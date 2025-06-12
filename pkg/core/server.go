package core

import (
	"fmt"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// NewServer creates a new Edge Delta MCP server with the specified Edge Delta client and logger.
func NewServer(client Client, version string) *server.MCPServer {
	// Create a new MCP server
	s := server.NewMCPServer("edgedelta-mcp-server", version)

	s.AddTool(LogSearchTool(client))
	s.AddTool(EventsSearchTool(client))
	s.AddTool(AnomalySearchTool(client))
	s.AddTool(PatternStatsTool(client))
	return s
}

// optionalParam is a helper function that can be used to fetch a requested parameter from the request.
// It does the following checks:
// 1. Checks if the parameter is present in the request, if not, it returns its zero-value
// 2. If it is present, it checks if the parameter is of the expected type and returns it
func optionalParam[T any](r mcp.CallToolRequest, p string) (T, error) {
	var zero T

	// Check if the parameter is present in the request
	if _, ok := r.Params.Arguments[p]; !ok {
		return zero, nil
	}

	// Check if the parameter is of the expected type
	if _, ok := r.Params.Arguments[p].(T); !ok {
		return zero, fmt.Errorf("parameter %s is not of type %T, is %T", p, zero, r.Params.Arguments[p])
	}

	return r.Params.Arguments[p].(T), nil
}
