package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/swagger2mcp"

	"github.com/mark3labs/mcp-go/server"
)

const (
	edAPITokenHeader = "X-ED-API-Token"
	mcpServerName    = "edgedelta-mcp-server"
	mcpServerVersion = "0.0.1"
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

func (t *authedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if token, ok := TokenKeyFromContext(req.Context()); ok {
		req.Header.Set(edAPITokenHeader, token)
	}
	return t.roundTripper.RoundTrip(req)
}

func main() {
	apiToken := os.Getenv("ED_API_TOKEN")
	if apiToken == "" {
		log.Fatal("ED_API_TOKEN environment variable not set")
	}

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

	stdioServer := server.NewStdioServer(s)
	stdioServer.SetContextFunc(func(ctx context.Context) context.Context {
		ctx = SetTokenInContext(ctx, apiToken)
		return ctx
	})

	// Start listening for messages
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errC := make(chan error, 1)
	go func() {
		in, out := io.Reader(os.Stdin), io.Writer(os.Stdout)
		errC <- stdioServer.Listen(ctx, in, out)
	}()

	// Output edgedelta-mcp-server string
	_, _ = fmt.Fprintf(os.Stderr, "Edge Delta MCP Server running on stdio\n")

	// Wait for shutdown signal
	select {
	case <-ctx.Done():
		fmt.Println("Shutting down...")
	case err := <-errC:
		if err != nil {
			fmt.Printf("Error starting server: %v\n", err)
		}
	}
}
