package swagger2mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/params"
	"github.com/edgedelta/edgedelta-mcp-server/pkg/tools"

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

func createToolToHandlers(apiURL string, httpClient client, swaggerSpec *spec.Swagger, allowedTags []string) ([]ToolToHandler, error) {
	var toolToHandlerSlice []ToolToHandler

	for path, pathItem := range swaggerSpec.Paths.Paths {
		operations := map[string]*spec.Operation{
			http.MethodGet:    pathItem.Get,
			http.MethodPost:   pathItem.Post,
			http.MethodPut:    pathItem.Put,
			http.MethodDelete: pathItem.Delete,
			http.MethodPatch:  pathItem.Patch,
		}

		for method, operation := range operations {
			if operation == nil {
				continue
			}
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

func createToolToHandler(httpClient client, apiURL, path, method string, operation *spec.Operation) (ToolToHandler, error) {
	toolName, err := getToolName(operation)
	if err != nil {
		return ToolToHandler{}, err
	}

	// I believe we shouldn't use path and method to generate description
	description, err := getDescription(operation)
	if err != nil {
		return ToolToHandler{}, err
	}

	inputSchema, err := inputSchemaFromOperation(operation)
	if err != nil {
		return ToolToHandler{}, err
	}
	tool := mcp.NewToolWithRawSchema(toolName, description, inputSchema)

	handler := func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		return makeOpenAPICall(ctx, httpClient, request, apiURL, path, method, operation)
	}

	return ToolToHandler{
		Tool:    tool,
		Handler: handler,
	}, err
}

func getToolName(operation *spec.Operation) (string, error) {
	if operation.ID != "" {
		return operation.ID, nil
	}
	if operation.Tags[0] != "" { // TODO: This is a fallback we'll get rid of once we ensure all operations have an ID.
		lower := strings.ToLower(operation.Tags[0])
		snakeCase := strings.ReplaceAll(lower, " ", "_")
		return snakeCase, nil
	}
	return "", fmt.Errorf("no operation id found for operation")
}

func getDescription(operation *spec.Operation) (string, error) {
	if operation.Description != "" {
		return operation.Description, nil
	}
	if operation.Summary != "" {
		return operation.Summary, nil
	}
	return "", fmt.Errorf("no description found for operation")
}

func hasAllowedTag(tags []string, allowedTags []string) bool {
	if len(allowedTags) == 0 {
		return true
	}

	for _, tag := range tags {
		if slices.Contains(allowedTags, tag) {
			return true
		}
	}
	return false
}

func inputSchemaFromOperation(operation *spec.Operation) ([]byte, error) {
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

			properties[param.Name] = bodySchema
		} else {
			// TODO: For now, we take org_id from the path which conflicts with the JSONRPC convention,
			//  we can remove this trick in the future.
			if param.Name == "org_id" {
				continue
			}

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
	httpClient client,
	request mcp.CallToolRequest,
	apiURL, path, method string,
	operation *spec.Operation,
) (*mcp.CallToolResult, error) {
	args, ok := request.Params.Arguments.(map[string]any)
	if !ok {
		return mcp.NewToolResultError("invalid arguments format"), nil
	}

	args["org_id"], ok = orgIDKeyFromContext(ctx)
	if !ok {
		return mcp.NewToolResultError("failed to get org_id from context"), nil
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
			bodyJSON, err := json.Marshal(bodyData)
			if err != nil {
				return mcp.NewToolResultError(fmt.Sprintf("failed to marshal body data: %v", err)), nil
			}
			requestBody = bytes.NewReader(bodyJSON)
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
			if value, err := params.Optional[float64](request, param.Name); err == nil && value != 0 {
				query.Add(param.Name, fmt.Sprintf("%v", value))
			}
		case "boolean":
			if value, err := params.Optional[bool](request, param.Name); err == nil {
				query.Add(param.Name, fmt.Sprintf("%t", value))
			}
		default:
			// Handle string and unknown types
			if value, err := params.Optional[string](request, param.Name); err == nil && value != "" {
				query.Add(param.Name, value)
			}
		}
	}

	req.URL.RawQuery = query.Encode()
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

func NewToolsFromSpec(apiURL string, swaggerSpec *spec.Swagger, httpClient client, opts ...NewToolsFromSpecOption) ([]ToolToHandler, error) {
	var options ToolsFromSpecOptions
	for _, opt := range opts {
		opt(&options)
	}

	return createToolToHandlers(apiURL, httpClient, swaggerSpec, options.AllowedTags)
}

func orgIDKeyFromContext(ctx context.Context) (string, bool) {
	if orgID, ok := ctx.Value(tools.OrgIDKey).(string); ok {
		return orgID, true
	}
	return "", false
}
