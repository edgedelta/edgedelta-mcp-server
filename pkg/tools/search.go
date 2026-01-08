package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/params"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

type SearchResponse struct {
	Data       json.RawMessage `json:"data"`
	TotalCount int             `json:"total_count"`
	Query      string          `json:"query_used,omitempty"`
	Guidance   *SearchGuidance `json:"guidance,omitempty"`
}

type SearchGuidance struct {
	ResultStatus string   `json:"result_status"`
	NextSteps    []string `json:"next_steps,omitempty"`
	Suggestions  []string `json:"suggestions,omitempty"`
}

// GetLogSearchTool creates a tool to search logs
func GetLogSearchTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_log_search",
			mcp.WithTitleAnnotation("Search Logs"),
			mcp.WithDescription(`Search logs using CQL (Common Query Language).

IMPORTANT: Call discover_schema tool with scope:"log" first to see available fields and values.

CQL Syntax:
- Field equals: field:"value"
- Multiple values: field:("val1" OR "val2")
- Negation: -field:"value"
- Full-text search: just type words without field prefix (supported)

NOT SUPPORTED: Regular expressions (/pattern/)

Common fields: service.name, severity_text, host.name, ed.tag

If empty results: verify field values with facet_options`),
			mcp.WithString("query",
				mcp.Description(`CQL query string. Examples:
- service.name:"api" AND severity_text:"ERROR"
- service.name:("api" OR "web") AND -severity_text:"DEBUG"
- @response.code > 400
- error OR exception (full-text search)
Use discover_schema tool or facet_options tool first to verify field names.`),
				mcp.DefaultString(""),
			),
			mcp.WithString("lookback",
				mcp.Description("Lookback period in GOLANG duration format. e.g. (1h, 15m, 24h). Either provide from/to or just lookback. Pass empty string to use from/to instead."),
				mcp.DefaultString("1h"),
			),
			mcp.WithString("from",
				mcp.Description("From datetime in ISO format 2006-01-02T15:04:05.000Z."),
				mcp.DefaultString(""),
			),
			mcp.WithString("to",
				mcp.Description("To datetime in ISO format 2006-01-02T15:04:05.000Z."),
				mcp.DefaultString(""),
			),
			mcp.WithNumber("limit",
				mcp.Description("Limits the number of logs in the response. Default is 20 for AI search, max is 1000. Can be negative to move cursor in prev direction."),
				mcp.DefaultNumber(20),
			),
			mcp.WithString("cursor",
				mcp.Description("Cursor provided from previous response, pass it to next request to move the cursor with given limit."),
				mcp.DefaultString(""),
			),
			mcp.WithString("order",
				mcp.Description("Order of the logs in the response, either 'ASC', 'asc', 'DESC' or 'desc'."),
				mcp.DefaultString("desc"),
			),
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

			// Build query parameters
			searchURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/logs/log_search/search", client.APIURL(), orgID))
			if err != nil {
				return nil, err
			}

			queryParams := searchURL.Query()
			if query, _ := params.Optional[string](request, "query"); query != "" {
				queryParams.Add("query", query)
			}

			if lookback, _ := params.Optional[string](request, "lookback"); lookback != "" {
				queryParams.Add("lookback", lookback)
			}

			if from, _ := params.Optional[string](request, "from"); from != "" {
				queryParams.Add("from", from)
			}

			if to, _ := params.Optional[string](request, "to"); to != "" {
				queryParams.Add("to", to)
			}

			if limit, _ := params.Optional[float64](request, "limit"); limit > 0 {
				queryParams.Add("limit", fmt.Sprintf("%v", limit))
			} else {
				// add always default limit if not provided
				queryParams.Add("limit", "20")
			}

			if cursor, _ := params.Optional[string](request, "cursor"); cursor != "" {
				queryParams.Add("cursor", cursor)
			}

			if order, _ := params.Optional[string](request, "order"); order != "" {
				queryParams.Add("order", order)
			}

			searchURL.RawQuery = queryParams.Encode()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, searchURL.String(), nil)
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %v", err)
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
				return nil, fmt.Errorf("failed to read response body: %v", err)
			}

			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to search logs, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			query, _ := params.Optional[string](request, "query")
			return formatSearchResponse(bodyBytes, query)
		}
}

