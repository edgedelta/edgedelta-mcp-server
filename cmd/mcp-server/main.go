package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/tools"
	"github.com/edgedelta/edgedelta-mcp-server/server"

	"github.com/go-openapi/spec"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	stdlog "log"
)

const (
	openAPIDocURL = "https://api.edgedelta.com/swagger/doc.json"
)

var (
	version = "version"
	commit  = "commit"
	date    = "date"
)

var (
	rootCmd = &cobra.Command{
		Use:     "server",
		Short:   "Edge Delta MCP Server",
		Long:    `A Edge Delta MCP server that handles various tools and resources.`,
		Version: fmt.Sprintf("%s (%s) %s", version, commit, date),
	}

	stdioCmd = &cobra.Command{
		Use:   "stdio",
		Short: "Start stdio server",
		Long:  `Start a server that communicates via standard input/output streams using JSON-RPC messages.`,
		Run: func(_ *cobra.Command, _ []string) {
			logFile := viper.GetString("log-file")
			logger, err := initLogger(logFile)
			if err != nil {
				stdlog.Fatal("Failed to initialize logger:", err)
			}
			cfg := runConfig{
				logger:     logger,
				serverType: server.StdinServerType,
			}

			if err := runServer(cfg); err != nil {
				stdlog.Fatal("failed to run stdio server:", err)
			}
		},
	}

	httpCmd = &cobra.Command{
		Use:   "http",
		Short: "Start http server",
		Long:  `Start a server that communicates via http using JSON-RPC messages.`,
		Run: func(_ *cobra.Command, _ []string) {
			logFile := viper.GetString("log-file")
			logger, err := initLogger(logFile)
			if err != nil {
				stdlog.Fatal("Failed to initialize logger:", err)
			}
			cfg := runConfig{
				logger:     logger,
				serverType: server.HTTPServerType,
			}

			if err := runServer(cfg); err != nil {
				stdlog.Fatal("failed to run http server:", err)
			}
		},
	}
)

func initLogger(outPath string) (*slog.Logger, error) {
	if outPath == "" {
		return slog.Default(), nil
	}

	file, err := os.OpenFile(outPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}

	logger := slog.New(slog.NewJSONHandler(file, &slog.HandlerOptions{
		Level: slog.LevelDebug,
	}))

	return logger, nil
}

func init() {
	// Add global flags that will be shared by all commands
	rootCmd.PersistentFlags().String("log-file", "", "Path to log file")

	// Bind flags to viper
	_ = viper.BindPFlag("log-file", rootCmd.PersistentFlags().Lookup("log-file"))

	// Add subcommands
	rootCmd.AddCommand(stdioCmd)
	rootCmd.AddCommand(httpCmd)
}

type runConfig struct {
	logger     *slog.Logger
	serverType server.ServerType
}

func runServer(cfg runConfig) error {
	var opts []server.ServerOption

	if apiURL := os.Getenv("ED_API_URL"); apiURL != "" {
		opts = append(opts, server.WithAPIURL(apiURL))
	}

	if portStr := os.Getenv("ED_MCP_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			opts = append(opts, server.WithPort(port))
		}
	}

	opts = append(opts, server.WithLogger(cfg.logger))

	apiToken := os.Getenv("ED_API_TOKEN")
	orgID := os.Getenv("ED_ORG_ID")

	spec, err := fetchOpenAPISpec()
	if err != nil {
		return fmt.Errorf("failed to fetch openapi spec, err: %w", err)
	}

	mcpServer, err := server.CreateServer(cfg.serverType, orgID, apiToken, spec, opts...)
	if err != nil {
		return fmt.Errorf("failed to create server, err: %w", err)
	}

	cfg.logger.Info("Starting EdgeDelta MCP Server")

	if err := mcpServer.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start server, err: %w", err)
	}

	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func fetchOpenAPISpec() (*spec.Swagger, error) {
	cl := tools.NewHTTPClient("")

	resp, err := cl.Get(openAPIDocURL)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch openapi spec, err: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected response status code: %d when fetching openapi spec", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body, err: %w", err)
	}

	swaggerSpec := &spec.Swagger{}
	if err := json.Unmarshal(data, swaggerSpec); err != nil {
		return nil, fmt.Errorf("failed to parse swagger json, err: %w", err)
	}

	if err := spec.ExpandSpec(swaggerSpec, &spec.ExpandOptions{
		RelativeBase: "",
	}); err != nil {
		return nil, fmt.Errorf("failed to expand spec, err: %w", err)
	}

	return swaggerSpec, nil
}
