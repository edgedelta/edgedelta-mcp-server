package skills

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"unicode/utf8"
)

const (
	codeParse          = -32700
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

// The cache hint on every result: the skills are built into the binary and the same for every caller.
const (
	defaultTTLMs = 5 * 60 * 1000
	cacheScope   = "public"
	pageSize     = 100
)

// Error is a JSON-RPC error a request answers with.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func invalidParams(format string, args ...any) *Error {
	return &Error{Code: codeInvalidParams, Message: fmt.Sprintf(format, args...)}
}

// Handler answers the extension's requests from a Catalog: skills/list, skills/get, resources/directory/read, and
// resources/read for skill:// URIs.
type Handler struct {
	catalog  *Catalog
	pageSize int
	ttlMs    int
}

// Option configures a Handler.
type Option func(*Handler)

// WithPageSize sets how many entries one page of skills/list or resources/directory/read holds.
func WithPageSize(n int) Option {
	return func(h *Handler) { h.pageSize = n }
}

// NewHandler returns a Handler that serves catalog.
func NewHandler(catalog *Catalog, opts ...Option) *Handler {
	h := &Handler{catalog: catalog, pageSize: pageSize, ttlMs: defaultTTLMs}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Catalog returns the skills the handler serves.
func (h *Handler) Catalog() *Catalog { return h.catalog }

// Capability is the extension's entry under the server capabilities' extensions.
func Capability() map[string]any {
	return map[string]any{"directoryRead": true}
}

type request struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

type uriParams struct {
	URI    string  `json:"uri"`
	Cursor *string `json:"cursor"`
}

// Intercepts reports whether a JSON-RPC message is a request this handler answers.
func (h *Handler) Intercepts(raw []byte) bool {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil || len(req.ID) == 0 {
		return false
	}
	switch req.Method {
	case "skills/list", "skills/get", "resources/directory/read":
		return true
	case "resources/read":
		var p uriParams
		return json.Unmarshal(req.Params, &p) == nil && strings.HasPrefix(p.URI, "skill://")
	}
	return false
}

// Handle answers a request, with the HTTP status Streamable HTTP sends it with.
func (h *Handler) Handle(raw []byte) ([]byte, int) {
	var req request
	if err := json.Unmarshal(raw, &req); err != nil {
		return encode(nil, nil, &Error{Code: codeParse, Message: "parse error"}), http.StatusBadRequest
	}
	var p uriParams
	if len(req.Params) > 0 && string(req.Params) != "null" {
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return encode(req.ID, nil, invalidParams("invalid params: %v", err)), http.StatusOK
		}
	}
	result, e := h.dispatch(req.Method, p)
	return encode(req.ID, result, e), http.StatusOK
}

func (h *Handler) cached(result map[string]any) map[string]any {
	result["resultType"] = "complete"
	result["ttlMs"] = h.ttlMs
	result["cacheScope"] = cacheScope
	return result
}

func (h *Handler) dispatch(method string, p uriParams) (any, *Error) {
	switch method {
	case "skills/list":
		skills := h.catalog.Skills()
		start, next, e := h.page(len(skills), func(i int) string { return skills[i].URI }, p.Cursor)
		if e != nil {
			return nil, e
		}
		result := map[string]any{"skills": skills[start:next]}
		if next < len(skills) {
			result["nextCursor"] = encodeCursor(skills[next-1].URI)
		}
		return h.cached(result), nil
	case "skills/get":
		if p.URI == "" {
			return nil, invalidParams("params.uri is required")
		}
		skill, ok := h.catalog.Get(p.URI)
		if !ok {
			return nil, invalidParams("No skill is served at %s", p.URI)
		}
		return h.cached(map[string]any{"skill": skill}), nil
	case "resources/directory/read":
		children, ok := h.catalog.Directory(p.URI)
		if !ok {
			return nil, invalidParams("%s is not a directory resource", p.URI)
		}
		start, next, e := h.page(len(children), func(i int) string { return children[i].URI }, p.Cursor)
		if e != nil {
			return nil, e
		}
		result := map[string]any{"resources": children[start:next]}
		if next < len(children) {
			result["nextCursor"] = encodeCursor(children[next-1].URI)
		}
		return h.cached(result), nil
	case "resources/read":
		return h.read(p.URI)
	default:
		return nil, &Error{Code: codeMethodNotFound, Message: fmt.Sprintf("method %q not found", method)}
	}
}

func (h *Handler) read(uri string) (any, *Error) {
	data, mimeType, ok := h.catalog.Read(uri)
	if !ok {
		if h.catalog.IsDirectory(uri) {
			return nil, invalidParams("%s is a directory; use resources/directory/read", uri)
		}
		return nil, invalidParams("Resource %s not found", uri)
	}
	content := map[string]any{"uri": uri, "mimeType": mimeType}
	if utf8.Valid(data) {
		content["text"] = string(data)
	} else {
		content["blob"] = base64.StdEncoding.EncodeToString(data)
	}
	return map[string]any{"contents": []any{content}}, nil
}

// page returns the bounds of the page after cursor, of n items sorted by key.
func (h *Handler) page(n int, key func(int) string, cursor *string) (int, int, *Error) {
	start := 0
	if cursor != nil {
		after, ok := decodeCursor(*cursor)
		if !ok {
			return 0, 0, invalidParams("Invalid cursor")
		}
		for start < n && key(start) <= after {
			start++
		}
	}
	return start, min(n, start+h.pageSize), nil
}

const cursorPrefix = "after:"

func encodeCursor(uri string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(cursorPrefix + uri))
}

func decodeCursor(cursor string) (string, bool) {
	b, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil {
		return "", false
	}
	return strings.CutPrefix(string(b), cursorPrefix)
}

type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
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