func formatSearchResponse(bodyBytes []byte, query string) (*mcp.CallToolResult, error) {
	var genericResp map[string]any
	if err := json.Unmarshal(bodyBytes, &genericResp); err != nil {
		return mcp.NewToolResultText(string(bodyBytes)), nil
	}

	totalCount := 0
	if items, ok := genericResp["items"].([]any); ok {
		// ArchiveResponseV1 (logs) and EventResponse (events)
		totalCount = len(items)
	} else if stats, ok := genericResp["stats"].([]any); ok {
		// ClusterStatResponse (patterns)
		totalCount = len(stats)
	} else if records, ok := genericResp["records"].([]any); ok {
		// CommonTimeseriesResponse (metrics graph)
		totalCount = len(records)
	} else {
		// Check for formula-based response structure: {"A": {"records": [...]}}
		for _, v := range genericResp {
			if formulaResp, ok := v.(map[string]any); ok {
				if records, ok := formulaResp["records"].([]any); ok {
					totalCount += len(records)
				}
			}
		}
	}

	response := SearchResponse{
		Data:       bodyBytes,
		TotalCount: totalCount,
		Query:      query,
	}

	if totalCount == 0 {
		response.Guidance = &SearchGuidance{
			ResultStatus: "empty",
			NextSteps: []string{
				fmt.Sprintf("No results found for query: %s", query),
				"This is a valid signal - the data may not exist for this time range or filter.",
			},
			Suggestions: []string{
				"Verify field values with facet_options tool to ensure the values exist in your data",
				"Try a broader time range (e.g., lookback:\"24h\" or lookback:\"7d\")",
				"Simplify the query by removing filters one at a time",
				"Use validate_cql tool to check your query syntax, or build_cql tool to reconstruct from structured parameters",
			},
		}
	} else {
		response.Guidance = &SearchGuidance{
			ResultStatus: "success",
			NextSteps: []string{
				fmt.Sprintf("Found %d results", totalCount),
			},
		}
	}

	result, _ := json.Marshal(response)
	return mcp.NewToolResultText(string(result)), nil
}

