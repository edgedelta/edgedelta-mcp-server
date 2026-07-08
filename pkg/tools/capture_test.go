package tools

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// --- start_pipeline_capture ---

func TestStartPipelineCaptureTool(t *testing.T) {
	var gotMethod, gotPath, gotBody, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("X-ED-API-Token")
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"T1","max_items":20}`))
	}))
	defer srv.Close()

	_, handler := StartPipelineCaptureTool(newTestClient(srv.URL))
	res, err := handler(testCtx(), callToolRequest(map[string]any{"conf_id": "abc"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/v1/orgs/org123/pipelines/abc/capture/task" {
		t.Errorf("path = %s, want /v1/orgs/org123/pipelines/abc/capture/task", gotPath)
	}
	if gotAuth != "tok" {
		t.Errorf("auth header = %q, want %q", gotAuth, "tok")
	}
	if !strings.Contains(gotBody, `"max_items"`) || !strings.Contains(gotBody, `"expires_at"`) {
		t.Errorf("body = %s, want max_items and expires_at fields", gotBody)
	}
	if strings.Contains(gotBody, `"nodes_to_capture"`) {
		t.Errorf("body = %s, want nodes_to_capture omitted when no nodes given", gotBody)
	}
	text := resultText(t, res)
	if !strings.Contains(text, "T1") {
		t.Errorf("result = %s, want task id echoed", text)
	}
	if !strings.Contains(text, "guidance") {
		t.Errorf("result = %s, want guidance", text)
	}
}

func TestStartPipelineCaptureTool_WithNodes(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"T2"}`))
	}))
	defer srv.Close()

	_, handler := StartPipelineCaptureTool(newTestClient(srv.URL))
	_, err := handler(testCtx(), callToolRequest(map[string]any{
		"conf_id": "abc",
		"nodes":   []any{"n1", "n2"},
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(gotBody, `"nodes_to_capture":["n1","n2"]`) {
		t.Errorf("body = %s, want nodes_to_capture:[n1,n2]", gotBody)
	}
}

func TestStartPipelineCaptureTool_MissingConfID(t *testing.T) {
	_, handler := StartPipelineCaptureTool(newTestClient("http://unused"))
	res, _ := handler(testCtx(), callToolRequest(map[string]any{}))
	if res == nil || !res.IsError {
		t.Fatalf("expected error result for missing conf_id, got %+v", res)
	}
}

// --- get_pipeline_capture_status ---

func TestGetPipelineCaptureStatusTool(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("X-ED-API-Token")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"T1","capture_status":{"agent-1":{"status":"success","updated_timestamp":1783541610}}}`))
	}))
	defer srv.Close()

	_, handler := GetPipelineCaptureStatusTool(newTestClient(srv.URL))
	res, err := handler(testCtx(), callToolRequest(map[string]any{"task_id": "T1"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %s, want GET", gotMethod)
	}
	if gotPath != "/v1/orgs/org123/pipelines/capture/task/T1/status" {
		t.Errorf("path = %s, want /v1/orgs/org123/pipelines/capture/task/T1/status", gotPath)
	}
	if gotAuth != "tok" {
		t.Errorf("auth header = %q, want %q", gotAuth, "tok")
	}
	text := resultText(t, res)
	if !strings.Contains(text, "success") || !strings.Contains(text, "guidance") {
		t.Errorf("result = %s, want status + guidance", text)
	}
}

func TestGetPipelineCaptureStatusTool_MissingTaskID(t *testing.T) {
	_, handler := GetPipelineCaptureStatusTool(newTestClient("http://unused"))
	res, _ := handler(testCtx(), callToolRequest(map[string]any{}))
	if res == nil || !res.IsError {
		t.Fatalf("expected error result for missing task_id, got %+v", res)
	}
}

// --- get_pipeline_capture_results ---

func TestGetPipelineCaptureResultsTool(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("X-ED-API-Token")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`[{"source":"a","nodes":{}}]`))
	}))
	defer srv.Close()

	_, handler := GetPipelineCaptureResultsTool(newTestClient(srv.URL))
	res, err := handler(testCtx(), callToolRequest(map[string]any{"conf_id": "abc"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %s, want GET", gotMethod)
	}
	if gotPath != "/v1/orgs/org123/pipelines/abc/capture" {
		t.Errorf("path = %s, want /v1/orgs/org123/pipelines/abc/capture", gotPath)
	}
	if gotAuth != "tok" {
		t.Errorf("auth header = %q, want %q", gotAuth, "tok")
	}
	text := resultText(t, res)
	if !strings.Contains(text, `[{"source":"a","nodes":{}}]`) {
		t.Errorf("result = %s, want the raw array to survive into data", text)
	}
	if !strings.Contains(text, "guidance") {
		t.Errorf("result = %s, want guidance", text)
	}
}

func TestGetPipelineCaptureResultsTool_MissingConfID(t *testing.T) {
	_, handler := GetPipelineCaptureResultsTool(newTestClient("http://unused"))
	res, _ := handler(testCtx(), callToolRequest(map[string]any{}))
	if res == nil || !res.IsError {
		t.Fatalf("expected error result for missing conf_id, got %+v", res)
	}
}

// --- get_pipeline_capture_task ---

func TestGetPipelineCaptureTaskTool(t *testing.T) {
	var gotMethod, gotPath, gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath, gotAuth = r.Method, r.URL.Path, r.Header.Get("X-ED-API-Token")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"T1","org_id":"org123","config_id":"abc","expires_at":"2026-07-08T00:00:00.000Z","max_items":20}`))
	}))
	defer srv.Close()

	_, handler := GetPipelineCaptureTaskTool(newTestClient(srv.URL))
	res, err := handler(testCtx(), callToolRequest(map[string]any{"conf_id": "abc"}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotMethod != http.MethodGet {
		t.Errorf("method = %s, want GET", gotMethod)
	}
	if gotPath != "/v1/orgs/org123/pipelines/abc/capture/task" {
		t.Errorf("path = %s, want /v1/orgs/org123/pipelines/abc/capture/task", gotPath)
	}
	if gotAuth != "tok" {
		t.Errorf("auth header = %q, want %q", gotAuth, "tok")
	}
	text := resultText(t, res)
	if !strings.Contains(text, "T1") || !strings.Contains(text, "guidance") {
		t.Errorf("result = %s, want task id + guidance", text)
	}
}

func TestGetPipelineCaptureTaskTool_MissingConfID(t *testing.T) {
	_, handler := GetPipelineCaptureTaskTool(newTestClient("http://unused"))
	res, _ := handler(testCtx(), callToolRequest(map[string]any{}))
	if res == nil || !res.IsError {
		t.Fatalf("expected error result for missing conf_id, got %+v", res)
	}
}
