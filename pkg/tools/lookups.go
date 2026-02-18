package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type LookupToolResponse struct {
	Data     json.RawMessage `json:"data"`
	Guidance *LookupGuidance `json:"guidance,omitempty"`
}

type LookupGuidance struct {
	ResultStatus string   `json:"result_status"`
	NextSteps    []string `json:"next_steps,omitempty"`
}

func GetLookupsTool(client Client) (mcp.Tool, server.ToolHandlerFunc) {
	return mcp.NewTool("get_lookups",
			mcp.WithTitleAnnotation("Get Lookups"),
			mcp.WithDescription("List all lookup table metadata for the organization. Returns names, sizes, row counts, descriptions, and timestamps."),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			orgID, token, err := FetchContextKeys(ctx)
			if err != nil {
				return nil, err
			}

			lookupsURL := fmt.Sprintf("%s/v1/orgs/%s/lookup_tables/metadata", client.APIURL(), orgID)
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, lookupsURL, nil)
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %w", err)
			}

			req.Header.Add("Content-Type", "application/json")
			req.Header.Add("X-ED-API-Token", token)

			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()

			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to read response body: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to get lookups, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			response := LookupToolResponse{
				Data: bodyBytes,
				Guidance: &LookupGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"Use create_lookup to upload a new CSV lookup table.",
						"Use update_lookup with a lookup table name to replace its content.",
					},
				},
			}

			r, err := json.Marshal(response)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal wrapped response: %w", err)
			}

			return mcp.NewToolResultText(string(r)), nil
		}
}

// buildMultipartBody creates a multipart form body with the CSV content as a file upload
// and optional description/tags fields.
func buildMultipartBody(filename, content string, fields map[string]string) (*bytes.Buffer, string, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add CSV content as file field "data"
	part, err := writer.CreateFormFile("data", filename)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create form file: %w", err)
	}
	if _, err := part.Write([]byte(content)); err != nil {
		return nil, "", fmt.Errorf("failed to write file content: %w", err)
	}

	// Add optional text fields
	for k, v := range fields {
		if v != "" {
			if err := writer.WriteField(k, v); err != nil {
				return nil, "", fmt.Errorf("failed to write field %s: %w", k, err)
			}
		}
	}

	if err := writer.Close(); err != nil {
		return nil, "", fmt.Errorf("failed to close multipart writer: %w", err)
	}

	return body, writer.FormDataContentType(), nil
}

func CreateLookupTool(client Client) (mcp.Tool, server.ToolHandlerFunc) {
	return mcp.NewTool("create_lookup",
			mcp.WithTitleAnnotation("Create Lookup"),
			mcp.WithDescription(`Create a new CSV lookup table by uploading CSV content. The first row is treated as the header. Pipelines can reference this table via csv_lookup nodes.`),
			mcp.WithString("name",
				mcp.Description("Filename for the lookup table (must end in .csv)"),
				mcp.Required(),
			),
			mcp.WithString("content",
				mcp.Description("CSV content for the lookup table. First row should be the header row."),
				mcp.Required(),
			),
			mcp.WithString("description",
				mcp.Description("Optional description of the lookup table"),
			),
			mcp.WithString("tags",
				mcp.Description("Optional tags for the lookup table"),
			),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			orgID, token, err := FetchContextKeys(ctx)
			if err != nil {
				return nil, err
			}

			name, err := request.RequireString("name")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: name"), err
			}

			content, err := request.RequireString("content")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: content"), err
			}

			// Build optional fields
			fields := map[string]string{}
			args := request.GetArguments()
			if desc, ok := args["description"].(string); ok && desc != "" {
				fields["description"] = desc
			}
			if tags, ok := args["tags"].(string); ok && tags != "" {
				fields["tags"] = tags
			}

			body, contentType, err := buildMultipartBody(name, content, fields)
			if err != nil {
				return nil, fmt.Errorf("failed to build multipart body: %w", err)
			}

			createURL := fmt.Sprintf("%s/v1/orgs/%s/lookup_tables", client.APIURL(), orgID)
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, createURL, body)
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %w", err)
			}

			req.Header.Add("Content-Type", contentType)
			req.Header.Add("X-ED-API-Token", token)

			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()

			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to read response body: %w", err)
			}

			if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
				return nil, fmt.Errorf("failed to create lookup, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			response := LookupToolResponse{
				Data: bodyBytes,
				Guidance: &LookupGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"Lookup table created successfully.",
						"Use get_lookups to verify the table appears in the list.",
						"Use update_lookup with the table name to replace content later.",
						"Reference this lookup in pipelines via csv_lookup nodes.",
					},
				},
			}

			r, err := json.Marshal(response)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal wrapped response: %w", err)
			}

			return mcp.NewToolResultText(string(r)), nil
		}
}

func UpdateLookupTool(client Client) (mcp.Tool, server.ToolHandlerFunc) {
	return mcp.NewTool("update_lookup",
			mcp.WithTitleAnnotation("Update Lookup"),
			mcp.WithDescription("Update an existing CSV lookup table by replacing its content. Use get_lookups to find available tables first."),
			mcp.WithString("lookup_id",
				mcp.Description("ID (name) of the lookup table to update"),
				mcp.Required(),
			),
			mcp.WithString("content",
				mcp.Description("Updated CSV content for the lookup table. First row should be the header row."),
				mcp.Required(),
			),
			mcp.WithString("description",
				mcp.Description("Optional updated description"),
			),
			mcp.WithString("tags",
				mcp.Description("Optional updated tags"),
			),
			mcp.WithReadOnlyHintAnnotation(false),
			mcp.WithIdempotentHintAnnotation(false),
			mcp.WithDestructiveHintAnnotation(true),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			orgID, token, err := FetchContextKeys(ctx)
			if err != nil {
				return nil, err
			}

			lookupID, err := request.RequireString("lookup_id")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: lookup_id"), err
			}

			content, err := request.RequireString("content")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: content"), err
			}

			// Build optional fields
			fields := map[string]string{}
			args := request.GetArguments()
			if desc, ok := args["description"].(string); ok && desc != "" {
				fields["description"] = desc
			}
			if tags, ok := args["tags"].(string); ok && tags != "" {
				fields["tags"] = tags
			}

			// Use the lookupID as the filename for the multipart upload
			body, contentType, err := buildMultipartBody(lookupID, content, fields)
			if err != nil {
				return nil, fmt.Errorf("failed to build multipart body: %w", err)
			}

			updateURL := fmt.Sprintf("%s/v1/orgs/%s/lookup_tables/%s", client.APIURL(), orgID, lookupID)
			req, err := http.NewRequestWithContext(ctx, http.MethodPut, updateURL, body)
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %w", err)
			}

			req.Header.Add("Content-Type", contentType)
			req.Header.Add("X-ED-API-Token", token)

			resp, err := client.Do(req)
			if err != nil {
				return nil, err
			}
			defer resp.Body.Close()

			bodyBytes, err := io.ReadAll(resp.Body)
			if err != nil {
				return nil, fmt.Errorf("failed to read response body: %w", err)
			}

			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to update lookup, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			response := LookupToolResponse{
				Data: bodyBytes,
				Guidance: &LookupGuidance{
					ResultStatus: "success",
					NextSteps: []string{
						"Lookup table updated successfully.",
						"Use get_lookups to verify the changes.",
						"Pipelines referencing this lookup via csv_lookup nodes will pick up the new content (within 15 minutes due to caching).",
					},
				},
			}

			r, err := json.Marshal(response)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal wrapped response: %w", err)
			}

			return mcp.NewToolResultText(string(r)), nil
		}
}
