package events

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/tools"
)

// The extension's error codes, provisional until SEP-3415 is accepted.
const (
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
	codeHeaderMismatch = -32020
	codeNotFound       = -32023
	codeForbidden      = -32024
	codeUnsupported    = -32026
)

// Error is a JSON-RPC error an events/* request answers with.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

func notFound(name string) *Error {
	return &Error{Code: codeNotFound, Message: fmt.Sprintf("no event type %q", name), Data: map[string]any{"kind": "event"}}
}

func invalidParams(message string) *Error {
	return &Error{Code: codeInvalidParams, Message: message}
}

func internal(message string) *Error {
	return &Error{Code: codeInternal, Message: message}
}

// Handler answers the extension's requests from the org's event search.
type Handler struct {
	client    tools.Client
	store     Store
	sender    *Sender
	appURL    string
	lag       time.Duration
	maxReplay time.Duration
	now       func() time.Time
}

// Option configures a Handler.
type Option func(*Handler)

// WithAppURL sets the Edge Delta app's origin, which events link to.
func WithAppURL(u string) Option {
	return func(h *Handler) { h.appURL = u }
}

// WithLag sets how far behind now a poll's cursor stays, for events the search indexes late.
func WithLag(d time.Duration) Option {
	return func(h *Handler) { h.lag = d }
}

// WithClock sets the handler's clock, for tests.
func WithClock(now func() time.Time) Option {
	return func(h *Handler) { h.now = now }
}

// NewHandler returns a Handler that searches events through client.
func NewHandler(client tools.Client, opts ...Option) *Handler {
	h := &Handler{
		client:    client,
		appURL:    AppURLFor(client.APIURL()),
		lag:       2 * time.Minute,
		maxReplay: 7 * 24 * time.Hour,
		now:       time.Now,
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// AppURLFor returns the app origin that goes with an API origin.
func AppURLFor(apiURL string) string {
	if strings.Contains(apiURL, "staging") {
		return "https://app-staging.edgedelta.com"
	}
	return "https://app.edgedelta.com"
}

// ListResult is events/list's result.
type ListResult struct {
	ResultType string      `json:"resultType"`
	Events     []EventType `json:"events"`
}

// List answers events/list.
func (h *Handler) List() *ListResult {
	types := make([]EventType, 0, len(eventTypes))
	for _, t := range eventTypes {
		types = append(types, h.withDelivery(t))
	}
	return &ListResult{ResultType: "complete", Events: types}
}

// withDelivery adds webhooks to a type's delivery modes where there's a store to keep their subscriptions.
func (h *Handler) withDelivery(t EventType) EventType {
	if h.store != nil {
		t.Delivery = []string{"poll", "webhook"}
	}
	return t
}

func (h *Handler) lookupType(name string) (EventType, bool) {
	t, ok := lookupType(name)
	return h.withDelivery(t), ok
}

// Method returns a JSON-RPC message's method, and whether it is a request for this extension.
func Method(raw []byte) (string, bool) {
	var m struct {
		Method string          `json:"method"`
		ID     json.RawMessage `json:"id"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return "", false
	}
	return m.Method, len(m.ID) > 0 && strings.HasPrefix(m.Method, "events/")
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

// Handle answers an events/* request, with the HTTP status Streamable HTTP sends it with.
// mcpName is the request's Mcp-Name header, which must name its event type when given.
func (h *Handler) Handle(ctx context.Context, raw []byte, mcpName string) ([]byte, int) {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return encode(nil, nil, &Error{Code: -32700, Message: "parse error"}), http.StatusBadRequest
	}
	result, e := h.dispatch(ctx, req, mcpName)
	status := http.StatusOK
	if e != nil && e.Code == codeHeaderMismatch {
		status = http.StatusBadRequest
	}
	return encode(req.ID, result, e), status
}

func (h *Handler) dispatch(ctx context.Context, req request, mcpName string) (any, *Error) {
	switch req.Method {
	case "events/list":
		return h.List(), nil
	case "events/poll":
		var p PollParams
		if err := unmarshalParams(req.Params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
		if e := checkMcpName(mcpName, p.Name); e != nil {
			return nil, e
		}
		return h.Poll(ctx, p)
	case "events/subscribe":
		var p SubscribeParams
		if err := unmarshalParams(req.Params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
		if e := checkMcpName(mcpName, p.Name); e != nil {
			return nil, e
		}
		return h.Subscribe(ctx, p)
	case "events/unsubscribe":
		var p UnsubscribeParams
		if err := unmarshalParams(req.Params, &p); err != nil {
			return nil, invalidParams(err.Error())
		}
		if e := checkMcpName(mcpName, p.Name); e != nil {
			return nil, e
		}
		return h.Unsubscribe(ctx, p)
	default:
		return nil, &Error{Code: codeMethodNotFound, Message: fmt.Sprintf("method %q not found", req.Method)}
	}
}

func unmarshalParams(raw json.RawMessage, v any) error {
	if len(raw) == 0 || string(raw) == "null" {
		return fmt.Errorf("params are required")
	}
	if err := json.Unmarshal(raw, v); err != nil {
		return fmt.Errorf("invalid params: %v", err)
	}
	return nil
}

// checkMcpName follows SEP-2243's Mcp-Name rule, but lets a request without the header through for now.
func checkMcpName(header, name string) *Error {
	if header == "" {
		return nil
	}
	got := header
	if b64, ok := strings.CutPrefix(header, "=?base64?"); ok {
		b64, ok = strings.CutSuffix(b64, "?=")
		decoded, err := base64.StdEncoding.DecodeString(b64)
		if !ok || err != nil {
			return &Error{Code: codeHeaderMismatch, Message: "Mcp-Name is not valid base64"}
		}
		got = string(decoded)
	}
	if got != name {
		return &Error{Code: codeHeaderMismatch, Message: fmt.Sprintf("Mcp-Name %q does not match params.name %q", got, name)}
	}
	return nil
}

func encode(id json.RawMessage, result any, e *Error) []byte {
	if len(id) == 0 {
		id = json.RawMessage("null")
	}
	r := response{JSONRPC: "2.0", ID: id, Result: result, Error: e}
	if e != nil {
		r.Result = nil
	}
	b, err := json.Marshal(r)
	if err != nil {
		return fmt.Appendf(nil, `{"jsonrpc":"2.0","id":%s,"error":{"code":%d,"message":"encoding the result failed"}}`, id, codeInternal)
	}
	return b
}
