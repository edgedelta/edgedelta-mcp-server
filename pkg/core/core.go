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

type QueryParamOption func(url.Values)

func WithLookback(lookback string) QueryParamOption {
	return func(v url.Values) {
		v.Add("lookback", lookback)
	}
}

func WithFromTo(from string, to string) QueryParamOption {
	return func(v url.Values) {
		v.Add("from", from)
		v.Add("to", to)
	}
}

func WithQuery(query string) QueryParamOption {
	return func(v url.Values) {
		v.Add("query", query)
	}
}

func WithLimit(limit string) QueryParamOption {
	return func(v url.Values) {
		v.Add("limit", limit)
	}
}

func WithCursor(cursor string) QueryParamOption {
	return func(v url.Values) {
		v.Add("cursor", cursor)
	}
}

func WithOrder(order string) QueryParamOption {
	return func(v url.Values) {
		v.Add("order", order)
	}
}
