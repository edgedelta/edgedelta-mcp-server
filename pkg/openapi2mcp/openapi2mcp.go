package openapi2mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"io"
	"net/http"
	"strings"
)

type OpenAPISpec struct {
	Swagger string                          `json:"swagger"`
	Info    OpenAPIInfo                     `json:"info"`
	Host    string                          `json:"host"`
	Schemes []string                        `json:"schemes"`
	Paths   map[string]map[string]Operation `json:"paths"`
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
	Enum        []string `json:"enum"`
	Description string   `json:"description"`
}

var (
	httpClient *http.Client
)

// ToolToHandler encapsulates a tool and its handler
type ToolToHandler struct {
	Tool    mcp.Tool
	Handler server.ToolHandlerFunc
}

func fetchOpenAPISpec(url string) (*OpenAPISpec, error) {
	resp, err := httpClient.Get(url)
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

	spec := new(OpenAPISpec)
	if err := json.Unmarshal(data, spec); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response body, err: %w", err)
	}
	return spec, nil
}

func genToolAndHandlers(apiURL string, httpClient *http.Client, openAPISpec *OpenAPISpec, allowedTags []string) ([]ToolToHandler, error) {
	var toolToHandlerSlice []ToolToHandler

	for path, methods := range openAPISpec.Paths {
		for method, operation := range methods {
			// Skip if no allowed tags match
			if !hasAllowedTag(operation.Tags, allowedTags) {
				continue
			}

			toolToHandler, err := createToolToHandler(httpClient, apiURL, path, method, operation)
			if err != nil {
				return nil, err
			}
			toolToHandlerSlice = append(toolToHandlerSlice, toolToHandler)
		}
	}

	return toolToHandlerSlice, nil
}

func createToolToHandler(httpClient *http.Client, apiURL, path, method string, operation Operation) (ToolToHandler, error) {
	// We get operationId as tool name
	if operation.OperationID == "" {
		return ToolToHandler{}, fmt.Errorf("no operationId found for operation")
	}
	toolName := operation.OperationID
	// I believe we shouldn't use path and method to generate description
	description, err := getDescription(operation)
	if err != nil {
		return ToolToHandler{}, err
	}

	toolOptions := []mcp.ToolOption{mcp.WithDescription(description)}

	for _, param := range operation.Parameters {
		addParameterToTool(&toolOptions, param)
	}
	tool := mcp.NewTool(toolName, toolOptions...)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return makeOpenAPICall(ctx, httpClient, request, apiURL, path, method, operation)
	}

	return ToolToHandler{
		Tool:    tool,
		Handler: handler,
	}, err
}

func getDescription(operation Operation) (string, error) {
	if operation.Description != "" {
		return operation.Description, nil
	} else if operation.Summary != "" {
		return operation.Summary, nil
	}
	return "", fmt.Errorf("no description found for operation")
}

func hasAllowedTag(tags []string, allowedTags []string) bool {
	if len(allowedTags) == 0 {
		return true
	}

	for _, tag := range tags {
		for _, allowedTag := range allowedTags {
			if tag == allowedTag {
				return true
			}
		}
	}
	return false
}

func addParameterToTool(toolOptions *[]mcp.ToolOption, param Parameter) {
	paramName := param.Name
	paramDesc := param.Description
	if paramDesc == "" {
		paramDesc = fmt.Sprintf("Parameter: %s", paramName)
	}

	// Handle body parameters
	if param.In == "body" {
		*toolOptions = append(*toolOptions, mcp.WithString(paramName,
			mcp.Description(paramDesc+" (JSON payload)"),
		))
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
		if param.Schema != nil && len(param.Schema.Enum) > 0 {
			*toolOptions = append(*toolOptions, mcp.WithString(paramName,
				mcp.Description(paramDesc),
				mcp.Enum(param.Schema.Enum...),
			))
		} else {
			*toolOptions = append(*toolOptions, mcp.WithString(paramName,
				mcp.Description(paramDesc),
			))
		}
	case "integer", "number":
		*toolOptions = append(*toolOptions, mcp.WithNumber(paramName,
			mcp.Description(paramDesc),
		))
	case "boolean":
		*toolOptions = append(*toolOptions, mcp.WithBoolean(paramName,
			mcp.Description(paramDesc),
		))
	default:
		// Default to string for unknown types
		*toolOptions = append(*toolOptions, mcp.WithString(paramName,
			mcp.Description(paramDesc),
		))
	}
}

func makeOpenAPICall(
	ctx context.Context,
	httpClient *http.Client,
	request mcp.CallToolRequest,
	apiURL, path, method string,
	operation Operation,
) (*mcp.CallToolResult, error) {
	args, ok := request.Params.Arguments.(map[string]any)
	if !ok {
		return mcp.NewToolResultError("invalid arguments format"), nil
	}

	fullURL := buildURL(apiURL, path, args)

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

	// Add query parameters (skip body parameters)
	addQueryParameters(req, operation.Parameters, request)

	// Note: Attach headers through the roundtripper. The roundtripper will fetch the headers from the context.
	// The context will be updated with the headers from the request.
	resp, err := httpClient.Do(req)
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

func addQueryParameters(req *http.Request, parameters []Parameter, request mcp.CallToolRequest) {
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

// buildURL builds the full URL with path parameters
func buildURL(apiURL, path string, args map[string]any) string {
	fullURL := apiURL + path

	// Replace path parameters
	for key, value := range args {
		placeholder := fmt.Sprintf("{%s}", key)
		if strings.Contains(fullURL, placeholder) {
			fullURL = strings.ReplaceAll(fullURL, placeholder, fmt.Sprintf("%v", value))
		}
	}
	return fullURL
}

type ToolsFromSpecOptions struct {
	AllowedTags []string
}

type NewToolsFromSpecOption func(*ToolsFromSpecOptions)

func WithAllowedTags(allowedTags []string) NewToolsFromSpecOption {
	return func(o *ToolsFromSpecOptions) {
		o.AllowedTags = allowedTags
	}
}

func NewToolsFromSpec(apiURL string, openAPISpec *OpenAPISpec, httpClient *http.Client, opts ...NewToolsFromSpecOption) ([]ToolToHandler, error) {
	var options ToolsFromSpecOptions
	for _, opt := range opts {
		opt(&options)
	}

	return genToolAndHandlers(apiURL, httpClient, openAPISpec, options.AllowedTags)
}

func NewToolsFromURL(url, apiURL string, httpClient *http.Client, opts ...NewToolsFromSpecOption) ([]ToolToHandler, error) {
	spec, err := fetchOpenAPISpec(url)
	if err != nil {
		return nil, err
	}

	return NewToolsFromSpec(apiURL, spec, httpClient, opts...)
}
