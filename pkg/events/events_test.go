package events

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/tools"

	"github.com/google/go-cmp/cmp"
)

var t0 = time.Date(2026, 11, 2, 12, 0, 0, 0, time.UTC)

// fakeSearch is the event search API over a fixed set of indexed events.
type fakeSearch struct {
	indexed []searchItem
	status  int
	queries []string
}

func (f *fakeSearch) APIURL() string                     { return "https://api.edgedelta.com" }
func (f *fakeSearch) Get(string) (*http.Response, error) { return nil, fmt.Errorf("unexpected get") }

func (f *fakeSearch) Do(req *http.Request) (*http.Response, error) {
	q := req.URL.Query()
	f.queries = append(f.queries, q.Get("query"))
	if f.status != 0 {
		return &http.Response{StatusCode: f.status, Body: io.NopCloser(bytes.NewReader([]byte("nope")))}, nil
	}
	from, err := time.Parse(timeFormat, q.Get("from"))
	if err != nil {
		return nil, err
	}
	to, err := time.Parse(timeFormat, q.Get("to"))
	if err != nil {
		return nil, err
	}
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil {
		return nil, err
	}
	if q.Get("order") != "asc" {
		return nil, fmt.Errorf("order %q", q.Get("order"))
	}
	var items []searchItem
	for _, it := range f.indexed {
		if it.Timestamp >= from.UnixMilli() && it.Timestamp <= to.UnixMilli() {
			items = append(items, it)
		}
	}
	slices.SortStableFunc(items, func(a, b searchItem) int { return int(a.Timestamp - b.Timestamp) })
	items = items[:min(len(items), limit)]
	b, err := json.Marshal(map[string]any{"items": items})
	if err != nil {
		return nil, err
	}
	return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(b))}, nil
}

var _ tools.Client = (*fakeSearch)(nil)

func alert(id string, at time.Time) searchItem {
	return searchItem{
		Timestamp:    at.UnixMilli(),
		EventDomain:  "Monitor",
		EventType:    "log_threshold",
		SeverityText: "ALERT",
		Body:         "Errors are up for service.name:api",
		Resource:     map[string]string{"event.name": "Errors", "ed.monitor.id": "mon1", "service.name": "api"},
		Attributes:   map[string]string{"event.id": id, "ed.monitor.priority": "High"},
	}
}

func orgContext() context.Context {
	return context.WithValue(context.Background(), tools.OrgIDKey, "org1")
}

func ptr[T any](v T) *T { return &v }

func ids(r *PollResult) []string {
	out := []string{}
	for _, e := range r.Events {
		out = append(out, e.EventID)
	}
	return out
}

