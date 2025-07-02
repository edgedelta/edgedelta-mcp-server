package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

var (
	ErrNoOpenAPISpec = errors.New("no OpenAPI spec loaded")
	aiExclusionTag   = "AI"
	snakeCaseRegex   = regexp.MustCompile("([a-z0-9])([A-Z])")
)

const refPrefix = "#/definitions/"

// OpenAPISpec represents the OpenAPI specification structure
type OpenAPISpec struct {
	Swagger     string                          `json:"swagger"`
	Info        OpenAPIInfo                     `json:"info"`
	Host        string                          `json:"host"`
	Schemes     []string                        `json:"schemes"`
	Paths       map[string]map[string]Operation `json:"paths"`
	Definitions map[string]Definition           `json:"definitions"`
}

type OpenAPIInfo struct {
	Title       string `json:"title"`
	Version     string `json:"version"`
	Description string `json:"description"`
}

type Operation struct {
	OperationID string                `json:"operationId"`
	Summary     string                `json:"summary"`
	Description string                `json:"description"`
	Tags        []string              `json:"tags"`
	Parameters  []Parameter           `json:"parameters"`
	Security    []map[string][]string `json:"security"`
}

type Parameter struct {
	Name        string       `json:"name"`
	In          string       `json:"in"`
	Type        string       `json:"type"`
	Required    bool         `json:"required"`
	Description string       `json:"description"`
	Schema      *ParamSchema `json:"schema,omitempty"`
}

type ParamSchema struct {
	Type        string   `json:"type"`
	Enum        []string `json:"enum,omitempty"`
	Description string   `json:"description,omitempty"`
	Ref         string   `json:"$ref,omitempty"`
}

type Definition struct {
	Type       string                 `json:"type"`
	Properties map[string]ParamSchema `json:"properties"`
}

type httpClient interface {
	Do(req *http.Request) (*http.Response, error)
	Get(url string) (*http.Response, error)
}

// Server manages auto-syncing OpenAPI-based MCP tools
type Server struct {
	httpClient    httpClient
	client        Client
	swaggerDocURL string
	apiURL        string
	allowedTags   map[string]struct{}
	spec          *OpenAPISpec
	tools         []mcp.Tool
	handlers      []server.ToolHandlerFunc
}

// newServer creates a new auto-syncing OpenAPI server
func newServer(swaggerDocURL, apiURL string, allowedTags []string) *Server {
	tagMap := make(map[string]struct{})
	for _, tag := range allowedTags {
		tagMap[tag] = struct{}{}
	}

	httpClient := NewHTTPlient()
	return &Server{
		swaggerDocURL: swaggerDocURL,
		allowedTags:   tagMap,
		apiURL:        apiURL,
		httpClient:    httpClient,
		client:        httpClient,
	}
}

// LoadSpec loads the OpenAPI specification from local file or URL
func (s *Server) LoadSpec() error {
	data, err := s.retrieveOpenAPISpec(s.swaggerDocURL)
	if err != nil {
		return fmt.Errorf("failed to fetch swagger from URL, err: %w", err)
	}

	var spec OpenAPISpec
	if err := json.Unmarshal(data, &spec); err != nil {
		return fmt.Errorf("failed to parse swagger JSON, err: %w", err)
	}
	s.spec = &spec

	return s.generateTools()
}

// retrieveOpenAPISpec fetches the OpenAPI specification from a URL
func (s *Server) retrieveOpenAPISpec(url string) ([]byte, error) {
	resp, err := s.httpClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL, err: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected response status code: %d when fetching URL", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body, err: %w", err)
	}

	return data, nil
}

// generateTools creates MCP tools from the OpenAPI specification
func (s *Server) generateTools() error {
	if s.spec == nil {
		return ErrNoOpenAPISpec
	}

	var tools []mcp.Tool
	var handlers []server.ToolHandlerFunc

	for path, methods := range s.spec.Paths {
		for method, operation := range methods {
			// Skip if no allowed tags match
			if !s.hasAllowedTag(operation.Tags) {
				continue
			}

			tool, handler := s.createToolFromOperation(path, method, operation, s.spec.Definitions)
			if tool.Name != "" {
				tools = append(tools, tool)
				handlers = append(handlers, handler)
			}
		}
	}

	s.tools = tools
	s.handlers = handlers

	return nil
}

// hasAllowedTag checks if operation has any allowed tags
func (s *Server) hasAllowedTag(tags []string) bool {
	for _, tag := range tags {
		if _, ok := s.allowedTags[tag]; ok {
			return true
		}
	}
	return false
}