// GetMetricSearchTool creates a tool to search metrics
func GetMetricSearchTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_metric_search",
			mcp.WithTitleAnnotation("Search Metrics"),
			mcp.WithDescription(`Search and aggregate metrics.

IMPORTANT: Before using this tool:
1. Use search_metrics tool to find the exact metric name (fuzzy search supported)
2. Or use facet_options tool with scope:"metric" and facet_path:"name" for exact names

Metric names must be EXACT - no wildcards or regex allowed.

Filter query uses CQL syntax:
- Field equals: field:"value"
- Multiple values: field:("val1" OR "val2")
- Negation: -field:"value"

NOT SUPPORTED for metrics:
- Full-text search (queries without field: prefix) - will cause error
- Regular expressions (/pattern/)

If empty results: verify metric name with search_metrics and filter values with facet_options`),
			mcp.WithString("metric_name",
				mcp.Description(`EXACT metric name (case-sensitive). Use search_metrics tool first to find available metric names. Examples: "http.request.duration", "system.cpu.usage". NO wildcards or regex.`),
				mcp.Required(),
			),
			mcp.WithString("aggregation_method",
				mcp.Description(`Aggregation method: "sum", "median", "count", "avg", "max", "min"`),
				mcp.DefaultString("sum"),
				mcp.Required(),
			),
			mcp.WithString("filter_query",
				mcp.Description(`CQL filter query. Use field:"value" syntax. Examples:
- service.name:"api"
- service.name:("api" OR "web") AND ed.tag:"prod"
Use "*" for no filter (default). Always verify field values with facet_options first.`),
				mcp.DefaultString("*"),
			),
			mcp.WithArray("group_by_keys",
				mcp.Description(`Grouping keys for the metric search. Use discover_schema tool with scope:"metric" or facet_options tool to see available keys. Common keys: service.name, host.name, ed.tag`),
				mcp.WithStringItems(),
			),
			mcp.WithNumber("rollup_period",
				mcp.Description("By default, rollup period will be handled according to the lookup period. However, one can specify it according to its own needs. This needs to be defined in seconds."),
			),
			mcp.WithString("lookback",
				mcp.Description("Lookback period in GOLANG duration format. e.g. (1h, 15m, 24h). Either provide from/to or just lookback. Pass empty string to use from/to instead."),
				mcp.DefaultString("1h"),
			),
			mcp.WithString("from",
				mcp.Description("From datetime in ISO format 2006-01-02T15:04:05.000Z."),
				mcp.DefaultString(""),
			),
			mcp.WithString("to",
				mcp.Description("To datetime in ISO format 2006-01-02T15:04:05.000Z."),
				mcp.DefaultString(""),
			),
			mcp.WithNumber("limit",
				mcp.Description("Limits the number of metrics in the response."),
			),
			mcp.WithString("order",
				mcp.Description("Order of the metrics in the response, either 'ASC', 'asc', 'DESC' or 'desc'."),
				mcp.DefaultString("desc"),
			),
			mcp.WithString("graph_type",
				mcp.Description(`Graph type of the query, valid options are "timeseries" and "table". Default is "timeseries".`),
				mcp.DefaultString("timeseries"),
			),
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

			// Build query parameters
			searchURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/graph", client.APIURL(), orgID))
			if err != nil {
				return nil, err
			}

			var metricName, aggregationMethod, filterQuery string
			var groupByKeys []string
			var rollupPeriod int
			if metric, _ := params.Optional[string](request, "metric_name"); metric != "" {
				metricName = metric
			} else {
				return nil, fmt.Errorf(`"metric_name" is required`)
			}

			if aggMethod, _ := params.Optional[string](request, "aggregation_method"); aggMethod != "" {
				aggregationMethod = aggMethod
			} else {
				aggregationMethod = "sum"
			}

			if query, _ := params.Optional[string](request, "filter_query"); query != "" {
				filterQuery = query
			} else {
				filterQuery = "*"
			}

			if groupBy := request.GetStringSlice("group_by_keys", nil); groupBy != nil {
				groupByKeys = groupBy
			}

			if rollup := request.GetInt("rollup_period", 0); rollup > 0 {
				rollupPeriod = rollup
			}

			cql := fmt.Sprintf("%s:%s{%s}", aggregationMethod, metricName, filterQuery)
			if len(groupByKeys) > 0 {
				cql += fmt.Sprintf(" by {%s}", strings.Join(groupByKeys, ","))
			}

			if rollupPeriod > 0 {
				cql += fmt.Sprintf(".rollup(%d)", rollupPeriod)
			}

			payload := map[string]any{
				"queries": map[string]any{
					"A": map[string]any{
						"scope": "metric",
						"query": cql,
					},
				},
				"formulas": map[string]any{
					"A": map[string]any{
						"formula": "A",
					},
				},
			}

			buffer := bytes.NewBuffer(nil)
			if err := json.NewEncoder(buffer).Encode(payload); err != nil {
				return nil, fmt.Errorf("failed to encode request body: %w", err)
			}

			queryParams := searchURL.Query()
			if lookback, _ := params.Optional[string](request, "lookback"); lookback != "" {
				queryParams.Add("lookback", lookback)
			}

			if from, _ := params.Optional[string](request, "from"); from != "" {
				queryParams.Add("from", from)
			}

			if to, _ := params.Optional[string](request, "to"); to != "" {
				queryParams.Add("to", to)
			}

			if limit, _ := params.Optional[float64](request, "limit"); limit > 0 {
				queryParams.Add("limit", fmt.Sprintf("%v", limit))
			}

			if order, _ := params.Optional[string](request, "order"); order != "" {
				queryParams.Add("order", order)
			}

			if graphType, _ := params.Optional[string](request, "graph_type"); graphType != "" {
				queryParams.Add("graph_type", graphType)
			}

			searchURL.RawQuery = queryParams.Encode()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, searchURL.String(), buffer)
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

			if resp.StatusCode != http.StatusMultiStatus {
				return nil, fmt.Errorf("failed to search metrics, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			queryDesc := fmt.Sprintf("metric:%s filter:%s", metricName, filterQuery)
			return formatSearchResponse(bodyBytes, queryDesc)
		}
}

