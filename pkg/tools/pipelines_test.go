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
	var gotMethod, gotPath, gotBody, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("X-ED-API-Token")
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
	if gotAuth != "tok" {
		t.Errorf("auth header = %q, want %q", gotAuth, "tok")
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

func TestValidatePipelineConfigTool_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer srv.Close()

	_, handler := ValidatePipelineConfigTool(newTestClient(srv.URL))
	_, err := handler(testCtx(), callToolRequest(map[string]any{"content": "nodes: []"}))
	if err == nil {
		t.Fatalf("expected non-nil error for non-2xx response, got nil")
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

// --- save_pipeline_config ---

func TestSavePipelineConfigTool(t *testing.T) {
	var gotMethod, gotPath, gotBody, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("X-ED-API-Token")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"version":"1752190141312"}`))
	}))
	defer srv.Close()

	_, handler := SavePipelineConfigTool(newTestClient(srv.URL))
	res, err := handler(testCtx(), callToolRequest(map[string]any{
		"conf_id":     "abc123",
		"content":     "nodes: []",
		"description": "restore severity",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/v1/orgs/org123/pipelines/abc123/save" {
		t.Errorf("path = %s, want .../pipelines/abc123/save", gotPath)
	}
	if gotAuth != "tok" {
		t.Errorf("auth header = %q, want %q", gotAuth, "tok")
	}
	if !strings.Contains(gotBody, `"content":"nodes: []"`) || !strings.Contains(gotBody, `"description":"restore severity"`) {
		t.Errorf("body = %s, want content+description", gotBody)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "1752190141312") || !strings.Contains(text, "deploy_pipeline") {
		t.Errorf("result = %s, want version echoed and deploy guidance", text)
	}
}

func TestSavePipelineConfigTool_MissingConfID(t *testing.T) {
	_, handler := SavePipelineConfigTool(newTestClient("http://unused"))
	res, _ := handler(testCtx(), callToolRequest(map[string]any{"content": "x"}))
	if res == nil || !res.IsError {
		t.Fatalf("expected error result for missing conf_id, got %+v", res)
	}
}

// --- revert_pipeline ---

func TestRevertPipelineTool(t *testing.T) {
	var gotMethod, gotPath, gotAuth, gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("X-ED-API-Token")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"reverted"}`))
	}))
	defer srv.Close()

	_, handler := RevertPipelineTool(newTestClient(srv.URL))
	res, err := handler(testCtx(), callToolRequest(map[string]any{
		"conf_id":     "abc123",
		"version":     "1752190141312",
		"description": "rollback",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/v1/orgs/org123/pipelines/abc123/revert/1752190141312" {
		t.Errorf("path = %s, want .../revert/1752190141312", gotPath)
	}
	if gotAuth != "tok" {
		t.Errorf("auth header = %q, want %q", gotAuth, "tok")
	}
	// The backend rejects an empty body with 400; a JSON object must always be sent.
	if !strings.Contains(gotBody, `"description":"rollback"`) {
		t.Errorf("body = %q, want description field", gotBody)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "reverted") || !strings.Contains(text, "guidance") {
		t.Errorf("result = %s, want status + guidance", text)
	}
}

func TestRevertPipelineTool_MissingVersion(t *testing.T) {
	_, handler := RevertPipelineTool(newTestClient("http://unused"))
	res, _ := handler(testCtx(), callToolRequest(map[string]any{"conf_id": "abc123"}))
	if res == nil || !res.IsError {
		t.Fatalf("expected error result for missing version, got %+v", res)
	}
}
