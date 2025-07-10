package main

import (
	"log"
	"os"
	"strconv"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/mcp_server_http"
)

func main() {
	var opts []mcp_server_http.HTTPServerOption

	if apiURL := os.Getenv("ED_API_URL"); apiURL != "" {
		opts = append(opts, mcp_server_http.WithAPIURL(apiURL))
	}
	if openAPIDocURL := os.Getenv("ED_OPENAPI_DOC_URL"); openAPIDocURL != "" {
		opts = append(opts, mcp_server_http.WithOpenAPIDocURL(openAPIDocURL))
	}
	if portStr := os.Getenv("ED_MCP_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			opts = append(opts, mcp_server_http.WithPort(port))
		}
	}

	mcpServer, err := mcp_server_http.NewEdgeDeltaMCPHTTPServer(opts...)
	if err != nil {
		log.Fatal(err)
	}

	if err := mcpServer.Start(); err != nil {
		log.Fatal(err)
	}
}
