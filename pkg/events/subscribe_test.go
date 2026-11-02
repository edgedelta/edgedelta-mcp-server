package events

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/tools"

	"github.com/google/go-cmp/cmp"
)

type memStore struct {
	mu       sync.Mutex
	subs     map[string]*Subscription
	verified map[string]time.Time
}

func newMemStore() *memStore {
	return &memStore{subs: map[string]*Subscription{}, verified: map[string]time.Time{}}
}

func (m *memStore) Get(_ context.Context, id string) (*Subscription, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.subs[id]
	if !ok {
		return nil, false, nil
	}
	c := *s
	return &c, true, nil
}

func (m *memStore) Put(_ context.Context, s *Subscription) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	c := *s
	m.subs[s.ID] = &c
	return nil
}

func (m *memStore) Delete(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.subs, id)
	return nil
}

func (m *memStore) VerifiedUntil(_ context.Context, key string) (time.Time, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.verified[key], nil
}

func (m *memStore) Verify(_ context.Context, key string, until time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.verified[key] = until
	return nil
}

var testSecret = "whsec_" + base64.StdEncoding.EncodeToString([]byte("0123456789abcdef01234567"))

// receiver is a webhook endpoint that checks signatures against its secrets and records what it's sent.
type receiver struct {
	t        *testing.T
	srv      *httptest.Server
	mu       sync.Mutex
	secrets  []string
	echo     bool
	status   int
	received []map[string]any
}

func newReceiver(t *testing.T) *receiver {
	t.Helper()
	r := &receiver{t: t, secrets: []string{testSecret}, echo: true, status: http.StatusOK}
	r.srv = httptest.NewTLSServer(http.HandlerFunc(r.serve))
	t.Cleanup(r.srv.Close)
	return r
}

func (r *receiver) serve(w http.ResponseWriter, req *http.Request) {
	body, err := io.ReadAll(req.Body)
	if err != nil {
		r.t.Errorf("receiver: read: %v", err)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.verifies(req.Header, body) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	var msg map[string]any
	if err := json.Unmarshal(body, &msg); err != nil {
		r.t.Errorf("receiver: decode: %v", err)
		return
	}
	msg["subscription"] = req.Header.Get("X-MCP-Subscription-Id")
	r.received = append(r.received, msg)
	if msg["type"] == "verification" {
		if r.echo {
			fmt.Fprintf(w, `{"challenge":%q}`, msg["challenge"])
		}
		return
	}
	w.WriteHeader(r.status)
}

func (r *receiver) verifies(h http.Header, body []byte) bool {
	for _, secret := range r.secrets {
		key, ok := parseSecret(secret)
		if !ok {
			r.t.Fatalf("receiver: bad secret %q", secret)
		}
		mac := hmac.New(sha256.New, key)
		mac.Write([]byte(h.Get("webhook-id") + "." + h.Get("webhook-timestamp") + "."))
		mac.Write(body)
		want := "v1," + base64.StdEncoding.EncodeToString(mac.Sum(nil))
		for _, sig := range strings.Fields(h.Get("webhook-signature")) {
			if hmac.Equal([]byte(sig), []byte(want)) {
				return true
			}
		}
	}
	return false
}

func (r *receiver) types() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []string{}
	for _, m := range r.received {
		if t, ok := m["type"].(string); ok {
			out = append(out, t)
			continue
		}
		out = append(out, m["eventId"].(string))
	}
	return out
}

func testSender(r *receiver) *Sender {
	s := NewSender(true)
	s.client.Transport.(*http.Transport).TLSClientConfig = r.srv.Client().Transport.(*http.Transport).TLSClientConfig
	return s
}

func principalContext(principal string) context.Context {
	ctx := context.WithValue(context.Background(), tools.OrgIDKey, "org1")
	return WithPrincipal(ctx, principal)
}

func subscribeParams(url string) SubscribeParams {
	return SubscribeParams{Name: monitorAlert, Arguments: map[string]any{}, Delivery: deliveryParams{URL: url, Secret: testSecret}}
}