func TestPollSequence(t *testing.T) {
	type step struct {
		at        time.Duration // since t0
		index     []searchItem  // indexed before this poll
		maxEvents *int
		maxAgeMs  *int64
		want      []string
		hasMore   bool
		truncated bool
	}
	tests := []struct {
		desc  string
		steps []step
	}{
		{
			desc: "starts from now and delivers each event once",
			steps: []step{
				{at: 0, index: []searchItem{alert("before", t0.Add(-time.Minute))}, want: []string{}},
				{at: 30 * time.Second, index: []searchItem{alert("a", t0.Add(10*time.Second)), alert("b", t0.Add(20*time.Second))}, want: []string{"a", "b"}},
				{at: 60 * time.Second, want: []string{}},
				{at: 5 * time.Minute, want: []string{}},
			},
		},
		{
			desc: "picks up an event indexed late, within the lag",
			steps: []step{
				{at: 0, want: []string{}},
				{at: 30 * time.Second, index: []searchItem{alert("a", t0.Add(25*time.Second))}, want: []string{"a"}},
				{at: 60 * time.Second, index: []searchItem{alert("late", t0.Add(5*time.Second))}, want: []string{"late"}},
				{at: 90 * time.Second, want: []string{}},
			},
		},
		{
			desc: "drains a backlog in batches, ties at one millisecond included",
			steps: []step{
				{at: 0, want: []string{}},
				{
					at: time.Minute,
					index: []searchItem{
						alert("a", t0.Add(time.Second)), alert("b", t0.Add(2*time.Second)),
						alert("c", t0.Add(2*time.Second)), alert("d", t0.Add(2*time.Second)), alert("e", t0.Add(3*time.Second)),
					},
					maxEvents: ptr(2), want: []string{"a", "b"}, hasMore: true,
				},
				{at: time.Minute, maxEvents: ptr(2), want: []string{"c", "d"}, hasMore: true},
				{at: time.Minute, maxEvents: ptr(2), want: []string{"e"}},
				{at: 2 * time.Minute, maxEvents: ptr(2), want: []string{}},
			},
		},
		{
			desc: "maxAgeMs skips what is older, and says so",
			steps: []step{
				{at: 0, want: []string{}},
				{
					at:       10 * time.Minute,
					index:    []searchItem{alert("old", t0.Add(time.Minute)), alert("new", t0.Add(9*time.Minute))},
					maxAgeMs: ptr(int64(5 * 60_000)), want: []string{"new"}, truncated: true,
				},
			},
		},
		{
			desc: "maxAgeMs past the cursor with nothing skipped is not a gap",
			steps: []step{
				{at: 0, want: []string{}},
				{at: 10 * time.Minute, index: []searchItem{alert("new", t0.Add(9*time.Minute))}, maxAgeMs: ptr(int64(5 * 60_000)), want: []string{"new"}},
			},
		},
	}
	for _, tc := range tests {
		search := &fakeSearch{}
		var now time.Time
		h := NewHandler(search, WithClock(func() time.Time { return now }))
		var cur *string
		for i, s := range tc.steps {
			now = t0.Add(s.at)
			search.indexed = append(search.indexed, s.index...)
			got, e := h.Poll(orgContext(), PollParams{Name: monitorAlert, Cursor: cur, MaxEvents: s.maxEvents, MaxAgeMs: s.maxAgeMs})
			if e != nil {
				t.Fatalf("%s: poll %d: %+v", tc.desc, i, e)
			}
			if diff := cmp.Diff(s.want, ids(got)); diff != "" {
				t.Errorf("%s: poll %d: events mismatch (-want, +got): %v", tc.desc, i, diff)
			}
			if got.HasMore != s.hasMore {
				t.Errorf("%s: poll %d: hasMore got: %v, want: %v", tc.desc, i, got.HasMore, s.hasMore)
			}
			if got.Truncated != s.truncated {
				t.Errorf("%s: poll %d: truncated got: %v, want: %v", tc.desc, i, got.Truncated, s.truncated)
			}
			if got.NextPollMs != nextPollMs {
				t.Errorf("%s: poll %d: nextPollMs got: %v, want: %v", tc.desc, i, got.NextPollMs, nextPollMs)
			}
			cur = &got.Cursor
		}
	}
}

func TestPollWatermark(t *testing.T) {
	search := &fakeSearch{}
	now := t0
	h := NewHandler(search, WithClock(func() time.Time { return now }))
	started, e := h.Poll(orgContext(), PollParams{Name: monitorAlert})
	if e != nil {
		t.Fatalf("start: %+v", e)
	}
	now = t0.Add(10 * time.Minute)
	search.indexed = []searchItem{alert("a", t0.Add(9*time.Minute)), alert("b", t0.Add(time.Minute))}
	got, e := h.Poll(orgContext(), PollParams{Name: monitorAlert, Cursor: &started.Cursor})
	if e != nil {
		t.Fatalf("poll: %+v", e)
	}
	c, ok := decodeCursor(got.Cursor)
	if !ok {
		t.Fatalf("decode %q failed", got.Cursor)
	}
	want := cursor{Watermark: t0.Add(8 * time.Minute).UnixMilli(), Seen: []seenItem{{T: t0.Add(9 * time.Minute).UnixMilli(), ID: "a"}}}
	if diff := cmp.Diff(want, c); diff != "" {
		t.Errorf("cursor mismatch (-want, +got): %v", diff)
	}
}

