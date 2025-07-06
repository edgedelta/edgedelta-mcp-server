package main

import (
	"context"
	"fmt"
	"github.com/edgedelta/edgedelta-mcp-server/pkg/openapi2mcp"
	"log"
	"net/http"

	"github.com/mark3labs/mcp-go/server"
)

const (
	edAPITokenHeader = "X-ED-API-Token"
	edgeDeltaAPIURL  = "https://api.edgedelta.com"
	openAPIDocURL    = "https://api.edgedelta.com/swagger/doc.json"
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
	httpClient := &http.Client{
		Transport: &authedTransport{http.DefaultTransport},
	}
	allowedTags := []string{"AI"}

	toolToHandlers, err := openapi2mcp.NewToolsFromURL(openAPIDocURL, edgeDeltaAPIURL, httpClient, openapi2mcp.WithAllowedTags(allowedTags))

	//specBytes, err := os.ReadFile("swagger.json")
	//if err != nil {
	//	log.Fatalf("failed to read swagger.json: %v", err)
	//}
	//
	//var spec openapi2mcp.OpenAPISpec
	//if err := json.Unmarshal(specBytes, &spec); err != nil {
	//	log.Fatalf("failed to unmarshal swagger.json: %v", err)
	//}
	//
	//toolToHandlers, err := openapi2mcp.NewToolsFromSpec(edgeDeltaAPIURL, &spec, httpClient, openapi2mcp.WithAllowedTags(allowedTags))
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
