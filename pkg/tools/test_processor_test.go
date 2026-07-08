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
