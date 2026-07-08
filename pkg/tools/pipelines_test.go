package tools

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// --- shared test helpers (reused by other pipeline tool tests) ---

func newTestClient(url string) *HTTPClient { return NewHTTPClient(url, "") }

func testCtx() context.Context {
	ctx := context.WithValue(context.Background(), OrgIDKey, "org123")
	return context.WithValue(ctx, EDTokenKey, "tok")
}

func callToolRequest(args map[string]any) mcp.CallToolRequest {
	return mcp.CallToolRequest{Params: mcp.CallToolParams{Arguments: args}}
}

func resultText(t *testing.T, r *mcp.CallToolResult) string {
	t.Helper()
	if r == nil || len(r.Content) == 0 {
		t.Fatalf("expected content in result, got %+v", r)
	}
	tc, ok := r.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatalf("expected TextContent, got %T", r.Content[0])
	}
	return tc.Text
}

// --- validate_pipeline_config ---

func TestValidatePipelineConfigTool_Valid(t *testing.T) {
	var gotMethod, gotPath, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"valid":true}`))
	}))
	defer srv.Close()

	_, handler := ValidatePipelineConfigTool(newTestClient(srv.URL))
	res, err := handler(testCtx(), callToolRequest(map[string]any{"content": "nodes: []"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/v1/orgs/org123/confs/validate" {
		t.Errorf("path = %s, want /v1/orgs/org123/confs/validate", gotPath)
	}
	if !strings.Contains(gotBody, `"content":"nodes: []"`) {
		t.Errorf("body = %s, want content field", gotBody)
	}
	text := resultText(t, res)
	if !strings.Contains(text, `"valid":true`) {
		t.Errorf("result = %s, want valid:true", text)
	}
	if !strings.Contains(text, "guidance") {
		t.Errorf("result = %s, want guidance", text)
	}
}

func TestValidatePipelineConfigTool_Invalid(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"valid":false,"reason":"unknown node type"}`))
	}))
	defer srv.Close()

	_, handler := ValidatePipelineConfigTool(newTestClient(srv.URL))
	res, err := handler(testCtx(), callToolRequest(map[string]any{"content": "bad"}))
	if err != nil {
		t.Fatalf("valid:false must not be a tool error, got: %v", err)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "unknown node type") {
		t.Errorf("result = %s, want reason echoed", text)
	}
}

func TestValidatePipelineConfigTool_MissingContent(t *testing.T) {
	_, handler := ValidatePipelineConfigTool(newTestClient("http://unused"))
	res, _ := handler(testCtx(), callToolRequest(map[string]any{}))
	if res == nil || !res.IsError {
		t.Fatalf("expected error result for missing content, got %+v", res)
	}
}
