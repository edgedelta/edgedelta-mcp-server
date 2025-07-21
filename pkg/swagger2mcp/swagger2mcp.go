package swagger2mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/go-openapi/spec"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type client interface {
	Get(url string) (*http.Response, error)
	Do(req *http.Request) (*http.Response, error)
}

// ToolToHandler encapsulates a tool and its handler
type ToolToHandler struct {
	Tool    mcp.Tool
	Handler server.ToolHandlerFunc
}

func fetchOpenAPISpec(cl client, url string) (*spec.Swagger, error) {
	resp, err := cl.Get(url)
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

	swaggerSpec := &spec.Swagger{}
	if err := json.Unmarshal(data, swaggerSpec); err != nil {
		log.Fatalf("Failed to parse swagger.json: %v", err)
	}

	err = spec.ExpandSpec(swaggerSpec, &spec.ExpandOptions{
		RelativeBase: "",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to expand spec: %w", err)
	}

	return swaggerSpec, nil
}

func createToolToHandlers(apiURL string, cl client, swaggerSpec *spec.Swagger, allowedTags []string) ([]ToolToHandler, error) {
	var toolToHandlerSlice []ToolToHandler

	for path, pathItem := range swaggerSpec.Paths.Paths {
		operations := map[string]*spec.Operation{
			"GET":    pathItem.Get,
			"POST":   pathItem.Post,
			"PUT":    pathItem.Put,
			"DELETE": pathItem.Delete,
			"PATCH":  pathItem.Patch,
		}

		for method, operation := range operations {
			if operation == nil {
				continue
			}
			if !hasAllowedTag(operation.Tags, allowedTags) {
				continue
			}
			toolToHandler, err := createToolToHandler(cl, apiURL, path, method, operation, swaggerSpec)
			if err != nil {
				return nil, err
			}
			toolToHandlerSlice = append(toolToHandlerSlice, toolToHandler)
		}
	}

	return toolToHandlerSlice, nil
}

func createToolToHandler(cl client, apiURL, path, method string, operation *spec.Operation, swaggerSpec *spec.Swagger) (ToolToHandler, error) {
	toolName, err := getToolName(operation)
	if err != nil {
		return ToolToHandler{}, err
	}

	// I believe we shouldn't use path and method to generate description
	description, err := getDescription(operation)
	if err != nil {
		return ToolToHandler{}, err
	}

	inputSchema, err := inputSchemaFromOperation(operation, swaggerSpec)
	if err != nil {
		return ToolToHandler{}, err
	}
	tool := mcp.NewToolWithRawSchema(toolName, description, inputSchema)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return makeOpenAPICall(ctx, cl, request, apiURL, path, method, operation)
	}

	return ToolToHandler{
		Tool:    tool,
		Handler: handler,
	}, err
}

func getToolName(operation *spec.Operation) (string, error) {
	if operation.ID != "" {
		return operation.ID, nil
	} else if operation.Tags[0] != "" { // TODO: This is a fallback we'll get rid of once we ensure all operations have an ID.
		lower := strings.ToLower(operation.Tags[0])
		snakeCase := strings.ReplaceAll(lower, " ", "_")
		return snakeCase, nil
	}
	return "", fmt.Errorf("no operation id found for operation")
}

func getDescription(operation *spec.Operation) (string, error) {
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

func inputSchemaFromOperation(operation *spec.Operation, swaggerSpec *spec.Swagger) ([]byte, error) {
	schema := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
	properties := schema["properties"].(map[string]any)
	var required []string

	for _, param := range operation.Parameters {
		if param.In == "body" {
			// Use anonymous struct to combine schema props with description
			bodySchema := struct {
				spec.SchemaProps
				Description string `json:"description,omitempty"`
			}{
				SchemaProps: param.Schema.SchemaProps,
				Description: param.Description,
			}

			if err := spec.ExpandSchema(param.Schema, swaggerSpec, nil); err != nil {
				return nil, fmt.Errorf("failed to expand schema for param %s: %w", param.Name, err)
			}

			properties[param.Name] = bodySchema
		} else {
			properties[param.Name] = map[string]any{
				"type":        param.Type,
				"description": param.Description,
			}
		}
		if param.Required {
			required = append(required, param.Name)
		}
	}

	if len(required) > 0 {
		schema["required"] = required
	}

	schemaJSON, err := json.MarshalIndent(schema, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("failed to marshal schema: %w", err)
	}

	return schemaJSON, nil
}

func makeOpenAPICall(
	ctx context.Context,
	cl client,
	request mcp.CallToolRequest,
	apiURL, path, method string,
	operation *spec.Operation,
) (*mcp.CallToolResult, error) {
	args, ok := request.Params.Arguments.(map[string]any)
	if !ok {
		return mcp.NewToolResultError("invalid arguments format"), nil
	}

	fullURL := buildURL(apiURL, path, args)

	// Check for body parameters and prepare request body
	var requestBody io.Reader
	var bodyParam *spec.Parameter
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
	resp, err := cl.Do(req)
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

func addQueryParameters(req *http.Request, parameters []spec.Parameter, request mcp.CallToolRequest) {
	query := req.URL.Query()

	for _, param := range parameters {
		// Skip body parameters and path parameters - only process query parameters
		if param.In != "query" {
			continue
		}

		// Get parameter type from param.Type or param.Schema.Type
		paramType := param.Type

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

func NewToolsFromSpec(apiURL string, swaggerSpec *spec.Swagger, cl client, opts ...NewToolsFromSpecOption) ([]ToolToHandler, error) {
	var options ToolsFromSpecOptions
	for _, opt := range opts {
		opt(&options)
	}

	return createToolToHandlers(apiURL, cl, swaggerSpec, options.AllowedTags)
}

func NewToolsFromURL(url, apiURL string, cl client, opts ...NewToolsFromSpecOption) ([]ToolToHandler, error) {
	spec, err := fetchOpenAPISpec(cl, url)
	if err != nil {
		return nil, err
	}

	return NewToolsFromSpec(apiURL, spec, cl, opts...)
}
