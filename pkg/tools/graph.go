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

// GetLogGraphTool creates a tool to render a graph from logs
func GetLogGraphTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_log_graph",
			mcp.WithDescription(`Render a graph from logs`),
			mcp.WithString("query",
				mcp.Description(`Log facets are to target in the tool. service.name is one of the keys, you must get "services://list" resource before setting service.name, if you don't set it, it is for all services. Keys are anded together and values in the keys are ORed. You can also mix and match with use other keys via using "facet-keys://logs" resource. Examples;
service.name:"ingestor"
ed.tag:"prod" AND -host.name:"server1.mydomain.com"
service.name:("api" OR "web")
Default is "*" to include all logs`),
				mcp.DefaultString("*"),
				mcp.Required(),
			),
			mcp.WithString("lookback",
				mcp.Description("Lookback period in GOLANG duration format. e.g. (1h, 15m, 24h). Either provide from/to or just lookback"),
				mcp.DefaultString("1h"),
			),
			mcp.WithString("from",
				mcp.Description("From datetime in ISO format 2006-01-02T15:04:05.000Z"),
				mcp.DefaultString(""),
			),
			mcp.WithString("to",
				mcp.Description("To datetime in ISO format 2006-01-02T15:04:05.000Z"),
				mcp.DefaultString(""),
			),
			mcp.WithNumber("limit",
				mcp.Description("Limits the number of logs in the response"),
			),
			mcp.WithString("order",
				mcp.Description("Order of the logs in the response, either 'ASC', 'asc', 'DESC' or 'desc'"),
				mcp.DefaultString("desc"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(false),
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

			var query string
			if q, _ := params.Optional[string](request, "query"); q != "" {
				query = q
			} else {
				return nil, fmt.Errorf(`"query" is required`)
			}

			payload := map[string]any{
				"queries": map[string]any{
					"Q1": map[string]any{
						"scope": "log",
						"query": query,
					},
				},
				"formulas": map[string]any{
					"R1": map[string]any{
						"formula": "Q1",
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

			if limit := request.GetInt("limit", 0); limit > 0 {
				queryParams.Add("limit", fmt.Sprintf("%d", limit))
			}

			if order, _ := params.Optional[string](request, "order"); order != "" {
				queryParams.Add("order", order)
			}

			searchURL.RawQuery = queryParams.Encode()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, searchURL.String(), buffer)
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

			if resp.StatusCode != http.StatusMultiStatus {
				return nil, fmt.Errorf("failed to search logs, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			return mcp.NewToolResultText(string(bodyBytes)), nil
		}
}

// GetMetricGraphTool creates a tool to render a graph from metrics
func GetMetricGraphTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_metric_graph",
			mcp.WithDescription(`Render a graph from metrics`),
			mcp.WithString("metric_name",
				mcp.Description(`Metric name that will be used for constructing the graph. Wildcards and regexes are not supported, it should be a plain name. For available metric names, please use "facet_options" tool with "metric" scope and "name" facet path.`),
				mcp.Required(),
			),
			mcp.WithString("aggregation_method",
				mcp.Description(`Aggregation method that will apply while obtaining the result as metrics gets rolled up. "sum", "median", "count", "avg" (for average), "max" (for maximum) and "min" (for minimum) are the valid options`),
				mcp.DefaultString("sum"),
				mcp.Required(),
			),
			mcp.WithString("filter_query",
				mcp.Description(`Metric facets are to target in the tool. service.name is one of the keys, you must get "services://list" resource before setting service.name, if you don't set it, it is for all services. Keys are anded together and values in the keys are ORed. You can also mix and match with use other keys via using "facet-keys://metrics" resource. Examples;
service.name:"ingestor"
ed.tag:"prod" AND -host.name:"server1.mydomain.com"
service.name:("api" OR "web")
Default is "*" to include all metrics`),
				mcp.DefaultString("*"),
			),
			mcp.WithArray("group_by_keys",
				mcp.Description(`Grouping keys that will be used for constructing the graph. One can refer "facet-keys://metrics" resource for available keys`),
				mcp.WithStringItems(),
			),
			mcp.WithNumber("rollup_period",
				mcp.Description("By default, rollup period will be handled according to the lookup period. However, one can specify it according to its own needs. This needs to be defined in seconds"),
			),
			mcp.WithString("lookback",
				mcp.Description("Lookback period in GOLANG duration format. e.g. (1h, 15m, 24h). Either provide from/to or just lookback"),
				mcp.DefaultString("1h"),
			),
			mcp.WithString("from",
				mcp.Description("From datetime in ISO format 2006-01-02T15:04:05.000Z"),
				mcp.DefaultString(""),
			),
			mcp.WithString("to",
				mcp.Description("To datetime in ISO format 2006-01-02T15:04:05.000Z"),
				mcp.DefaultString(""),
			),
			mcp.WithNumber("limit",
				mcp.Description("Limits the number of metrics in the response"),
			),
			mcp.WithString("order",
				mcp.Description("Order of the metrics in the response, either 'ASC', 'asc', 'DESC' or 'desc'"),
				mcp.DefaultString("desc"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(false),
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
					"Q1": map[string]any{
						"scope": "metric",
						"query": cql,
					},
				},
				"formulas": map[string]any{
					"R1": map[string]any{
						"formula": "Q1",
					},
				},
			}

			buffer := bytes.NewBuffer(nil)
			if err := json.NewEncoder(buffer).Encode(payload); err != nil {
				return nil, fmt.Errorf("failed to encode request body: %w", err)
			}

			queryParams := searchURL.Query()
			queryParams.Add("graph_type", "timeseries")
			if lookback, _ := params.Optional[string](request, "lookback"); lookback != "" {
				queryParams.Add("lookback", lookback)
			}

			if from, _ := params.Optional[string](request, "from"); from != "" {
				queryParams.Add("from", from)
			}

			if to, _ := params.Optional[string](request, "to"); to != "" {
				queryParams.Add("to", to)
			}

			if limit := request.GetInt("limit", 0); limit > 0 {
				queryParams.Add("limit", fmt.Sprintf("%d", limit))
			}

			if order, _ := params.Optional[string](request, "order"); order != "" {
				queryParams.Add("order", order)
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
				return nil, fmt.Errorf("failed to search logs, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			return mcp.NewToolResultText(string(bodyBytes)), nil
		}
}

// GetTraceGraphTool creates a tool to render a graph from traces
func GetTraceGraphTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_trace_graph",
			mcp.WithDescription(`Render a graph from traces`),
			mcp.WithString("query",
				mcp.Description(`Trace facets are to target in the tool. service.name is one of the keys, you must get "services://list" resource before setting service.name, if you don't set it, it is for all services. Keys are anded together and values in the keys are ORed. You can also mix and match with use other keys via using "facet-keys://traces" resource. Examples;
service.name:"ingestor"
ed.tag:"prod" AND -host.name:"server1.mydomain.com"
service.name:("api" OR "web")
Default is "*" to include all traces`),
				mcp.DefaultString("*"),
				mcp.Required(),
			),
			mcp.WithString("data_type",
				mcp.Description(`Data type that will be used for value of traces. "request" (for request count) and "latency" (for P50 and P95 values of percentiles) are the valid options`),
				mcp.DefaultString("request"),
				mcp.Required(),
			),
			mcp.WithBoolean("include_child_spans",
				mcp.Description(`Whether to include or not child spans while returning the values. Should be set to true if include behavior is desired`),
				mcp.DefaultBool(false),
			),
			mcp.WithString("lookback",
				mcp.Description("Lookback period in GOLANG duration format. e.g. (1h, 15m, 24h). Either provide from/to or just lookback"),
				mcp.DefaultString("1h"),
			),
			mcp.WithString("from",
				mcp.Description("From datetime in ISO format 2006-01-02T15:04:05.000Z"),
				mcp.DefaultString(""),
			),
			mcp.WithString("to",
				mcp.Description("To datetime in ISO format 2006-01-02T15:04:05.000Z"),
				mcp.DefaultString(""),
			),
			mcp.WithNumber("limit",
				mcp.Description("Limits the number of traces in the response"),
			),
			mcp.WithString("order",
				mcp.Description("Order of the traces in the response, either 'ASC', 'asc', 'DESC' or 'desc'"),
				mcp.DefaultString("desc"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(false),
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

			var query, dataType string
			var includeChildSpans bool
			if q, _ := params.Optional[string](request, "query"); q != "" {
				query = q
			} else {
				return nil, fmt.Errorf(`"query" is required`)
			}

			if dType, _ := params.Optional[string](request, "data_type"); dType != "" {
				dataType = dType
			} else {
				dataType = "request"
			}

			if incChildSpans, _ := params.Optional[bool](request, "include_child_spans"); incChildSpans {
				includeChildSpans = true
			}

			payload := map[string]any{
				"queries": map[string]any{
					"Q1": map[string]any{
						"scope":             "trace",
						"query":             query,
						"dataType":          dataType,
						"includeChildSpans": includeChildSpans,
					},
				},
				"formulas": map[string]any{
					"R1": map[string]string{
						"formula": "Q1",
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

			if limit := request.GetInt("limit", 0); limit > 0 {
				queryParams.Add("limit", fmt.Sprintf("%d", limit))
			}

			if order, _ := params.Optional[string](request, "order"); order != "" {
				queryParams.Add("order", order)
			}

			searchURL.RawQuery = queryParams.Encode()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, searchURL.String(), buffer)
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

			if resp.StatusCode != http.StatusMultiStatus {
				return nil, fmt.Errorf("failed to graph traces, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			return mcp.NewToolResultText(string(bodyBytes)), nil
		}
}

// GetPatternGraphTool creates a tool to render a graph from patterns
func GetPatternGraphTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_pattern_graph",
			mcp.WithDescription(`Render a graph from patterns`),
			mcp.WithString("query",
				mcp.Description(`Pattern facets are to target in the tool. service.name is one of the keys, you must get "services://list" resource before setting service.name, if you don't set it, it is for all services. Keys are anded together and values in the keys are ORed. You can also mix and match with use other keys via using "facet-keys://patterns" resource. Examples;
service.name:"ingestor"
ed.tag:"prod" AND -host.name:"server1.mydomain.com"
service.name:("api" OR "web")
Default is "*" to include all patterns`),
				mcp.DefaultString("*"),
				mcp.Required(),
			),
			mcp.WithBoolean("omit_zero_patterns",
				mcp.Description(`Whether to omit patterns with zero samples or not`),
				mcp.DefaultBool(false),
			),
			mcp.WithBoolean("include_negative_patterns",
				mcp.Description(`Whether to include patterns with negative sentiment value or not`),
				mcp.DefaultBool(false),
			),
			mcp.WithBoolean("include_missing_under_other",
				mcp.Description(`Whether to include missing values under "Other" or not`),
				mcp.DefaultBool(false),
			),
			mcp.WithString("volatility",
				mcp.Description(`Volatility filter for patterns. "all" (no filtering), "new" (new patterns according to volatility offset), "existing" (pre-existing patterns according to volatility offset) and "gone" (gone patterns according to volatility offset) are the valid options`),
				mcp.DefaultString("all"),
			),
			mcp.WithString("volatility_offset",
				mcp.Description(`Offset to be used by volatility parameter. Should be in GOLANG duration format. e.g. (1h, 15m, 24h)`),
				mcp.DefaultString("24h"),
			),
			mcp.WithString("lookback",
				mcp.Description("Lookback period in GOLANG duration format. e.g. (1h, 15m, 24h). Either provide from/to or just lookback"),
				mcp.DefaultString("1h"),
			),
			mcp.WithString("from",
				mcp.Description("From datetime in ISO format 2006-01-02T15:04:05.000Z"),
				mcp.DefaultString(""),
			),
			mcp.WithString("to",
				mcp.Description("To datetime in ISO format 2006-01-02T15:04:05.000Z"),
				mcp.DefaultString(""),
			),
			mcp.WithNumber("limit",
				mcp.Description("Limits the number of patterns in the response"),
			),
			mcp.WithString("order",
				mcp.Description("Order of the patterns in the response, either 'ASC', 'asc', 'DESC' or 'desc'"),
				mcp.DefaultString("desc"),
			),
			mcp.WithReadOnlyHintAnnotation(true),
			mcp.WithIdempotentHintAnnotation(false),
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

			var query, volatility, volatilityOffset string
			var omitZeroPatterns, includeNegativePatterns, includeMissingUnderOther bool
			if q, _ := params.Optional[string](request, "query"); q != "" {
				query = q
			} else {
				return nil, fmt.Errorf(`"query" is required`)
			}

			if omitZero, _ := params.Optional[bool](request, "omit_zero_patterns"); omitZero {
				omitZeroPatterns = true
			}

			if incNegative, _ := params.Optional[bool](request, "include_negative_patterns"); incNegative {
				includeNegativePatterns = true
			}

			if incMissingUnderOther, _ := params.Optional[bool](request, "include_negative_patterns"); incMissingUnderOther {
				includeMissingUnderOther = true
			}

			if vol, _ := params.Optional[string](request, "volatility"); vol != "" {
				volatility = vol
			} else {
				volatility = "all"
			}

			if volOffset, _ := params.Optional[string](request, "volatility_offset"); volOffset != "" {
				volatilityOffset = volOffset
			} else {
				volatilityOffset = "24h"
			}

			payload := map[string]any{
				"queries": map[string]any{
					"Q1": map[string]any{
						"scope":        "pattern",
						"query":        query,
						"omitZero":     omitZeroPatterns,
						"negative":     includeNegativePatterns,
						"includeOther": includeMissingUnderOther,
						"volatility":   volatility,
						"offset":       volatilityOffset,
					},
				},
				"formulas": map[string]any{
					"R1": map[string]string{
						"formula": "Q1",
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

			if limit := request.GetInt("limit", 0); limit > 0 {
				queryParams.Add("limit", fmt.Sprintf("%d", limit))
			}

			if order, _ := params.Optional[string](request, "order"); order != "" {
				queryParams.Add("order", order)
			}

			searchURL.RawQuery = queryParams.Encode()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, searchURL.String(), buffer)
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

			if resp.StatusCode != http.StatusMultiStatus {
				return nil, fmt.Errorf("failed to graph patterns, status code %d: %s", resp.StatusCode, string(bodyBytes))
			}

			return mcp.NewToolResultText(string(bodyBytes)), nil
		}
}

// GetTraceTimelineTool creates a tool to fetch spans suitable for the TraceTimeline component
func GetTraceTimelineTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_trace_timeline",
			mcp.WithDescription(`Fetch spans (OTel) for a timeline view using trace facet queries. Combine facets to narrow results; different keys are ANDed, values within a key are ORed. Use "facet-keys://traces" to discover available keys and "services://list" to discover service names.`),
			mcp.WithString("query",
				mcp.Description(`Trace facet query. Examples:\nservice.name:"api"\n'span.name':"GET /checkout"\nstatus.code:"ERROR"\nservice.name:("api" OR "web")\n-attributes.http.route:"/healthz"`),
				mcp.DefaultString(""),
			),
			mcp.WithString("lookback",
				mcp.Description("Lookback period in Go duration format (e.g., 1h, 15m, 24h). Provide either lookback or from/to."),
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
			mcp.WithIdempotentHintAnnotation(false),
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

			return mcp.NewToolResultText(string(bodyBytes)), nil
		}
}
