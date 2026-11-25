package server

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"sync"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/events"
	"github.com/edgedelta/edgedelta-mcp-server/pkg/skills"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// extensions answers the requests of the extensions this server offers itself, since mcp-go has no handlers for
// custom methods: the Events extension's events/*, and the Skills extension's skills/*, resources/directory/read and
// resources/read of skill:// URIs. Skills are nil when the server offers none.
type extensions struct {
	events *events.Handler
	skills *skills.Handler
}

// handle answers a JSON-RPC message if it is a request for one of the extensions.
func (x extensions) handle(ctx context.Context, raw []byte, mcpName string) ([]byte, int, bool) {
	if _, ok := events.Method(raw); ok {
		resp, status := x.events.Handle(ctx, raw, mcpName)
		return resp, status, true
	}
	if x.skills != nil && x.skills.Intercepts(raw) {
		resp, status := x.skills.Handle(raw)
		return resp, status, true
	}
	return nil, 0, false
}

// extensionHooks advertise the extensions from initialize.
func extensionHooks(withSkills bool) *server.Hooks {
	hooks := &server.Hooks{}
	hooks.AddAfterInitialize(func(_ context.Context, _ any, _ *mcp.InitializeRequest, result *mcp.InitializeResult) {
		if result.Capabilities.Extensions == nil {
			result.Capabilities.Extensions = map[string]any{}
		}
		result.Capabilities.Extensions[events.Extension] = map[string]any{}
		if withSkills {
			result.Capabilities.Extensions[skills.Extension] = skills.Capability()
		}
	})
	return hooks
}

// addSkillResources lists each skill's SKILL.md in resources/list. Reads of every skill:// URI, these included, are
// answered by the skills handler before they reach mcp-go.
func addSkillResources(s *server.MCPServer, h *skills.Handler) {
	for _, skill := range h.Catalog().Skills() {
		s.AddResource(
			mcp.NewResource(skill.URI, skill.Name(),
				mcp.WithResourceDescription(skill.Description()),
				mcp.WithMIMEType("text/markdown"),
			),
			func(_ context.Context, req mcp.ReadResourceRequest) ([]mcp.ResourceContents, error) {
				data, mimeType, _ := h.Catalog().Read(req.Params.URI)
				return []mcp.ResourceContents{mcp.TextResourceContents{URI: req.Params.URI, MIMEType: mimeType, Text: string(data)}}, nil
			},
		)
	}
}

// extensionsHTTPHandler answers the extensions' requests itself and passes every other request on to next.
func extensionsHTTPHandler(x extensions, contextFunc server.HTTPContextFunc, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			next.ServeHTTP(w, r)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "reading the request failed", http.StatusBadRequest)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		resp, status, ok := x.handle(contextFunc(r.Context(), r), body, r.Header.Get("Mcp-Name"))
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if _, err := w.Write(resp); err != nil {
			return
		}
	})
}

// lineWriter writes each message as one line, whoever sends it.
type lineWriter struct {
	mu sync.Mutex
	w  io.Writer
}

func (l *lineWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

// filterExtensions answers the extensions' requests read from in on out, and passes every other line on to next.
func filterExtensions(ctx context.Context, x extensions, in io.Reader, out io.Writer, next *io.PipeWriter) error {
	r := bufio.NewReader(in)
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if _, isEvent := events.Method(line); isEvent || (x.skills != nil && x.skills.Intercepts(line)) {
				wg.Add(1)
				go func(line []byte) {
					defer wg.Done()
					resp, _, _ := x.handle(ctx, line, "")
					if _, err := out.Write(append(resp, '\n')); err != nil {
						return
					}
				}(line)
			} else if _, err := next.Write(line); err != nil {
				return err
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				err = nil
			}
			return next.CloseWithError(err)
		}
	}
}
