package main

import (
	"context"
	"log"
	"os"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/mcp_server_stdin"
)

func main() {
	apiToken := os.Getenv("ED_API_TOKEN")
	if apiToken == "" {
		log.Fatal("ED_API_TOKEN environment variable not set")
	}

	var opts []mcp_server_stdin.ServerOption
	if apiURL := os.Getenv("ED_API_URL"); apiURL != "" {
		opts = append(opts, mcp_server_stdin.WithAPIURL(apiURL))
	}
	if openAPIDocURL := os.Getenv("ED_OPENAPI_DOC_URL"); openAPIDocURL != "" {
		opts = append(opts, mcp_server_stdin.WithOpenAPIDocURL(openAPIDocURL))
	}

	opts = append(opts, mcp_server_stdin.WithAllowedTags([]string{"AI"}))

	mcpServer, err := mcp_server_stdin.NewEdgeDeltaMCPStdinServer(apiToken, opts...)
	if err != nil {
		log.Fatal(err)
	}

	if err := mcpServer.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
