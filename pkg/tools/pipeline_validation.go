package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
	"gopkg.in/yaml.v3"
)

// knownNodeFields maps node types to their valid YAML field names.
// Strict configv3 parser rejects unknown fields with a misleading 500 error,
// so we validate client-side to give actionable feedback.
var knownNodeFields = map[string]map[string]bool{
	"http_pull_input": {
		"name": true, "type": true, "endpoint": true, "method": true,
		"headers": true, "parameters": true, "pull_interval": true,
		"pull_schedule": true, "request_timeout": true, "request_body": true,
		"retry_http_code": true, "metadata": true, "description": true,
		"user_description": true, "data_types": true,
	},
	"http_workflow_input": {
		"name": true, "type": true, "num_of_requests": true,
		"workflow_pull_interval": true, "steps": true,
		"metadata": true, "description": true,
		"user_description": true, "data_types": true, "timeout": true,
	},
}

// commonFieldMistakes maps wrong field names to the correct ones.
var commonFieldMistakes = map[string]string{
	"url":             "endpoint (for http_pull_input) or steps[].endpoint (for http_workflow_input)",
	"interval":        "pull_interval (for http_pull_input) or workflow_pull_interval (for http_workflow_input)",
	"routing":         "links",
	"initial_request": "steps (for http_workflow_input)",
	"log_level":       "log: { level: info } under settings",
}

type validationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// validatePipelineYAML performs client-side validation of pipeline YAML content.
// Returns errors and warnings without calling the API.
func validatePipelineYAML(content string) validationResult {
	result := validationResult{Valid: true}

	// Parse YAML
	var config map[string]any
	if err := yaml.Unmarshal([]byte(content), &config); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("Invalid YAML: %v", err))
		return result
	}

	// Check version
	version, _ := config["version"].(string)
	if version != "v3" {
		result.Valid = false
		result.Errors = append(result.Errors, "Missing or invalid 'version' field. Must be 'version: v3' as the first line.")
	}

	// Check settings.tag
	settings, _ := config["settings"].(map[string]any)
	if settings == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "Missing 'settings' section.")
	} else if _, ok := settings["tag"]; !ok {
		result.Valid = false
		result.Errors = append(result.Errors, "Missing 'settings.tag' field (pipeline identifier).")
	}

	// Check nodes exist
	nodes, _ := config["nodes"].([]any)
	if len(nodes) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "Missing or empty 'nodes' section.")
	}

	// Check links (not routing)
	if _, hasRouting := config["routing"]; hasRouting {
		result.Valid = false
		result.Errors = append(result.Errors, "Use 'links:' instead of 'routing:' for node connections.")
	}
	links, _ := config["links"].([]any)
	if len(links) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "Missing or empty 'links' section.")
	}

	// Validate nodes
	nodeNames := map[string]bool{}
	hasOutput := false
	hasSelfTelemetry := false

	for i, nodeAny := range nodes {
		node, ok := nodeAny.(map[string]any)
		if !ok {
			result.Errors = append(result.Errors, fmt.Sprintf("Node %d is not a valid YAML mapping.", i))
			result.Valid = false
			continue
		}

		name, _ := node["name"].(string)
		nodeType, _ := node["type"].(string)

		if name == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("Node %d missing 'name' field.", i))
			result.Valid = false
		}
		if nodeType == "" {
			result.Errors = append(result.Errors, fmt.Sprintf("Node '%s' (index %d) missing 'type' field.", name, i))
			result.Valid = false
		}

		if name != "" {
			if nodeNames[name] {
				result.Errors = append(result.Errors, fmt.Sprintf("Duplicate node name: '%s'.", name))
				result.Valid = false
			}
			nodeNames[name] = true
		}

		if nodeType == "ed_output" {
			hasOutput = true
		}
		if nodeType == "ed_self_telemetry_input" {
			hasSelfTelemetry = true
		}

		// Check for known field mistakes on specific node types
		if knownFields, exists := knownNodeFields[nodeType]; exists {
			for field := range node {
				if !knownFields[field] {
					if suggestion, hasSuggestion := commonFieldMistakes[field]; hasSuggestion {
						result.Errors = append(result.Errors,
							fmt.Sprintf("Node '%s' (%s): unknown field '%s'. Did you mean: %s?", name, nodeType, field, suggestion))
						result.Valid = false
					}
				}
			}
		}

		// Check http_pull_input headers format
		if nodeType == "http_pull_input" {
			if headers, ok := node["headers"]; ok {
				if headerMap, isMap := headers.(map[string]any); isMap && len(headerMap) > 0 {
					result.Errors = append(result.Errors,
						fmt.Sprintf("Node '%s' (http_pull_input): 'headers' must be an array of {header, value} objects, not a map. Example:\n  headers:\n  - header: Accept\n    value: application/json", name))
					result.Valid = false
				}
			}
		}

		// Validate sequence processors
		if nodeType == "sequence" {
			validateSequenceNode(name, node, &result)
		}
	}

	if !hasOutput {
		result.Errors = append(result.Errors, "Missing output node. Add at least one 'ed_output' node.")
		result.Valid = false
	}
	if !hasSelfTelemetry {
		result.Errors = append(result.Errors, "Missing required 'ed_self_telemetry_input' node.")
		result.Valid = false
	}

	// Validate link references
	for i, linkAny := range links {
		link, ok := linkAny.(map[string]any)
		if !ok {
			continue
		}
		from, _ := link["from"].(string)
		to, _ := link["to"].(string)
		if from != "" && !nodeNames[from] {
			result.Errors = append(result.Errors, fmt.Sprintf("Link %d: 'from' references non-existent node '%s'.", i, from))
			result.Valid = false
		}
		if to != "" && !nodeNames[to] {
			result.Errors = append(result.Errors, fmt.Sprintf("Link %d: 'to' references non-existent node '%s'.", i, to))
			result.Valid = false
		}
	}

	// Check for Unicode in content
	if strings.ContainsAny(content, "→✓✗") {
		result.Warnings = append(result.Warnings, "YAML contains Unicode characters (→, ✓, ✗) which may cause API errors. Use ASCII only.")
	}

	// Check for json_field_path starting with dot
	if strings.Contains(content, "json_field_path") && strings.Contains(content, "\".\"") {
		result.Errors = append(result.Errors, "json_field_path cannot start with '.' — use '$' instead.")
		result.Valid = false
	}

	// Check for persisting_cursor_settings
	if strings.Contains(content, "persisting_cursor_settings") {
		result.Warnings = append(result.Warnings, "persisting_cursor_settings may cause API 500 errors. Consider removing.")
	}

	return result
}

