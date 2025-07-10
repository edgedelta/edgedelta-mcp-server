package main

import (
	"log"
	"os"
	"strconv"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/mcp-server-http/cmd"
)

func main() {
	var opts []cmd.HTTPServerOption

	if apiURL := os.Getenv("ED_API_URL"); apiURL != "" {
		opts = append(opts, cmd.WithAPIURL(apiURL))
	}
	if openAPIDocURL := os.Getenv("ED_OPENAPI_DOC_URL"); openAPIDocURL != "" {
		opts = append(opts, cmd.WithOpenAPIDocURL(openAPIDocURL))
	}
	if portStr := os.Getenv("ED_MCP_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			opts = append(opts, cmd.WithPort(port))
		}
	}

	mcpServer, err := cmd.NewEdgeDeltaMCPHTTPServer(opts...)
	if err != nil {
		log.Fatal(err)
	}

	if err := mcpServer.Start(); err != nil {
		log.Fatal(err)
	}
}