// createToolFromOperation creates an MCP tool from an OpenAPI operation
func (s *Server) createToolFromOperation(path, method string, operation Operation, definitions map[string]Definition) (mcp.Tool, server.ToolHandlerFunc) {
	toolName := s.generateToolName(path, method, operation)
	description := getDescription(path, method, operation)

	toolOptions := []mcp.ToolOption{mcp.WithDescription(description)}
	for _, param := range operation.Parameters {
		s.addParameterToTool(&toolOptions, param, definitions)
	}
	tool := mcp.NewTool(toolName, toolOptions...)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return s.executeOperation(ctx, request, path, method, operation)
	}

	return tool, handler
}

// generateToolName creates a tool name from an operation.
func (s *Server) generateToolName(path, method string, operation Operation) string {
	for _, tag := range operation.Tags {
		// return default tag as tool name, AI is used for filtering
		if !strings.EqualFold(tag, aiExclusionTag) {
			return s.toSnakeCase(tag)
		}
	}

	if operation.OperationID != "" {
		return s.toSnakeCase(operation.OperationID)
	}

	// Generate from path and method
	cleanPath := strings.ReplaceAll(path, "/", "_")
	cleanPath = strings.ReplaceAll(cleanPath, "{", "")
	cleanPath = strings.ReplaceAll(cleanPath, "}", "")
	cleanPath = strings.Trim(cleanPath, "_")

	return fmt.Sprintf("%s_%s", strings.ToLower(method), s.toSnakeCase(cleanPath))
}

// toSnakeCase converts camelCase to snake_case
func (s *Server) toSnakeCase(str string) string {
	// Replace spaces with underscores first
	str = strings.ReplaceAll(str, " ", "_")

	// Insert underscore before uppercase letters
	snake := snakeCaseRegex.ReplaceAllString(str, "${1}_${2}")
	return strings.ToLower(snake)
}

// addParameterToTool adds a parameter to the tool options
func (s *Server) addParameterToTool(toolOptions *[]mcp.ToolOption, param Parameter, definitions map[string]Definition) {
	// Skip org_id parameter since it's auto-injected from context
	if strings.EqualFold(param.Name, "org_id") {
		return
	}

	// Handle body parameters
	if param.In == "body" {
		*toolOptions = append(*toolOptions, mcp.WithString(param.Name, withBodyParam(param, definitions)))
		return
	}

	// Get parameter type
	paramType := param.Type
	if paramType == "" && param.Schema != nil {
		paramType = param.Schema.Type
	}

	// Add parameter based on type
	switch paramType {
	case "string":
		*toolOptions = append(*toolOptions, mcp.WithString(param.Name, withParam(param)))
	case "integer", "number":
		*toolOptions = append(*toolOptions, mcp.WithNumber(param.Name, withParam(param)))
	case "boolean":
		*toolOptions = append(*toolOptions, mcp.WithBoolean(param.Name, withParam(param)))
	default:
		// Default to string for unknown types
		*toolOptions = append(*toolOptions, mcp.WithString(param.Name, withParam(param)))
	}
}

// executeOperation executes an API operation
func (s *Server) executeOperation(ctx context.Context, request mcp.CallToolRequest, path, method string, operation Operation) (*mcp.CallToolResult, error) {
	// Type assert the arguments
	args, ok := request.Params.Arguments.(map[string]any)
	if !ok {
		return mcp.NewToolResultError("invalid arguments format"), nil
	}

	// Auto-inject orgID if the path contains {org_id} and orgID is available in context
	if strings.Contains(path, "{org_id}") {
		if orgID := ctx.Value(OrgIDKey); orgID != nil {
			args["org_id"] = orgID
		}
	}

	// Build the full URL
	fullURL := s.buildURL(path, args)

	// Check for body parameters and prepare request body
	var requestBody io.Reader
	var bodyParam *Parameter
	for _, param := range operation.Parameters {
		if param.In == "body" {
			bodyParam = &param
			break
		}
	}

	if bodyParam != nil {
		// Get the JSON payload from arguments
		if bodyData, exists := args[bodyParam.Name]; exists {
			if bodyStr, ok := bodyData.(string); ok && bodyStr != "" {
				requestBody = strings.NewReader(bodyStr)
			}
		}
	}

	// Create HTTP request
	req, err := http.NewRequestWithContext(ctx, strings.ToUpper(method), fullURL, requestBody)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to create request: %v", err)), nil
	}

	// Set Content-Type header for body requests
	if bodyParam != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	// Add authentication headers
	s.addAuthHeaders(req, ctx)

	// Add query parameters (skip body parameters)
	s.addQueryParameters(req, operation.Parameters, request)

	// Execute request
	resp, err := s.httpClient.Do(req)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to execute request: %v", err)), nil
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return mcp.NewToolResultError(fmt.Sprintf("failed to read response: %v", err)), nil
	}

	if resp.StatusCode >= 400 {
		return mcp.NewToolResultError(fmt.Sprintf("API error %d: %s", resp.StatusCode, string(respBody))), nil
	}

	return mcp.NewToolResultText(string(respBody)), nil
}

