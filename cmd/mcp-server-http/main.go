package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/swagger2mcp"

	"github.com/mark3labs/mcp-go/server"
)

const (
	edAPITokenHeader = "X-ED-API-Token"
	mcpServerName    = "edgedelta-mcp-server"
	mcpServerVersion = "0.0.1"
	mcpServerPort    = 8080
)

type authedTransport struct {
	roundTripper http.RoundTripper
}

type APITokenKey string

var (
	apiTokenKey APITokenKey = "apiToken"
)

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

// authMiddleware extracts the API token from the request header and adds it to the context
func authMiddleware(ctx context.Context, r *http.Request) context.Context {
	apiToken := r.Header.Get(edAPITokenHeader)
	if apiToken != "" {
		return SetTokenInContext(ctx, apiToken)
	}
	return ctx
}

func (t *authedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if token, ok := TokenKeyFromContext(req.Context()); ok {
		req.Header.Set(edAPITokenHeader, token)
	}
	return t.roundTripper.RoundTrip(req)
}

func main() {
	apiURL := os.Getenv("ED_API_URL")
	if apiURL == "" {
		apiURL = "https://api.staging.edgedelta.com"
	}

	openAPIDocURL := os.Getenv("ED_OPENAPI_DOC_URL")
	if openAPIDocURL == "" {
		openAPIDocURL = "https://api.staging.edgedelta.com/swagger/doc.json"
	}

	httpClient := &http.Client{
		Transport: &authedTransport{http.DefaultTransport},
	}
	allowedTags := []string{"AI"}

	toolToHandlers, err := swagger2mcp.NewToolsFromURL(openAPIDocURL, apiURL, httpClient, swagger2mcp.WithAllowedTags(allowedTags))

	if err != nil {
		log.Fatal(err)
	}

	s := server.NewMCPServer(mcpServerName, mcpServerVersion)

	for _, toolToHandler := range toolToHandlers {
		s.AddTool(toolToHandler.Tool, toolToHandler.Handler)
	}

	log.Printf("Starting MCP server on :%d", mcpServerPort)
	httpServer := server.NewStreamableHTTPServer(s, server.WithHTTPContextFunc(authMiddleware), server.WithStateLess(true))
	if err := httpServer.Start(fmt.Sprintf(":%d", mcpServerPort)); err != nil {
		log.Fatal(err)
	}
}
