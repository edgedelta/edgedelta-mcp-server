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

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// eventsHooks advertise the Events extension from initialize.
func eventsHooks() *server.Hooks {
	hooks := &server.Hooks{}
	hooks.AddAfterInitialize(func(_ context.Context, _ any, _ *mcp.InitializeRequest, result *mcp.InitializeResult) {
		if result.Capabilities.Extensions == nil {
			result.Capabilities.Extensions = map[string]any{}
		}
		result.Capabilities.Extensions[events.Extension] = map[string]any{}
	})
	return hooks
}

// eventsHTTPHandler answers events/* requests itself, since mcp-go has no handlers for custom methods.
func eventsHTTPHandler(h *events.Handler, contextFunc server.HTTPContextFunc, next http.Handler) http.Handler {
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
		if _, ok := events.Method(body); !ok {
			next.ServeHTTP(w, r)
			return
		}
		resp, status := h.Handle(contextFunc(r.Context(), r), body, r.Header.Get("Mcp-Name"))
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

// filterEvents answers the events/* requests read from in on out, and passes every other line on to next.
func filterEvents(ctx context.Context, h *events.Handler, in io.Reader, out io.Writer, next *io.PipeWriter) error {
	r := bufio.NewReader(in)
	var wg sync.WaitGroup
	defer wg.Wait()
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			if _, ok := events.Method(line); ok {
				wg.Add(1)
				go func(line []byte) {
					defer wg.Done()
					resp, _ := h.Handle(ctx, line, "")
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
