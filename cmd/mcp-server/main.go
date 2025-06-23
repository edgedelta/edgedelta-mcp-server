package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"log/slog"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/tools"
	"github.com/mark3labs/mcp-go/server"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	stdlog "log"
)

var version = "version"
var commit = "commit"
var date = "date"

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
				logger: logger,
			}
			if err := runStdioServer(cfg); err != nil {
				stdlog.Fatal("failed to run stdio server:", err)
			}
		},
	}

	allowedTags = []string{"AI"}
	// local swagger doc url
	swaggerDocURL = "http://localhost:4445/swagger_internal/doc.json"
	// swaggerDocURL = "https://api2.edgedelta.com/swagger/doc.json"
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
}

type runConfig struct {
	logger *slog.Logger
}

func runStdioServer(cfg runConfig) error {
	// Create app context
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Create Edge Delta client
	orgID := os.Getenv("ED_ORG_ID")
	if orgID == "" {
		return fmt.Errorf("ED_ORG_ID not set")
	}

	token := os.Getenv("ED_API_TOKEN")
	if token == "" {
		return fmt.Errorf("ED_API_TOKEN not set")
	}

	apiURL := os.Getenv("ED_API_URL")
	if apiURL == "" {
		apiURL = "https://api.edgedelta.com"
	}

	// Create auto-sync OpenAPI server with AI tag filtering
	cfg.logger.Info("Starting EdgeDelta MCP Server with derived from swagger doc, %s")

	edServer, err := tools.CreateServer(version, swaggerDocURL, apiURL, allowedTags)
	if err != nil {
		return fmt.Errorf("failed to create server: %w", err)
	}

	stdioServer := server.NewStdioServer(edServer)
	stdioServer.SetContextFunc(func(ctx context.Context) context.Context {
		ctx = context.WithValue(ctx, tools.OrgIDKey, orgID)
		ctx = context.WithValue(ctx, tools.TokenKey, token)
		ctx = context.WithValue(ctx, tools.APIURLKey, apiURL)
		return ctx
	})

	// Start listening for messages
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
		cfg.logger.Info("shutting down server...")
	case err := <-errC:
		if err != nil {
			return fmt.Errorf("error running server: %w", err)
		}
	}

	return nil
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