func TestSubscribe(t *testing.T) {
	r := newReceiver(t)
	store := newMemStore()
	now := t0
	h := NewHandler(&fakeSearch{}, WithClock(func() time.Time { return now }), WithStore(store, testSender(r)))
	ctx := principalContext("user1")

	first, e := h.Subscribe(ctx, subscribeParams(r.srv.URL))
	if e != nil {
		t.Fatalf("subscribe: %+v", e)
	}
	if first.DeliveryStatus != nil || first.Truncated || !strings.HasPrefix(first.ID, "sub_") {
		t.Errorf("first subscribe got: %+v, want a new subscription", first)
	}
	if got, want := first.RefreshBefore, t0.Add(defaultTTL).Format(time.RFC3339Nano); got != want {
		t.Errorf("refreshBefore got: %v, want: %v", got, want)
	}

	now = t0.Add(10 * time.Minute)
	p := subscribeParams(r.srv.URL)
	p.TTLMs = json.RawMessage("172800000")
	p.Delivery.Secret = "whsec_" + base64.StdEncoding.EncodeToString([]byte("another secret, 24 bytes"))
	second, e := h.Subscribe(ctx, p)
	if e != nil {
		t.Fatalf("refresh: %+v", e)
	}
	if second.ID != first.ID || second.Cursor != first.Cursor {
		t.Errorf("refresh got: %s %s, want the same subscription and cursor %s %s", second.ID, second.Cursor, first.ID, first.Cursor)
	}
	if diff := cmp.Diff(&DeliveryStatus{Active: true}, second.DeliveryStatus); diff != "" {
		t.Errorf("deliveryStatus mismatch (-want, +got): %v", diff)
	}
	if got, want := second.RefreshBefore, now.Add(maxTTL).Format(time.RFC3339Nano); got != want {
		t.Errorf("clamped refreshBefore got: %v, want: %v", got, want)
	}
	s, _, err := store.Get(ctx, first.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if s.PreviousSecret != testSecret || !s.PreviousUntil.Equal(now.Add(rotationOverlap)) {
		t.Errorf("rotation got: %q until %v, want the old secret for %v", s.PreviousSecret, s.PreviousUntil, rotationOverlap)
	}

	other, e := h.Subscribe(principalContext("user2"), subscribeParams(r.srv.URL))
	if e != nil {
		t.Fatalf("other principal: %+v", e)
	}
	if other.ID == first.ID {
		t.Errorf("another principal got the same subscription %s", other.ID)
	}
	if diff := cmp.Diff([]string{"verification", "verification"}, r.types()); diff != "" {
		t.Errorf("challenges mismatch, one per principal and url (-want, +got): %v", diff)
	}

	if _, e := h.Unsubscribe(ctx, UnsubscribeParams{Name: monitorAlert, Delivery: struct {
		URL string `json:"url"`
	}{URL: r.srv.URL}}); e != nil {
		t.Fatalf("unsubscribe: %+v", e)
	}
	if _, found, err := store.Get(ctx, first.ID); err != nil || found {
		t.Errorf("after unsubscribe got: found %v, err %v, want gone", found, err)
	}
}

func TestSubscribeErrors(t *testing.T) {
	r := newReceiver(t)
	refusing := newReceiver(t)
	refusing.echo = false
	tests := []struct {
		desc   string
		ctx    context.Context
		params func(p *SubscribeParams)
		store  bool
		want   int
		data   any
	}{
		{desc: "no store", ctx: principalContext("u"), want: codeMethodNotFound},
		{desc: "no principal", ctx: orgContext(), store: true, want: codeForbidden},
		{desc: "unknown type", ctx: principalContext("u"), store: true, params: func(p *SubscribeParams) { p.Name = "nope" }, want: codeNotFound, data: map[string]any{"kind": "event"}},
		{desc: "push", ctx: principalContext("u"), store: true, params: func(p *SubscribeParams) { p.Delivery.Mode = "push" }, want: codeUnsupported},
		{desc: "plain http", ctx: principalContext("u"), store: true, params: func(p *SubscribeParams) { p.Delivery.URL = "http://example.com/hook" }, want: codeInvalidParams},
		{desc: "short secret", ctx: principalContext("u"), store: true, params: func(p *SubscribeParams) { p.Delivery.Secret = "whsec_c2hvcnQ=" }, want: codeInvalidParams},
		{desc: "bad arguments", ctx: principalContext("u"), store: true, params: func(p *SubscribeParams) { p.Arguments = map[string]any{"x": 1} }, want: codeInvalidParams},
		{desc: "bad ttl", ctx: principalContext("u"), store: true, params: func(p *SubscribeParams) { p.TTLMs = json.RawMessage(`"soon"`) }, want: codeInvalidParams},
		{
			desc: "no echo", ctx: principalContext("u"), store: true, params: func(p *SubscribeParams) { p.Delivery.URL = refusing.srv.URL },
			want: codeCallbackEndpoint, data: map[string]any{"reason": reasonChallenge},
		},
		{
			desc: "unreachable", ctx: principalContext("u"), store: true, params: func(p *SubscribeParams) { p.Delivery.URL = "https://127.0.0.1:1/hook" },
			want: codeCallbackEndpoint, data: map[string]any{"reason": reasonRefused},
		},
	}
	for _, tc := range tests {
		opts := []Option{WithClock(func() time.Time { return t0 })}
		if tc.store {
			opts = append(opts, WithStore(newMemStore(), testSender(r)))
		}
		h := NewHandler(&fakeSearch{}, opts...)
		p := subscribeParams(r.srv.URL)
		if tc.params != nil {
			tc.params(&p)
		}
		_, e := h.Subscribe(tc.ctx, p)
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

func TestCheckCallback(t *testing.T) {
	tests := []struct {
		url string
		ok  bool
	}{
		{url: "https://canvas.example.com/mcp-events/1", ok: true},
		{url: "http://canvas.example.com/mcp-events/1"},
		{url: "https://user:pass@canvas.example.com/"},
		{url: "https://127.0.0.1/hook"},
		{url: "https://10.1.2.3/hook"},
		{url: "https://[::1]/hook"},
		{url: "https://169.254.169.254/latest"},
		{url: "https://100.64.0.1/hook"},
		{url: "https://8.8.8.8/hook", ok: true},
	}
	s := NewSender(false)
	for _, tc := range tests {
		if err := s.check(tc.url); (err == nil) != tc.ok {
			t.Errorf("%s: got: %v, want ok: %v", tc.url, err, tc.ok)
		}
	}
}

func TestDialRefusesPrivate(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer srv.Close()
	s := NewSender(false)
	got, err := s.Post(context.Background(), "sub_1", srv.URL, []string{testSecret}, "", []byte(`{}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if got.Reason != reasonRefused {
		t.Errorf("reason got: %q, want: %q", got.Reason, reasonRefused)
	}
}