func TestPollErrors(t *testing.T) {
	tests := []struct {
		desc   string
		params PollParams
		status int
		ctx    context.Context
		want   int
		data   any
	}{
		{desc: "unknown type", params: PollParams{Name: "nope"}, want: codeNotFound, data: map[string]any{"kind": "event"}},
		{desc: "unknown argument", params: PollParams{Name: monitorAlert, Arguments: map[string]any{"color": "red"}}, want: codeInvalidParams},
		{desc: "unknown state", params: PollParams{Name: monitorAlert, Arguments: map[string]any{"states": []any{"ON FIRE"}}}, want: codeInvalidParams},
		{desc: "unknown monitor type", params: PollParams{Name: monitorAlert, Arguments: map[string]any{"type": "vibes"}}, want: codeInvalidParams},
		{desc: "type on pattern.anomaly", params: PollParams{Name: patternAnomaly, Arguments: map[string]any{"type": "log_threshold"}}, want: codeInvalidParams},
		{desc: "maxEvents of zero", params: PollParams{Name: monitorAlert, MaxEvents: ptr(0)}, want: codeInvalidParams},
		{desc: "foreign cursor", params: PollParams{Name: monitorAlert, Cursor: ptr("abc")}, want: codeInvalidParams},
		{desc: "search forbids", params: PollParams{Name: monitorAlert, Cursor: ptr(validCursor(t))}, status: http.StatusForbidden, want: codeForbidden},
		{desc: "search refuses the query", params: PollParams{Name: monitorAlert, Cursor: ptr(validCursor(t))}, status: http.StatusBadRequest, want: codeInvalidParams},
		{desc: "search fails", params: PollParams{Name: monitorAlert, Cursor: ptr(validCursor(t))}, status: http.StatusBadGateway, want: codeInternal},
		{desc: "no organization", params: PollParams{Name: monitorAlert, Cursor: ptr(validCursor(t))}, ctx: context.Background(), want: codeForbidden},
	}
	for _, tc := range tests {
		ctx := tc.ctx
		if ctx == nil {
			ctx = orgContext()
		}
		h := NewHandler(&fakeSearch{status: tc.status}, WithClock(func() time.Time { return t0 }))
		_, e := h.Poll(ctx, tc.params)
		if e == nil {
			t.Errorf("%s: got no error, want: %d", tc.desc, tc.want)
			continue
		}
		if e.Code != tc.want {
			t.Errorf("%s: code got: %d, want: %d (%s)", tc.desc, e.Code, tc.want, e.Message)
		}
		if tc.data != nil {
			if diff := cmp.Diff(tc.data, e.Data); diff != "" {
				t.Errorf("%s: data mismatch (-want, +got): %v", tc.desc, diff)
			}
		}
	}
}

func validCursor(t *testing.T) string {
	t.Helper()
	s, err := cursor{Watermark: t0.Add(-time.Minute).UnixMilli()}.encode()
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	return s
}

func TestQuery(t *testing.T) {
	tests := []struct {
		desc string
		name string
		args map[string]any
		want string
	}{
		{desc: "no arguments", name: monitorAlert, want: `event.domain:("Monitor" OR "Monitor Alerts")`},
		{desc: "pattern anomalies", name: patternAnomaly, want: `event.domain:("Monitor" OR "Monitor Alerts") AND event.type:"pattern_anomaly"`},
		{
			desc: "every argument",
			name: monitorAlert,
			args: map[string]any{
				"monitorId": "mon1", "monitor": `Say "hi"`, "service": "api", "type": "log_threshold",
				"states": []any{"ALERT", "NO DATA"}, "query": `@ed.monitor.priority:"High"`,
			},
			want: `event.domain:("Monitor" OR "Monitor Alerts") AND event.name:"Say \"hi\"" AND ed.monitor.id:"mon1"` +
				` AND (@ed.monitor.priority:"High") AND service.name:"api" AND severity_text:("ALERT" OR "NO DATA") AND event.type:"log_threshold"`,
		},
		{desc: "empty values", name: monitorAlert, args: map[string]any{"monitor": "", "states": []any{}}, want: `event.domain:("Monitor" OR "Monitor Alerts")`},
	}
	for _, tc := range tests {
		typ, ok := lookupType(tc.name)
		if !ok {
			t.Fatalf("%s: no type %s", tc.desc, tc.name)
		}
		got, err := typ.query(tc.args)
		if err != nil {
			t.Fatalf("%s: %v", tc.desc, err)
		}
		if got != tc.want {
			t.Errorf("%s: got: %v, want: %v", tc.desc, got, tc.want)
		}
	}
}

