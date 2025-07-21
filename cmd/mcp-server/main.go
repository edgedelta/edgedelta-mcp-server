package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strconv"

	"github.com/edgedelta/edgedelta-mcp-server/server"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	stdlog "log"
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

	if openAPIDocURL := os.Getenv("ED_OPENAPI_DOC_URL"); openAPIDocURL != "" {
		opts = append(opts, server.WithOpenAPIDocURL(openAPIDocURL))
	}

	if portStr := os.Getenv("ED_MCP_PORT"); portStr != "" {
		if port, err := strconv.Atoi(portStr); err == nil {
			opts = append(opts, server.WithPort(port))
		}
	}

	opts = append(opts, server.WithLogger(cfg.logger))

	apiToken := os.Getenv("ED_API_TOKEN")
	mcpServer, err := server.CreateServer(cfg.serverType, apiToken, opts...)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	cfg.logger.Info("Starting EdgeDelta MCP Server")

	if err := mcpServer.Start(context.Background()); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
