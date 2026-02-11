package validation

import (
	"fmt"
	"regexp"
	"strings"
)

// MarkdownContentRule validates HTML content in markdown widgets against DOMPurify restrictions.
// EdgeDelta's markdown widget uses DOMPurify with FORBID_TAGS: ['style'] and strips scripts/event handlers.
type MarkdownContentRule struct{}

func (r *MarkdownContentRule) Name() string { return "markdown_content" }

// Forbidden tags that DOMPurify will strip or block
var forbiddenTags = []string{
	"script",
	"style",
	"link",     // Can load external stylesheets
	"iframe",   // Security risk
	"object",   // Plugin content
	"embed",    // Plugin content
	"form",     // Can submit data
	"input",    // Form element
	"textarea", // Form element
	"button",   // Form element
}

// Event handler attributes that DOMPurify strips
var eventHandlerPattern = regexp.MustCompile(`(?i)\s+on\w+\s*=`)

// JavaScript protocol in URLs
var jsProtocolPattern = regexp.MustCompile(`(?i)(?:href|src|action)\s*=\s*["']?\s*javascript:`)

func (r *MarkdownContentRule) Validate(ctx *DashboardContext) *ValidationResult {
	result := &ValidationResult{}

	for _, w := range ctx.Widgets {
		widgetType, _ := w["type"].(string)
		if widgetType != "markdown" {
			continue
		}

		widgetID := w["id"]

		// Get content from params.content (v4 format)
		var content string
		if params, ok := w["params"].(map[string]interface{}); ok {
			content, _ = params["content"].(string)
		}
		// Also check direct content field
		if content == "" {
			content, _ = w["content"].(string)
		}

		if content == "" {
			continue
		}

		// Check for forbidden tags
		contentLower := strings.ToLower(content)
		for _, tag := range forbiddenTags {
			// Match opening tags: <script, <style, etc.
			pattern := "<" + tag + "[\\s>]"
			if matched, _ := regexp.MatchString("(?i)"+pattern, content); matched {
				result.AddError(
					fmt.Sprintf("widget[%v].content", widgetID),
					fmt.Sprintf("Forbidden HTML tag <%s> detected - will be stripped by DOMPurify", tag),
					getSuggestionForTag(tag),
				)
			}
		}

		// Check for event handlers
		if eventHandlerPattern.MatchString(content) {
			// Extract the specific handler for better error message
			matches := regexp.MustCompile(`(?i)\s+(on\w+)\s*=`).FindStringSubmatch(content)
			handlerName := "event handler"
			if len(matches) > 1 {
				handlerName = matches[1]
			}
			result.AddError(
				fmt.Sprintf("widget[%v].content", widgetID),
				fmt.Sprintf("Event handler attribute '%s' detected - will be stripped by DOMPurify", handlerName),
				"Remove event handlers; use native HTML elements like <details>/<summary> for interactivity",
			)
		}

		// Check for javascript: protocol
		if jsProtocolPattern.MatchString(content) {
			result.AddError(
				fmt.Sprintf("widget[%v].content", widgetID),
				"JavaScript protocol in URL detected - will be sanitized by DOMPurify",
				"Use regular URLs (https://) instead of javascript: protocol",
			)
		}

		// Check for base64 data URIs with script content (potential XSS vector)
		if strings.Contains(contentLower, "data:text/html") {
			result.AddError(
				fmt.Sprintf("widget[%v].content", widgetID),
				"HTML data URI detected - potential security risk",
				"Use direct HTML content or image data URIs (data:image/) instead",
			)
		}
	}

	return result
}

func getSuggestionForTag(tag string) string {
	switch tag {
	case "script":
		return "Remove <script> tags; JavaScript is not supported in markdown widgets"
	case "style":
		return "Use inline style=\"...\" attributes instead of <style> tags"
	case "link":
		return "External stylesheets not supported; use inline styles"
	case "iframe":
		return "Embedded frames not supported in markdown widgets"
	case "form", "input", "textarea", "button":
		return "Form elements not supported; markdown widgets are display-only"
	default:
		return fmt.Sprintf("Remove <%s> tag - not supported in markdown widgets", tag)
	}
}