// sequenceCompatibleProcessors is the authoritative list from
// configv3/config_validation.go validateSequenceNode().
var sequenceCompatibleProcessors = map[string]bool{
	"ottl_transform":         true,
	"dedup":                  true,
	"sample":                 true,
	"suppress":               true,
	"extract_metric":         true,
	"aggregate_metric":       true,
	"log_to_pattern_metric":  true,
	"json_unroll":            true,
	"generic_mask":           true,
	"split_with_delimiter":   true,
	"extract_json_field":     true,
	"lookup":                 true,
	"ottl_filter":            true,
	"delete_empty_values":    true,
	"tail_sample":            true,
	"sequence":               true,
	"ottl_context_filter":    true,
	"compound":               true,
	"deotel":                 true,
	"quota":                  true,
	"rate_limit":             true,
	"cumulative_to_delta":    true,
	"comment":                true,
}

func validateSequenceNode(name string, node map[string]any, result *validationResult) {
	processors, ok := node["processors"].([]any)
	if !ok || len(processors) == 0 {
		result.Warnings = append(result.Warnings, fmt.Sprintf("Sequence '%s' has no processors.", name))
		return
	}

	finalCount := 0
	deotelIdx := -1

	for i, procAny := range processors {
		proc, ok := procAny.(map[string]any)
		if !ok {
			continue
		}
		procType, _ := proc["type"].(string)

		if procType != "" && !sequenceCompatibleProcessors[procType] {
			result.Errors = append(result.Errors,
				fmt.Sprintf("Sequence '%s': processor '%s' at position %d is not sequence-compatible.", name, procType, i))
			result.Valid = false
		}

		if isFinal, _ := proc["final"].(bool); isFinal {
			finalCount++
			if i != len(processors)-1 {
				result.Errors = append(result.Errors,
					fmt.Sprintf("Sequence '%s': 'final: true' on processor %d but it's not the last processor.", name, i))
				result.Valid = false
			}
		}

		if procType == "deotel" {
			deotelIdx = i
		}
	}

	if deotelIdx >= 0 && deotelIdx != len(processors)-1 {
		result.Errors = append(result.Errors,
			fmt.Sprintf("Sequence '%s': 'deotel' processor must be last in the sequence.", name))
		result.Valid = false
	}

	if finalCount > 1 {
		result.Errors = append(result.Errors,
			fmt.Sprintf("Sequence '%s': multiple processors have 'final: true'. Only the last should.", name))
		result.Valid = false
	}
}

// ValidatePipelineTool provides client-side validation of pipeline YAML
// before calling the save API, giving actionable error messages.
func ValidatePipelineTool() (mcp.Tool, server.ToolHandlerFunc) {
	return mcp.NewTool("validate_pipeline",
			mcp.WithTitleAnnotation("Validate Pipeline"),
			mcp.WithDescription(`Validate pipeline v3 YAML content without saving. Checks for common errors including:
- Missing version: v3, settings.tag, required nodes
- Wrong field names (url vs endpoint, routing vs links)
- Invalid headers format for http_pull_input
- Non-sequence-compatible processors
- Incorrect final/deotel ordering
- Invalid link references
- Unicode characters, json_field_path issues

Use this BEFORE save_pipeline to catch errors with clear messages instead of cryptic API 500s.`),
			mcp.WithString("content",
				mcp.Description("Pipeline YAML content to validate"),
				mcp.Required(),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(true),
			mcp.WithDestructiveHintAnnotation(false),
			mcp.WithOpenWorldHintAnnotation(false),
		),
		func(ctx context.Context, request mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			content, err := request.RequireString("content")
			if err != nil {
				return mcp.NewToolResultError("missing required parameter: content"), err
			}

			result := validatePipelineYAML(content)

			resultBytes, err := json.Marshal(result)
			if err != nil {
				return nil, fmt.Errorf("failed to marshal validation result: %w", err)
			}

			return mcp.NewToolResultText(string(resultBytes)), nil
		}
}
