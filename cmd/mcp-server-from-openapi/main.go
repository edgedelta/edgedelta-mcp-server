package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/openapi2mcp"

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

type TokenKey string

var (
	tokenKey TokenKey = "token"
)

func TokenKeyFromContext(ctx context.Context) (string, bool) {
	value := ctx.Value(tokenKey)
	if value == nil {
		return "", false
	}

	token, ok := value.(string)
	if !ok {
		return "", false
	}

	return token, true
}

func SetTokenInContext(ctx context.Context, token string) context.Context {
	return context.WithValue(ctx, tokenKey, token)
}

// tokenMiddleware extracts the token from the request header and adds it to the context
func tokenMiddleware(ctx context.Context, r *http.Request) context.Context {
	token := r.Header.Get(edAPITokenHeader)
	if token != "" {
		return SetTokenInContext(ctx, token)
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
	if err != nil {
		log.Fatal(err)
	}

	s := server.NewMCPServer(mcpServerName, mcpServerVersion)

	for _, toolToHandler := range toolToHandlers {
		s.AddTool(toolToHandler.Tool, toolToHandler.Handler)
	}

	httpServer := server.NewStreamableHTTPServer(s, server.WithHTTPContextFunc(tokenMiddleware))
	if err := httpServer.Start(fmt.Sprintf(":%d", mcpServerPort)); err != nil {
		log.Fatal(err)
	}
}