func TestOccurrence(t *testing.T) {
	at := time.Date(2026, 11, 2, 14, 36, 59, 919_000_000, time.UTC)
	it := searchItem{
		Timestamp:    at.UnixMilli(),
		EventDomain:  "Monitor",
		EventType:    "pattern_anomaly",
		SeverityText: "WARN",
		Body:         "Default Pattern Anomaly Monitor for service.name:http-receiver",
		Resource:     map[string]string{"ed.monitor.id": "mon1", "event.name": "Default Pattern Anomaly Monitor", "service.name": "http-receiver"},
		Attributes: map[string]string{
			"event.id":                   "3LYTvn0VvZmW9VzAvVnuApLTmSH",
			"ed.monitor.priority":        "High",
			"ed.monitor.group.name":      "service.name:http-receiver",
			"ed.monitor.evaluated.value": "4.00",
			"ed.monitor.patterns.1":      "second: 2",
			"ed.monitor.patterns.0":      "first: 9",
			"ed.monitor.patterns.10":     "eleventh: 1",
		},
	}
	want := Occurrence{
		EventID:   "3LYTvn0VvZmW9VzAvVnuApLTmSH",
		Name:      patternAnomaly,
		Timestamp: "2026-11-02T14:36:59.919Z",
		Data: map[string]any{
			"title":       "Default Pattern Anomaly Monitor for service.name:http-receiver",
			"url":         "https://app.edgedelta.com/events/monitors?from=2026-11-02T14%3A06%3A59.919Z&query=%40event.id%3A3LYTvn0VvZmW9VzAvVnuApLTmSH&to=2026-11-02T15%3A06%3A59.919Z",
			"monitorId":   "mon1",
			"monitorName": "Default Pattern Anomaly Monitor",
			"monitorType": "pattern_anomaly",
			"state":       "WARN",
			"priority":    "High",
			"service":     "http-receiver",
			"group":       "service.name:http-receiver",
			"value":       "4.00",
			"patterns":    []string{"first: 9", "second: 2", "eleventh: 1"},
			"resource":    it.Resource,
		},
	}
	if diff := cmp.Diff(want, it.occurrence(patternAnomaly, "https://app.edgedelta.com")); diff != "" {
		t.Errorf("occurrence mismatch (-want, +got): %v", diff)
	}

	legacy := it
	legacy.Attributes = map[string]string{}
	if got, again := legacy.id(), legacy.id(); got != again || got == "" {
		t.Errorf("legacy id got: %q then %q, want one stable id", got, again)
	}
}