// buildURL builds the full URL with path parameters
func (s *Server) buildURL(path string, args map[string]any) string {
	fullURL := s.apiURL + path

	// Replace path parameters
	for key, value := range args {
		placeholder := fmt.Sprintf("{%s}", key)
		if strings.Contains(fullURL, placeholder) {
			fullURL = strings.ReplaceAll(fullURL, placeholder, fmt.Sprintf("%v", value))
		}
	}
	return fullURL
}

// addAuthHeaders adds authentication headers to the request
func (s *Server) addAuthHeaders(req *http.Request, ctx context.Context) {
	// Try to get token from context
	if token := ctx.Value(TokenKey); token != nil {
		req.Header.Set("X-ED-API-Token", fmt.Sprintf("%s", token))
	}
}

// addQueryParameters adds query parameters to the request
func (s *Server) addQueryParameters(req *http.Request, parameters []Parameter, request mcp.CallToolRequest) {
	query := req.URL.Query()

	for _, param := range parameters {
		// Skip body parameters and path parameters - only process query parameters
		if param.In != "query" {
			continue
		}

		// Get parameter type from param.Type or param.Schema.Type
		paramType := param.Type
		if paramType == "" && param.Schema != nil {
			paramType = param.Schema.Type
		}

		// Use type-safe parameter extraction based on OpenAPI spec
		switch paramType {
		case "integer", "number":
			if value, err := optionalParam[float64](request, param.Name); err == nil && value != 0 {
				query.Add(param.Name, fmt.Sprintf("%v", value))
			}
		case "boolean":
			if value, err := optionalParam[bool](request, param.Name); err == nil {
				query.Add(param.Name, fmt.Sprintf("%t", value))
			}
		default:
			// Handle string and unknown types
			if value, err := optionalParam[string](request, param.Name); err == nil && value != "" {
				query.Add(param.Name, value)
			}
		}
	}

	req.URL.RawQuery = query.Encode()
}

// CreateServer creates an MCP server with auto-syncing OpenAPI tools
func CreateServer(version, swaggerDocURL, apiURL string, allowedTags []string) (*server.MCPServer, error) {
	srv := newServer(swaggerDocURL, apiURL, allowedTags)

	if err := srv.LoadSpec(); err != nil {
		return nil, fmt.Errorf("failed to load initial spec: %w", err)
	}

	// Create MCP server
	s := server.NewMCPServer("edgedelta-mcp-server", version)

	// Add tools
	for i, tool := range srv.tools {
		s.AddTool(tool, srv.handlers[i])
	}

	// Add manual tools
	s.AddTool(GetPipelinesTool(srv.client))

	return s, nil
}

func getDescription(path, method string, operation Operation) string {
	if operation.Description != "" {
		return operation.Description
	}
	if operation.Summary != "" {
		return operation.Summary
	}
	return fmt.Sprintf("%s %s", strings.ToUpper(method), path)
}

// optionalParam is a helper function that can be used to fetch a requested parameter from the request.
// It does the following checks:
// 1. Checks if the parameter is present in the request, if not, it returns its zero-value
// 2. If it is present, it checks if the parameter is of the expected type and returns it
func optionalParam[T any](r mcp.CallToolRequest, p string) (T, error) {
	var zero T

	// Check if the parameter is present in the request
	if _, ok := r.GetArguments()[p]; !ok {
		return zero, nil
	}

	// Check if the parameter is of the expected type
	if _, ok := r.GetArguments()[p].(T); !ok {
		return zero, fmt.Errorf("parameter %s is not of type %T, is %T", p, zero, r.GetArguments()[p])
	}

	return r.GetArguments()[p].(T), nil
}

// withParam populates schema based on the parameter definition
func withParam(param Parameter) mcp.PropertyOption {
	if param.Description != "" {
		param.Description = fmt.Sprintf("Parameter: %s", param.Name)
	}
	return func(schema map[string]any) {
		schema["description"] = param.Description
		if param.Required {
			schema["required"] = true
		}
		if param.Schema != nil && len(param.Schema.Enum) > 0 {
			schema["enum"] = param.Schema.Enum
		}
	}
}

// withBodyParam includes additional information about the body parameter in the description
func withBodyParam(param Parameter, definitions map[string]Definition) mcp.PropertyOption {
	if param.Description != "" {
		param.Description = "Parameter: " + param.Name
	}
	return func(schema map[string]any) {
		schema["description"] = param.Description + " (JSON Payload)"
		if param.Schema == nil || param.Schema.Ref == "" {
			return
		}
		ref := strings.TrimPrefix(param.Schema.Ref, refPrefix)
		definition, ok := definitions[ref]
		if !ok {
			return
		}
		json, err := json.Marshal(definition.Properties)
		if err != nil {
			return
		}
		schema["description"] = param.Description + ". It is JSON Payload with following fields: " + string(json)
	}
}
