package tools

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/params"
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// GetLogSearchTool creates a tool to search logs
func GetLogSearchTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_log_search",
			mcp.WithDescription(`Search logs`),
			mcp.WithString("query",
				mcp.Description(`Log facets are to target the search. service.name is one of the keys, you must get services://list before setting service.name, if you don't set it, it is for all services. Keys are anded together and values in the keys are ORed. Examples;
service.name:"ingestor"
ed.tag:"prod" AND -host.name:"server1.mydomain.com"
service.name:("api" OR "web")`),
				mcp.DefaultString(""),
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
				mcp.Description("Limits the number of logs in the response. Default is 20 for AI search, max is 1000. Can be negative to move cursor in prev direction."),
				mcp.DefaultNumber(20),
			),
			mcp.WithString("cursor",
				mcp.Description("Cursor provided from previous response, pass it to next request to move the cursor with given limit."),
				mcp.DefaultString(""),
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

			return mcp.NewToolResultText(string(bodyBytes)), nil
		}
}

// GetEventSearchTool creates a tool to search events
func GetEventSearchTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_event_search",
			mcp.WithDescription("Search query using Edge Delta events search syntax, for anomaly search query should include event.type:pattern_anomaly"),
			mcp.WithString("query",
				mcp.Description(`Log facets are for targeting the search, service.name is one of the keys, you must get services://list before setting service.name, if you don't set it, it is for all services.
Keys are anded together and values in the keys are ORed. Examples;
event.type:"pattern_anomaly" // all pattern anomalies
event.domain:"Monitor Alerts" // all monitor events including logs, metrics, patterns
event.domain:"K8s" // all kubernetes events
service.name:"ingestor" AND event.type:pattern_anomaly" // all anomalies in ingestor service
event.type:"metric_threshold" // all metric threshold exceeding monitor events
event.type:"log_threshold" // all log threshold exceeding monitor events
service.name:("api" OR "web")`),
				mcp.DefaultString(""),
			),
			mcp.WithString("lookback",
				mcp.Description("Lookback period in golang duration format. e.g. '1h'. Either provide from/to or provide lookback/to or just lookback"),
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
				mcp.Description("Limits the number of events in the response. Default is 20 for AI search, max is 1000. Can be negative to move cursor in prev direction."),
				mcp.DefaultNumber(20),
			),
			mcp.WithString("cursor",
				mcp.Description("Cursor provided from previous response, pass it to next request to move the cursor with given limit."),
				mcp.DefaultString(""),
			),
			mcp.WithString("order",
				mcp.Description("Order of the events in the response, either 'ASC', 'asc', 'DESC' or 'desc'"),
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

			return mcp.NewToolResultText(string(bodyBytes)), nil
		}
}

// GetLogPatternsTool creates a tool to get pattern stats
func GetLogPatternsTool(client Client) (tool mcp.Tool, handler server.ToolHandlerFunc) {
	return mcp.NewTool("get_log_patterns",
			mcp.WithDescription("Returns top log patterns (signatures of log messages) and their stats; count, proportion, sentiment and delta. If you want to get negative sentiments, you must set negative to true."),
			mcp.WithString("query",
				mcp.Description(`Pattern facets are for targeting the search.
service.name is one of the keys, you must get services://list before setting service.name, if you don't set it, it is for all services.
Keys are anded together and values in the keys are ORed. Examples;
service.name:"ingestor"
ed.tag:"prod" AND -host.name:"server1.mydomain.com"
service.name:("api" OR "web")`),
				mcp.DefaultString(""),
			),
			mcp.WithString("lookback",
				mcp.Description("Lookback period in golang duration format. e.g. '1h'. Either provide from/to or provide lookback/to or just lookback"),
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
			mcp.WithBoolean("summary",
				mcp.Description("If summary true call returns up to 50 interesting clusters with 10 top anomaly, top/bottom delta, top/bottom count. Param size is ignored"),
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

			return mcp.NewToolResultText(string(bodyBytes)), nil
		}
}