func TestHandle(t *testing.T) {
	tests := []struct {
		desc    string
		body    string
		mcpName string
		status  int
		want    string
	}{
		{
			desc:   "lists types",
			body:   `{"jsonrpc":"2.0","id":1,"method":"events/list"}`,
			status: http.StatusOK,
			want:   `{"jsonrpc":"2.0","id":1,"result":{"resultType":"complete","events":["..."]}}`,
		},
		{
			desc:   "starts a poll",
			body:   `{"jsonrpc":"2.0","id":"x","method":"events/poll","params":{"name":"monitor.alert","arguments":{},"cursor":null}}`,
			status: http.StatusOK,
			want:   `{"jsonrpc":"2.0","id":"x","result":{"resultType":"complete","events":[],"cursor":"...","truncated":false,"hasMore":false,"nextPollMs":30000}}`,
		},
		{
			desc:    "takes a matching Mcp-Name",
			body:    `{"jsonrpc":"2.0","id":2,"method":"events/poll","params":{"name":"monitor.alert","cursor":null}}`,
			mcpName: "=?base64?bW9uaXRvci5hbGVydA==?=",
			status:  http.StatusOK,
			want:    `{"jsonrpc":"2.0","id":2,"result":{"resultType":"complete","events":[],"cursor":"...","truncated":false,"hasMore":false,"nextPollMs":30000}}`,
		},
		{
			desc:    "refuses a mismatched Mcp-Name",
			body:    `{"jsonrpc":"2.0","id":3,"method":"events/poll","params":{"name":"monitor.alert","cursor":null}}`,
			mcpName: "pattern.anomaly",
			status:  http.StatusBadRequest,
			want:    `{"jsonrpc":"2.0","id":3,"error":{"code":-32020,"message":"Mcp-Name \"pattern.anomaly\" does not match params.name \"monitor.alert\""}}`,
		},
		{
			desc:   "has no webhooks yet",
			body:   `{"jsonrpc":"2.0","id":4,"method":"events/subscribe","params":{"name":"monitor.alert"}}`,
			status: http.StatusOK,
			want:   `{"jsonrpc":"2.0","id":4,"error":{"code":-32601,"message":"method \"events/subscribe\" not found"}}`,
		},
		{
			desc:   "needs poll params",
			body:   `{"jsonrpc":"2.0","id":5,"method":"events/poll"}`,
			status: http.StatusOK,
			want:   `{"jsonrpc":"2.0","id":5,"error":{"code":-32602,"message":"params are required"}}`,
		},
	}
	h := NewHandler(&fakeSearch{}, WithClock(func() time.Time { return t0 }))
	for _, tc := range tests {
		got, status := h.Handle(orgContext(), []byte(tc.body), tc.mcpName)
		if status != tc.status {
			t.Errorf("%s: status got: %d, want: %d", tc.desc, status, tc.status)
		}
		if diff := cmp.Diff(decode(t, []byte(tc.want)), elide(t, got)); diff != "" {
			t.Errorf("%s: response mismatch (-want, +got): %v", tc.desc, diff)
		}
	}
}

func decode(t *testing.T, b []byte) map[string]any {
	t.Helper()
	var r map[string]any
	if err := json.Unmarshal(b, &r); err != nil {
		t.Fatalf("decode %s: %v", b, err)
	}
	return r
}

// elide replaces a response's event types and cursor, which other tests cover, with "...".
func elide(t *testing.T, b []byte) map[string]any {
	t.Helper()
	r := decode(t, b)
	if result, ok := r["result"].(map[string]any); ok {
		if _, ok := result["cursor"]; ok {
			result["cursor"] = "..."
		}
		if events, ok := result["events"].([]any); ok && len(events) > 0 {
			result["events"] = []any{"..."}
		}
	}
	return r
}

func TestMethod(t *testing.T) {
	tests := []struct {
		desc   string
		body   string
		method string
		ok     bool
	}{
		{desc: "events request", body: `{"jsonrpc":"2.0","id":1,"method":"events/poll"}`, method: "events/poll", ok: true},
		{desc: "core request", body: `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`, method: "tools/list"},
		{desc: "events notification", body: `{"jsonrpc":"2.0","method":"events/whatever"}`, method: "events/whatever"},
		{desc: "batch", body: `[{"jsonrpc":"2.0","id":1,"method":"events/poll"}]`},
	}
	for _, tc := range tests {
		method, ok := Method([]byte(tc.body))
		if method != tc.method || ok != tc.ok {
			t.Errorf("%s: got: %q %v, want: %q %v", tc.desc, method, ok, tc.method, tc.ok)
		}
	}
}
