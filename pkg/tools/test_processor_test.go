package tools

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- test_pipeline_ottl ---

func TestTestPipelineOTTLTool(t *testing.T) {
	var gotMethod, gotPath, gotBody, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("X-ED-API-Token")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"items":["{\"severity_text\":\"INFO\"}"]}`))
	}))
	defer srv.Close()

	_, handler := TestPipelineOTTLTool(newTestClient(srv.URL))
	res, err := handler(testCtx(), callToolRequest(map[string]any{
		"conf_id":    "abc",
		"statements": `set(severity_text,"INFO")`,
		"items":      []any{`{"body":"x"}`},
	}))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method=%s", gotMethod)
	}
	if gotPath != "/v1/orgs/org123/pipelines/abc/test/ottl" {
		t.Errorf("path=%s", gotPath)
	}
	if gotAuth != "tok" {
		t.Errorf("auth=%q", gotAuth)
	}
	if !strings.Contains(gotBody, `"statements"`) || !strings.Contains(gotBody, `"items"`) {
		t.Errorf("body=%s", gotBody)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "severity_text") {
		t.Errorf("missing transformed output, result=%s", text)
	}
	if !strings.Contains(text, "guidance") {
		t.Errorf("missing guidance, result=%s", text)
	}
}

func TestTestPipelineOTTLTool_MissingStatements(t *testing.T) {
	_, handler := TestPipelineOTTLTool(newTestClient("http://unused"))
	res, _ := handler(testCtx(), callToolRequest(map[string]any{
		"conf_id": "abc",
		"items":   []any{`{"body":"x"}`},
	}))
	if res == nil || !res.IsError {
		t.Fatalf("expected error result for missing statements, got %+v", res)
	}
}

// --- test_pipeline_grok ---

func TestTestPipelineGrokTool(t *testing.T) {
	var gotMethod, gotPath, gotBody, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("X-ED-API-Token")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"structuredLogs":[{"status":"200"}]}`))
	}))
	defer srv.Close()

	_, handler := TestPipelineGrokTool(newTestClient(srv.URL))
	res, err := handler(testCtx(), callToolRequest(map[string]any{
		"conf_id": "abc",
		"pattern": "%{COMMONAPACHELOG}",
		"logs":    []any{`127.0.0.1 - - [10/Oct/2000:13:55:36 -0700] "GET /apache.gif HTTP/1.0" 200 2326`},
	}))
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method=%s", gotMethod)
	}
	if gotPath != "/v1/orgs/org123/pipelines/abc/test/grok" {
		t.Errorf("path=%s", gotPath)
	}
	if gotAuth != "tok" {
		t.Errorf("auth=%q", gotAuth)
	}
	if !strings.Contains(gotBody, `"pattern"`) || !strings.Contains(gotBody, `"logs"`) {
		t.Errorf("body=%s", gotBody)
	}
	text := resultText(t, res)
	if !strings.Contains(text, `"status":"200"`) {
		t.Errorf("missing structured output, result=%s", text)
	}
	if !strings.Contains(text, "guidance") {
		t.Errorf("missing guidance, result=%s", text)
	}
}

func TestTestPipelineGrokTool_MissingPattern(t *testing.T) {
	_, handler := TestPipelineGrokTool(newTestClient("http://unused"))
	res, _ := handler(testCtx(), callToolRequest(map[string]any{
		"conf_id": "abc",
		"logs":    []any{"a log line"},
	}))
	if res == nil || !res.IsError {
		t.Fatalf("expected error result for missing pattern, got %+v", res)
	}
}