// GetEventSearchTool creates a tool to search events
func GetEventSearchTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_event_search",
			mcp.WithTitleAnnotation("Search Events"),
			mcp.WithDescription(`Search events (anomalies, alerts, kubernetes events) using CQL.

IMPORTANT: Call discover_schema tool with scope:"event" first to see available event types and domains.

Common event queries:
- event.type:"pattern_anomaly" - Log anomaly detections
- event.type:"metric_threshold" - Metric alert triggers
- event.type:"log_threshold" - Log alert triggers
- event.domain:"Monitor Alerts" - All monitor-triggered events
- event.domain:"K8s" - Kubernetes events

CQL Syntax:
- Field equals: field:"value"
- Boolean: term1 AND term2
- Negation: -field:"value"
- Full-text search: just type words without field prefix (supported)

NOT SUPPORTED: Regular expressions (/pattern/)

If empty results: verify event.type/event.domain values with facet_options`),
			mcp.WithString("query",
				mcp.Description(`CQL query for events. Examples:
- event.type:"pattern_anomaly" (all anomalies)
- service.name:"api" AND event.type:"pattern_anomaly"
- event.domain:"Monitor Alerts"
Use discover_schema tool or facet_options tool to verify field values.`),
				mcp.DefaultString(""),
			),
			mcp.WithString("lookback",
				mcp.Description("Lookback period in golang duration format. e.g. '1h'. Either provide from/to or provide lookback/to or just lookback. Pass empty string to use from/to instead."),
				mcp.DefaultString("1h"),
			),
			mcp.WithString("from",
				mcp.Description("From datetime in ISO format 2006-01-02T15:04:05.000Z."),
				mcp.DefaultString(""),
			),
			mcp.WithString("to",
				mcp.Description("To datetime in ISO format 2006-01-02T15:04:05.000Z."),
				mcp.DefaultString(""),
			),
			mcp.WithNumber("limit",
				mcp.Description("Limits the number of events in the response. Default is 20 for AI search, max is 1000. Can be negative to move cursor in prev direction."),
				mcp.DefaultNumber(20),
			),
			mcp.WithString("cursor",
				mcp.Description("Cursor provided from previous response, pass it to next request to move the cursor with given limit."),
				mcp.DefaultString(""),
			),
			mcp.WithString("order",
				mcp.Description("Order of the events in the response, either 'ASC', 'asc', 'DESC' or 'desc'."),
				mcp.DefaultString("desc"),
			),
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

			// Build query parameters
			eventsURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/events/search", client.APIURL(), orgID))
			if err != nil {
				return nil, err
			}

			queryParams := eventsURL.Query()
			if query, _ := params.Optional[string](request, "query"); query != "" {
				queryParams.Add("query", query)
			}

			if lookback, _ := params.Optional[string](request, "lookback"); lookback != "" {
				queryParams.Add("lookback", lookback)
			}

			if from, _ := params.Optional[string](request, "from"); from != "" {
				queryParams.Add("from", from)
			}

			if to, _ := params.Optional[string](request, "to"); to != "" {
				queryParams.Add("to", to)
			}

			if limit, _ := params.Optional[float64](request, "limit"); limit > 0 {
				queryParams.Add("limit", fmt.Sprintf("%.0f", limit))
			} else {
				// add always default limit if not provided
				queryParams.Add("limit", "20")
			}

			if cursor, _ := params.Optional[string](request, "cursor"); cursor != "" {
				queryParams.Add("cursor", cursor)
			}

			if order, _ := params.Optional[string](request, "order"); order != "" {
				queryParams.Add("order", order)
			}

			eventsURL.RawQuery = queryParams.Encode()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, eventsURL.String(), nil)
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %v", err)
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
				return nil, fmt.Errorf("failed to read response body: %v", err)
			}

			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to search events, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			query, _ := params.Optional[string](request, "query")
			return formatSearchResponse(bodyBytes, query)
		}
}

