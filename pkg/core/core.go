package core

import (
	"net/url"
)

const (
	// URLTimeFormat is used to parse date time query parameters
	URLTimeFormat = "2006-01-02T15:04:05.000Z"
)

type LogSearchResponse struct {
	QueryID    string           `json:"query_id"`
	Items      []*LogSearchItem `json:"items"`
	NextCursor string           `json:"next_cursor"`
}

type LogSearchItem struct {
	ID           string            `json:"id"`
	Timestamp    int64             `json:"timestamp"`
	SeverityText string            `json:"severity_text"`
	Body         string            `json:"body"`
	Resource     map[string]string `json:"resource"`
	Attributes   map[string]string `json:"attributes"`
}

type EventItem struct {
	Timestamp    int64             `json:"timestamp"`
	EventDomain  string            `json:"event_domain"`
	EventType    string            `json:"event_type"`
	SeverityText string            `json:"severity_text"`
	Body         string            `json:"body"`
	Resource     map[string]string `json:"resource"`
	Attributes   map[string]string `json:"attributes"`
}

type EventResponse struct {
	QueryID    string       `json:"query_id"`
	Items      []*EventItem `json:"items"`
	NextCursor string       `json:"next_cursor"`
}

type PatternStatsResponse struct {
	Stats []*PatternStats `json:"stats"`
}

type PatternStats struct {
	// Pattern of the cluster
	Pattern string `json:"pattern"`
	// Count of cluster in the window
	Count int `json:"count"`
	// Proportion of this cluster to rest of the cluster. Rest of the cluster can be scoped to tag or source.
	Proportion float64 `json:"proportion"`
	// Sentiment analysis results
	Sentiment float64 `json:"sentiment"`
	// Delta is the percentage increase of this cluster's count compared to previous window.
	Delta float64 `json:"delta"`
}

type QueryParamOption func(url.Values)

func WithLookback(lookback string) QueryParamOption {
	return func(v url.Values) {
		if lookback != "" {
			v.Add("lookback", lookback)
		}
	}
}

func WithFromTo(from string, to string) QueryParamOption {
	return func(v url.Values) {
		if from != "" && to != "" {
			v.Add("from", from)
			v.Add("to", to)
		}
	}
}

func WithQuery(query string) QueryParamOption {
	return func(v url.Values) {
		if query != "" {
			v.Add("query", query)
		}
	}
}

func WithLimit(limit string) QueryParamOption {
	return func(v url.Values) {
		if limit != "" {
			v.Add("limit", limit)
		}
	}
}

func WithCursor(cursor string) QueryParamOption {
	return func(v url.Values) {
		if cursor != "" {
			v.Add("cursor", cursor)
		}
	}
}

func WithOrder(order string) QueryParamOption {
	return func(v url.Values) {
		if order != "" {
			v.Add("order", order)
		}
	}
}

func WithOffset(offset string) QueryParamOption {
	return func(v url.Values) {
		if offset != "" {
			v.Add("offset", offset)
		}
	}
}

func WithNegative(negative bool) QueryParamOption {
	return func(v url.Values) {
		if negative {
			v.Add("negative", "true")
		}
	}
}

func WithSummary(summary bool) QueryParamOption {
	return func(v url.Values) {
		if summary {
			v.Add("summary", "true")
		}
	}
}

// Client defines the methods required for interacting with Edge Delta services.
// It can be implemented with different transports (e.g. HTTP, Mock, GRPC).
//
// Example implementations:
//   - HTTPClient: Uses HTTP REST API (provided in this package)
//   - MockClient: For testing (implement this interface in your test package)
//
// Usage:
//
//	// Using the built-in HTTP client
//	httpClient := core.NewClient(orgID, apiURL, token)
//	server := core.NewServer(httpClient, version)
type Client interface {
	GetLogs(opts ...QueryParamOption) (*LogSearchResponse, error)
	GetEvents(opts ...QueryParamOption) (*EventResponse, error)
	GetPatternStats(opts ...QueryParamOption) (*PatternStatsResponse, error)
}
