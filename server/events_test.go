package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/events"
	"github.com/edgedelta/edgedelta-mcp-server/pkg/tools"

	"github.com/google/go-cmp/cmp"
)

func post(t *testing.T, url, body string, header http.Header) (int, map[string]any) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range header {
		req.Header[k] = v
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return resp.StatusCode, got
}

func TestHTTPEvents(t *testing.T) {
	m, err := NewHTTPServer()
	if err != nil {
		t.Fatalf("NewHTTPServer: %v", err)
	}
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()

	tests := []struct {
		desc   string
		body   string
		header http.Header
		status int
		check  func(map[string]any) any
		want   any
	}{
		{
			desc:   "initialize advertises the extension",
			body:   `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`,
			status: http.StatusOK,
			check:  func(r map[string]any) any { return dig(r, "result", "capabilities", "extensions") },
			want:   map[string]any{events.Extension: map[string]any{}},
		},
		{
			desc:   "events/list names the types",
			body:   `{"jsonrpc":"2.0","id":2,"method":"events/list"}`,
			status: http.StatusOK,
			check: func(r map[string]any) any {
				var names []any
				for _, e := range dig(r, "result", "events").([]any) {
					names = append(names, e.(map[string]any)["name"])
				}
				return names
			},
			want: []any{"monitor.alert", "pattern.anomaly"},
		},
		{
			desc:   "other events methods are not found",
			body:   `{"jsonrpc":"2.0","id":3,"method":"events/stream","params":{"name":"monitor.alert"}}`,
			status: http.StatusOK,
			check:  func(r map[string]any) any { return dig(r, "error", "code") },
			want:   float64(-32601),
		},
		{
			desc:   "a mismatched Mcp-Name is refused",
			body:   `{"jsonrpc":"2.0","id":4,"method":"events/poll","params":{"name":"monitor.alert","cursor":null}}`,
			header: http.Header{"Mcp-Name": {"pattern.anomaly"}},
			status: http.StatusBadRequest,
			check:  func(r map[string]any) any { return dig(r, "error", "code") },
			want:   float64(-32020),
		},
		{
			desc:   "core methods still reach mcp-go",
			body:   `{"jsonrpc":"2.0","id":5,"method":"ping"}`,
			status: http.StatusOK,
			check:  func(r map[string]any) any { return r["result"] },
			want:   map[string]any{},
		},
	}
	for _, tc := range tests {
		status, got := post(t, srv.URL, tc.body, tc.header)
		if status != tc.status {
			t.Errorf("%s: status got: %d, want: %d", tc.desc, status, tc.status)
		}
		if diff := cmp.Diff(tc.want, tc.check(got)); diff != "" {
			t.Errorf("%s: mismatch (-want, +got): %v", tc.desc, diff)
		}
	}
}

func dig(v any, keys ...string) any {
	for _, k := range keys {
		m, ok := v.(map[string]any)
		if !ok {
			return nil
		}
		v = m[k]
	}
	return v
}

func TestFilterEvents(t *testing.T) {
	h := events.NewHandler(tools.NewHTTPClient("https://api.edgedelta.com", "X-ED-API-Token"))
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":2,"method":"events/list"}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":3,"method":"events/unsubscribe"}`,
	}, "\n") + "\n"

	passed, next := io.Pipe()
	var forwarded []string
	done := make(chan struct{})
	go func() {
		defer close(done)
		s := bufio.NewScanner(passed)
		for s.Scan() {
			forwarded = append(forwarded, s.Text())
		}
	}()
	out := &strings.Builder{}
	if err := filterEvents(context.Background(), h, strings.NewReader(input), &lineWriter{w: out}, next); err != nil {
		t.Fatalf("filterEvents: %v", err)
	}
	<-done

	want := []string{`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, `{"jsonrpc":"2.0","method":"notifications/initialized"}`}
	if diff := cmp.Diff(want, forwarded); diff != "" {
		t.Errorf("forwarded mismatch (-want, +got): %v", diff)
	}
	answered := map[float64]any{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var r map[string]any
		if err := json.Unmarshal([]byte(line), &r); err != nil {
			t.Fatalf("decode %q: %v", line, err)
		}
		answered[r["id"].(float64)] = r["error"] == nil
	}
	if diff := cmp.Diff(map[float64]any{2: true, 3: false}, answered); diff != "" {
		t.Errorf("answered mismatch (-want, +got): %v", diff)
	}
}

type noStore struct{}

func (noStore) Get(context.Context, string) (*events.Subscription, bool, error) {
	return nil, false, nil
}
func (noStore) Put(context.Context, *events.Subscription) error          { return nil }
func (noStore) Delete(context.Context, string) error                     { return nil }
func (noStore) VerifiedUntil(context.Context, string) (time.Time, error) { return time.Time{}, nil }
func (noStore) Verify(context.Context, string, time.Time) error          { return nil }

func TestHTTPEventsWithSubscriptions(t *testing.T) {
	m, err := NewHTTPServer(WithEventSubscriptions(noStore{}, events.NewSender(false)))
	if err != nil {
		t.Fatalf("NewHTTPServer: %v", err)
	}
	srv := httptest.NewServer(m.Handler())
	defer srv.Close()

	_, got := post(t, srv.URL, `{"jsonrpc":"2.0","id":1,"method":"events/list"}`, nil)
	var delivery []any
	for _, e := range dig(got, "result", "events").([]any) {
		delivery = append(delivery, e.(map[string]any)["delivery"])
	}
	want := []any{[]any{"poll", "webhook"}, []any{"poll", "webhook"}}
	if diff := cmp.Diff(want, delivery); diff != "" {
		t.Errorf("delivery mismatch (-want, +got): %v", diff)
	}
}