// GetLogPatternsTool creates a tool to get pattern stats
func GetLogPatternsTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_log_patterns",
			mcp.WithTitleAnnotation("Get Log Patterns"),
			mcp.WithDescription(`Get top log patterns (message signatures) with statistics.

Returns pattern clusters with: count, proportion, sentiment (positive/negative/neutral), and delta (change over time).

IMPORTANT: Call discover_schema tool with scope:"pattern" first to see available fields.

CQL Syntax for query:
- Field equals: field:"value"
- Multiple values: field:("val1" OR "val2")
- Negation: -field:"value"
- Full-text search: just type words without field prefix (supported)

NOT SUPPORTED: Regular expressions (/pattern/)

Common fields: service.name, host.name, ed.tag

Note: Sentiment filtering is done via the negative parameter, not CQL.

If empty results: verify field values with facet_options`),
			mcp.WithString("query",
				mcp.Description(`CQL filter query. Examples:
- service.name:"api" (patterns from api service)
- service.name:("api" OR "web") AND ed.tag:"prod"
Use discover_schema tool or facet_options tool to verify field values.`),
				mcp.DefaultString(""),
			),
			mcp.WithString("lookback",
				mcp.Description("Lookback period in golang duration format. e.g. '1h'. Either provide from/to or provide lookback/to or just lookback. Pass empty string to use from/to instead."),
				mcp.DefaultString("1h"),
			),
			mcp.WithString("from",
				mcp.Description("From datetime in ISO format 2006-01-02T15:04:05.000Z."),
				mcp.DefaultString(""),
			),
			mcp.WithString("to",
				mcp.Description("To datetime in ISO format 2006-01-02T15:04:05.000Z."),
				mcp.DefaultString(""),
			),
			mcp.WithBoolean("summary",
				mcp.Description("If summary true call returns up to 50 interesting clusters with 10 top anomaly, top/bottom delta, top/bottom count. Param size is ignored."),
			),
			mcp.WithNumber("limit",
				mcp.Description("Max number of clusters in response. For AI search, limit should be 20."),
				mcp.DefaultNumber(20),
			),
			mcp.WithString("offset",
				mcp.Description("Comma separated offsets for delta stat calculation. Each offset is in golang duration format. Default value is lookback duration. e.g. '24h'."),
				mcp.DefaultString(""),
			),
			mcp.WithBoolean("negative",
				mcp.Description("Negative param is used to get negative sentiments."),
			),
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

			// Build query parameters
			statsURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/clustering/stats", client.APIURL(), orgID))
			if err != nil {
				return nil, err
			}

			queryParams := statsURL.Query()
			if query, _ := params.Optional[string](request, "query"); query != "" {
				queryParams.Add("query", query)
			}

			if lookback, _ := params.Optional[string](request, "lookback"); lookback != "" {
				queryParams.Add("lookback", lookback)
			}

			if from, _ := params.Optional[string](request, "from"); from != "" {
				queryParams.Add("from", from)
			}

			if to, _ := params.Optional[string](request, "to"); to != "" {
				queryParams.Add("to", to)
			}

			if summary, _ := params.Optional[bool](request, "summary"); summary {
				queryParams.Add("summary", "true")
			}

			if limit, _ := params.Optional[float64](request, "limit"); limit > 0 {
				queryParams.Add("limit", fmt.Sprintf("%.0f", limit))
			} else {
				// add always default limit if not provided
				queryParams.Add("limit", "20")
			}

			if offset, _ := params.Optional[string](request, "offset"); offset != "" {
				queryParams.Add("offset", offset)
			}
			if negative, _ := params.Optional[bool](request, "negative"); negative {
				queryParams.Add("negative", "true")
			}

			statsURL.RawQuery = queryParams.Encode()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, statsURL.String(), nil)
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %v", err)
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
				return nil, fmt.Errorf("failed to read response body: %v", err)
			}

			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to get clustering stats, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			query, _ := params.Optional[string](request, "query")
			return formatSearchResponse(bodyBytes, query)
		}
}

