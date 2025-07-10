package main

import (
	"context"
	"log"
	"os"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/mcp-server-stdin/cmd"
)

func main() {
	apiToken := os.Getenv("ED_API_TOKEN")
	if apiToken == "" {
		log.Fatal("ED_API_TOKEN environment variable not set")
	}

	var opts []cmd.ServerOption
	if apiURL := os.Getenv("ED_API_URL"); apiURL != "" {
		opts = append(opts, cmd.WithAPIURL(apiURL))
	}
	if openAPIDocURL := os.Getenv("ED_OPENAPI_DOC_URL"); openAPIDocURL != "" {
		opts = append(opts, cmd.WithOpenAPIDocURL(openAPIDocURL))
	}

	opts = append(opts, cmd.WithAllowedTags([]string{"AI"}))

	mcpServer, err := cmd.NewEdgeDeltaMCPStdinServer(apiToken, opts...)
	if err != nil {
		log.Fatal(err)
	}

	if err := mcpServer.Run(context.Background()); err != nil {
		log.Fatal(err)
	}
}
