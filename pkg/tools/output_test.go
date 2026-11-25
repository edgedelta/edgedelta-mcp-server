package tools

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type apiResponse struct {
	status int
	body   string
}

// fakeAPI answers each Edge Delta API path the tools call with a typical response, overridden by overrides.
func fakeAPI(t *testing.T, overrides map[string]apiResponse) *httptest.Server {
	t.Helper()
	timeseries := `{"from":"2026-11-25T00:00:00Z","to":"2026-11-25T01:00:00Z","window":"1m","keys":["service.name"],"records":[{"keys":{"service.name":"api"},"timeseries":[{"timestamp":1,"value":2}]}]}`
	responses := map[string]apiResponse{
		"/logs/log_search/search":           {200, `{"query_id":"q1","items":[{"id":"l1","timestamp":1764028800000,"severity_text":"ERROR","body":"boom","resource":{"service.name":"api"},"attributes":{}}],"next_cursor":"c2"}`},
		"/logs/log_search/graph":            {200, `{"records":[{"keys":{"service.name":"api"}}]}`},
		"/graph":                            {207, timeseries},
		"/events/search":                    {200, `{"query_id":"q1","items":[{"id":"e1"}],"next_cursor":""}`},
		"/clustering/stats":                 {200, `{"stats":[{"pattern":"user * logged in","count":3}]}`},
		"/traces":                           {200, `{"items":[{"span_id":"s1"}]}`},
		"/pipelines":                        {200, `[{"id":"c1","tag":"prod","creator":"a@example.com","created":"2026-11-01","updated":"2026-11-20","environment":"Kubernetes","fleet_type":"Edge","status":"running"}]`},
		"/confs":                            {200, `[{"id":"c1","tag":"AI-Connector-Telemetry-Pipeline","fleet_type":"IngestionPipeline"}]`},
		"/confs/c1":                         {200, `{"id":"c1","tag":"AI-Connector-Telemetry-Pipeline","fleet_type":"IngestionPipeline","content":"nodes:\n- name: http_in\n  type: http_ingestion_input\n"}`},
		"/confs/validate":                   {200, `{"valid":false,"reason":"nodes: required"}`},
		"/pipelines/c1/history":             {200, `[{"version":"1700000000000","description":"first"}]`},
		"/pipelines/c1/deploy/1":            {200, `{"status":"deployed"}`},
		"/pipelines/c1/add_source":          {200, `{"version":"2"}`},
		"/pipelines/c1/save":                {200, `{"version":"2"}`},
		"/pipelines/c1/revert/1":            {200, `{"version":"3"}`},
		"/pipelines/c1/capture/task":        {200, `{"id":"T1","max_items":20}`},
		"/pipelines/capture/task/T1/status": {200, `{"capture_status":[{"agent":"a1","status":"success"}]}`},
		"/pipelines/c1/capture":             {200, `{"n1":{"before":[],"after":[]}}`},
		"/pipelines/c1/test/ottl":           {200, `{"items":["{}"]}`},
		"/pipelines/c1/test/node":           {200, `{"default":["{}"]}`},
		"/dashboards":                       {200, `[{"id":"d1","name":"Overview"}]`},
		"/dashboards/d1":                    {200, `{"id":"d1","name":"Overview","definition":{}}`},
		"/facets":                           {200, `{"builtin":[{"name":"Service","path":"service.name","scope":"log"}],"userDefined":[]}`},
		"/facet_options":                    {200, `{"name":"Service","path":"service.name","options":[{"name":"api","count":3},{"name":"apiserver.requests","count":1}]}`},
		"/facet_keys":                       {200, `[{"key":"service.name"},{"key":"severity_text"}]`},
		"/ingestion_endpoints":              {200, `{"https":{"base_url":"https://in.example.com","path_for_data_type":{"logs":"/v1/logs"},"sample_data":{"logs":"{}"},"test_commands":{"logs":"curl -d {} 'https://in.example.com/v1/logs?token={TOKEN}'"}}}`},
		"/ingestion_token":                  {200, `{"raw_token":"secret","conf_id":"c1","node_name":"http_in"}`},
	}
	for path, r := range overrides {
		responses[path] = r
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r2, ok := responses[strings.TrimPrefix(r.URL.Path, "/v1/orgs/org123")]
		if !ok {
			t.Errorf("unexpected API call: %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(r2.status)
		_, _ = w.Write([]byte(r2.body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

type toolFunc func(Client) (mcp.Tool, server.ToolHandlerFunc)

func pair(tool mcp.Tool, handler func(Client) server.ToolHandlerFunc) toolFunc {
	return func(c Client) (mcp.Tool, server.ToolHandlerFunc) { return tool, handler(c) }
}

func noClient(f func() (mcp.Tool, server.ToolHandlerFunc)) toolFunc {
	return func(Client) (mcp.Tool, server.ToolHandlerFunc) { return f() }
}

// TestStructuredContentMatchesOutputSchema calls each tool against a fake API and checks that its
// structuredContent validates against its outputSchema, and that its text content is the same JSON.
func TestStructuredContentMatchesOutputSchema(t *testing.T) {
	empty := map[string]apiResponse{
		"/logs/log_search/search": {200, `{"query_id":"q1","items":[],"next_cursor":""}`},
		"/graph":                  {207, `{}`},
		"/facets":                 {200, `{"builtin":[],"userDefined":[]}`},
		"/facet_options":          {200, `{"name":"Service","options":[]}`},
		"/pipelines":              {200, `[]`},
	}
	tests := []struct {
		name      string
		tool      toolFunc
		args      map[string]any
		overrides map[string]apiResponse
		want      string // in structuredContent, so the case checks a result that isn't empty
	}{
		{"get_log_search", GetLogSearchTool, map[string]any{"query": `service.name:"api"`}, nil, `"total_count":1`},
		{"get_log_search with no results", GetLogSearchTool, map[string]any{"query": `service.name:"none"`}, empty, ""},
		{"get_log_search with a body that isn't JSON", GetLogSearchTool, nil, map[string]apiResponse{"/logs/log_search/search": {200, "not json"}}, `"data":"not json"`},
		{"get_metric_search", GetMetricSearchTool, map[string]any{"metric_name": "cpu.usage", "graph_type": "timeseries"}, nil, ""},
		{"get_metric_search with no results", GetMetricSearchTool, map[string]any{"metric_name": "cpu.usage", "graph_type": "timeseries"}, empty, ""},
		{"get_event_search", GetEventSearchTool, nil, nil, ""},
		{"get_log_patterns", GetLogPatternsTool, nil, nil, ""},
		{"get_trace_timeline", GetTraceTimelineTool, nil, nil, ""},
		{"get_log_graph", GetLogGraphTool, map[string]any{"query": `service.name:"api"`}, nil, ""},
		{"get_log_graph with no data", GetLogGraphTool, map[string]any{"query": `service.name:"api"`}, empty, ""},
		{"get_log_graph with a body that isn't JSON", GetLogGraphTool, map[string]any{"query": `service.name:"api"`}, map[string]apiResponse{"/graph": {207, "not json"}}, `"data":"not json"`},
		{"get_metric_graph", GetMetricGraphTool, map[string]any{"metric_name": "cpu.usage", "graph_type": "timeseries"}, nil, ""},
		{"get_metric_graph with formulas", GetMetricGraphTool, map[string]any{"metric_name": "cpu.usage", "graph_type": "timeseries"}, map[string]apiResponse{"/graph": {207, `{"R1":{"records":[{"keys":{}}]}}`}}, ""},
		{"get_trace_graph", GetTraceGraphTool, map[string]any{"query": `service.name:"api"`}, nil, ""},
		{"get_pattern_graph", GetPatternGraphTool, map[string]any{"query": `service.name:"api"`}, nil, ""},
		{"get_pipelines", GetPipelinesTool, nil, nil, `"id":"c1"`},
		{"get_pipelines with none", GetPipelinesTool, nil, empty, ""},
		{"get_pipeline_config", GetPipelineConfigTool, map[string]any{"conf_id": "c1"}, nil, ""},
		{"get_pipeline_history", GetPipelineHistoryTool, map[string]any{"conf_id": "c1"}, nil, ""},
		{"deploy_pipeline", DeployPipelineTool, map[string]any{"conf_id": "c1", "version": "1"}, nil, ""},
		{"add_pipeline_source", AddPipelineSourceTool, map[string]any{"conf_id": "c1", "node": map[string]any{"name": "in", "type": "http_input"}}, nil, ""},
		{"validate_pipeline_config", ValidatePipelineConfigTool, map[string]any{"content": "version: v3"}, nil, ""},
		{"save_pipeline_config", SavePipelineConfigTool, map[string]any{"conf_id": "c1", "content": "version: v3", "description": "d"}, nil, ""},
		{"revert_pipeline", RevertPipelineTool, map[string]any{"conf_id": "c1", "version": "1"}, nil, ""},
		{"start_pipeline_capture", StartPipelineCaptureTool, map[string]any{"conf_id": "c1"}, nil, ""},
		{"get_pipeline_capture_status", GetPipelineCaptureStatusTool, map[string]any{"task_id": "T1"}, nil, ""},
		{"get_pipeline_capture_results", GetPipelineCaptureResultsTool, map[string]any{"conf_id": "c1"}, nil, ""},
		{"get_pipeline_capture_task", GetPipelineCaptureTaskTool, map[string]any{"conf_id": "c1"}, nil, ""},
		{"test_pipeline_ottl", TestPipelineOTTLTool, map[string]any{"conf_id": "c1", "statements": `set(attributes["a"], "b")`, "items": []any{"{}"}}, nil, ""},
		{"test_pipeline_node", TestPipelineNodeTool, map[string]any{"conf_id": "c1", "node": map[string]any{"id": "n1", "type": "ottl_transform"}, "items": []any{"{}"}}, nil, ""},
		{"get_ingestion_endpoint", GetIngestionEndpointTool, nil, nil, `"endpoints":[{`},
		{"get_all_dashboards", GetAllDashboardsTool, nil, nil, ""},
		{"get_dashboard", GetDashboardTool, map[string]any{"dashboard_id": "d1"}, nil, ""},
		{"facets", pair(FacetsTool, FacetsToolHandler), map[string]any{"scope": "log"}, nil, `"path":"service.name"`},
		{"facets with none", pair(FacetsTool, FacetsToolHandler), map[string]any{"scope": "log"}, empty, ""},
		{"facet_options", pair(FacetOptionsTool, FacetOptionsToolHandler), map[string]any{"scope": "log", "facet_path": "service.name"}, nil, `"options":[{`},
		{"facet_options with none", pair(FacetOptionsTool, FacetOptionsToolHandler), map[string]any{"scope": "log", "facet_path": "service.name"}, empty, ""},
		{"discover_schema", GetDiscoverSchemaTool, map[string]any{"scope": "log"}, nil, `"facet_keys":[{"key"`},
		{"discover_schema without facet keys", GetDiscoverSchemaTool, map[string]any{"scope": "log"}, map[string]apiResponse{"/facet_keys": {500, "down"}}, ""},
		{"search_metrics", GetSearchMetricsTool, map[string]any{"pattern": "api"}, nil, `"matches":[{`},
		{"search_metrics with no match", GetSearchMetricsTool, map[string]any{"pattern": "nothing"}, nil, ""},
		{"validate_cql", noClient(GetValidateCQLTool), map[string]any{"query": `service.name:"api"`, "scope": "log"}, nil, ""},
		{"validate_cql with an invalid query", noClient(GetValidateCQLTool), map[string]any{"query": `/regex/`, "scope": "log"}, nil, `"errors":[`},
		{"build_cql", GetBuildCQLTool, map[string]any{"scope": "log", "filters": map[string]any{"service.name": "api", "nope": "x"}}, nil, `"unknown_fields":[`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool, handler := tt.tool(newTestClient(fakeAPI(t, tt.overrides).URL))
			schema := resolveOutputSchema(t, tool)

			res, err := handler(testCtx(), callToolRequest(tt.args))
			if err != nil {
				t.Fatalf("handler: %v", err)
			}
			if res.IsError {
				t.Fatalf("error result: %s", resultText(t, res))
			}
			structured, err := json.Marshal(res.StructuredContent)
			if err != nil {
				t.Fatalf("marshal structuredContent: %v", err)
			}
			var instance any
			if err := json.Unmarshal(structured, &instance); err != nil {
				t.Fatalf("structuredContent %s: %v", structured, err)
			}
			if err := schema.Validate(instance); err != nil {
				t.Errorf("structuredContent doesn't match outputSchema: %v\n%s", err, structured)
			}
			if !strings.Contains(string(structured), tt.want) {
				t.Errorf("structuredContent = %s, want it to contain %s", structured, tt.want)
			}
			if text := resultText(t, res); text != string(structured) {
				t.Errorf("text content = %s, want the structuredContent %s", text, structured)
			}
		})
	}
}

func resolveOutputSchema(t *testing.T, tool mcp.Tool) *jsonschema.Resolved {
	t.Helper()
	if tool.RawOutputSchema == nil {
		t.Fatalf("tool %s has no outputSchema", tool.Name)
	}
	var schema jsonschema.Schema
	if err := json.Unmarshal(tool.RawOutputSchema, &schema); err != nil {
		t.Fatalf("outputSchema: %v", err)
	}
	if schema.Type != "object" {
		t.Fatalf("outputSchema type = %q, want object", schema.Type)
	}
	resolved, err := schema.Resolve(nil)
	if err != nil {
		t.Fatalf("resolve outputSchema: %v", err)
	}
	return resolved
}

// TestOutputSchemaDescribesData checks that a tool's own description of data replaces the shared one.
func TestOutputSchemaDescribesData(t *testing.T) {
	tool, _ := GetLogPatternsTool(newTestClient("http://unused"))
	var schema struct {
		Properties map[string]struct {
			Description string `json:"description"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(tool.RawOutputSchema, &schema); err != nil {
		t.Fatal(err)
	}
	if got := schema.Properties["data"].Description; !strings.Contains(got, "patterns") {
		t.Errorf("data description = %q, want get_log_patterns' own", got)
	}
	if got := schema.Properties["total_count"].Description; got == "" {
		t.Error("total_count has no description")
	}
}

func TestOutputSchemaForRejects(t *testing.T) {
	if _, err := outputSchemaFor[[]string](); err == nil {
		t.Error("an array schema was accepted")
	}
	if _, err := outputSchemaFor[SearchResponse](doc("nope", "x")); err == nil {
		t.Error("a doc for a missing property was accepted")
	}
}