// GetTraceTimelineTool creates a tool to fetch spans suitable for the TraceTimeline component
func GetTraceTimelineTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_trace_timeline",
			mcp.WithTitleAnnotation("Get Trace Timeline"),
			mcp.WithDescription(`Fetch spans (OTel) for a timeline view.

IMPORTANT: Call discover_schema tool with scope:"trace" first to see available fields.

CQL Syntax:
- Field equals: field:"value"
- Multiple values: field:("val1" OR "val2")
- Negation: -field:"value"

NOT SUPPORTED for traces:
- Full-text search (queries without field: prefix) - will cause error
- Regular expressions (/pattern/)

Common fields: service.name, status.code, span.kind, ed.tag

If empty results: verify field values with facet_options`),
			mcp.WithString("query",
				mcp.Description(`CQL filter query (field:value syntax required). Examples:
- service.name:"api"
- span.kind:"server"
- status.code:"ERROR"
- ed.tag:"prod" AND service.name:("api" OR "web")
NOTE: Full-text search is NOT supported for traces.`),
				mcp.DefaultString(""),
			),
			mcp.WithString("lookback",
				mcp.Description("Lookback period in Go duration format (e.g., 1h, 15m, 24h). Provide either lookback or from/to. Pass empty string to use from/to instead."),
				mcp.DefaultString("1h"),
			),
			mcp.WithString("from",
				mcp.Description("From datetime (ISO 8601: 2006-01-02T15:04:05.000Z). Use with 'to' when not using lookback."),
				mcp.DefaultString(""),
			),
			mcp.WithString("to",
				mcp.Description("To datetime (ISO 8601: 2006-01-02T15:04:05.000Z). Use with 'from' when not using lookback."),
				mcp.DefaultString(""),
			),
			mcp.WithNumber("limit",
				mcp.Description("Maximum number of items to return per page (default 20, max 1000)."),
				mcp.DefaultNumber(20),
			),
			mcp.WithString("cursor",
				mcp.Description("Pagination cursor from a previous response (use next_cursor/previous_cursor)."),
				mcp.DefaultString(""),
			),
			mcp.WithString("order",
				mcp.Description("Sort order: 'ASC' or 'DESC' (case-insensitive)."),
				mcp.DefaultString("desc"),
			),
			mcp.WithBoolean("include_child_spans",
				mcp.Description("If true, include child spans for matched spans to provide full trace context."),
			),
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

			// Build query parameters for traces search
			tracesURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/traces", client.APIURL(), orgID))
			if err != nil {
				return nil, err
			}

			queryParams := tracesURL.Query()
			var query string
			if q, _ := params.Optional[string](request, "query"); q != "" {
				query = q
				queryParams.Add("query", query)
			}

			if lookback, _ := params.Optional[string](request, "lookback"); lookback != "" {
				queryParams.Add("lookback", lookback)
			}

			if from, _ := params.Optional[string](request, "from"); from != "" {
				queryParams.Add("from", from)
			}

			if to, _ := params.Optional[string](request, "to"); to != "" {
				queryParams.Add("to", to)
			}

			if limit, _ := params.Optional[float64](request, "limit"); limit > 0 {
				queryParams.Add("limit", fmt.Sprintf("%.0f", limit))
			} else {
				queryParams.Add("limit", "20")
			}

			if cursor, _ := params.Optional[string](request, "cursor"); cursor != "" {
				queryParams.Add("cursor", cursor)
			}

			if order, _ := params.Optional[string](request, "order"); order != "" {
				queryParams.Add("order", order)
			}

			if include, _ := params.Optional[bool](request, "include_child_spans"); include {
				queryParams.Add("include_child_spans", "true")
			}

			tracesURL.RawQuery = queryParams.Encode()
			req, err := http.NewRequestWithContext(ctx, http.MethodGet, tracesURL.String(), nil)
			if err != nil {
				return nil, fmt.Errorf("failed to create request: %v", err)
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
				return nil, fmt.Errorf("failed to read response body: %v", err)
			}

			if resp.StatusCode != http.StatusOK {
				return nil, fmt.Errorf("failed to search traces, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			return formatSearchResponse(bodyBytes, query)
		}
}
