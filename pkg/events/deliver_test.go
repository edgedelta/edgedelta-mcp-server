package events

import (
	"context"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
)

func TestDeliver(t *testing.T) {
	type run struct {
		at       time.Duration // since t0
		index    []searchItem
		status   int
		want     Outcome
		received []string
		failing  string
	}
	tests := []struct {
		desc   string
		search *fakeSearch
		runs   []run
	}{
		{
			desc: "delivers each event once, in order",
			runs: []run{
				{at: time.Minute, index: []searchItem{alert("a", t0.Add(10*time.Second)), alert("b", t0.Add(20*time.Second))}, want: Outcome{Delivered: 2}, received: []string{"a", "b"}},
				{at: 2 * time.Minute, want: Outcome{}, received: []string{}},
				{at: 3 * time.Minute, index: []searchItem{alert("late", t0.Add(70*time.Second))}, want: Outcome{Delivered: 1}, received: []string{"late"}},
			},
		},
		{
			desc: "retries from the first failure",
			runs: []run{
				{at: time.Minute, index: []searchItem{alert("a", t0.Add(10*time.Second)), alert("b", t0.Add(20*time.Second))}, status: http.StatusServiceUnavailable, received: []string{"a"}, failing: reason5xx},
				{at: 2 * time.Minute, want: Outcome{Delivered: 2}, received: []string{"a", "b"}},
			},
		},
		{
			desc: "skips an event too large to take",
			runs: []run{
				{at: time.Minute, index: []searchItem{alert("a", t0.Add(10*time.Second))}, status: http.StatusRequestEntityTooLarge, received: []string{"a"}},
				{at: 2 * time.Minute, received: []string{}},
			},
		},
		{
			desc: "ends on 410",
			runs: []run{
				{at: time.Minute, index: []searchItem{alert("a", t0.Add(10*time.Second))}, status: http.StatusGone, want: Outcome{Ended: true}, received: []string{"a"}},
			},
		},
		{
			desc: "ends past its grant, sending nothing",
			runs: []run{
				{at: 2 * time.Hour, index: []searchItem{alert("a", t0.Add(10*time.Second))}, want: Outcome{Ended: true}, received: []string{}},
			},
		},
		{
			desc:   "terminates when access is revoked",
			search: &fakeSearch{status: http.StatusForbidden},
			runs: []run{
				{at: time.Minute, want: Outcome{Ended: true}, received: []string{"terminated"}},
			},
		},
	}
	for _, tc := range tests {
		r := newReceiver(t)
		store := newMemStore()
		search := tc.search
		if search == nil {
			search = &fakeSearch{}
		}
		now := t0
		h := NewHandler(search, WithClock(func() time.Time { return now }), WithStore(store, testSender(r)))
		sub, e := h.Subscribe(principalContext("user1"), subscribeParams(r.srv.URL))
		if e != nil {
			t.Fatalf("%s: subscribe: %+v", tc.desc, e)
		}
		for i, run := range tc.runs {
			now = t0.Add(run.at)
			search.indexed = append(search.indexed, run.index...)
			r.mu.Lock()
			r.received, r.status = nil, run.status
			if r.status == 0 {
				r.status = http.StatusOK
			}
			r.mu.Unlock()

			s, _, err := store.Get(context.Background(), sub.ID)
			if err != nil {
				t.Fatalf("%s: get: %v", tc.desc, err)
			}
			got, err := h.Deliver(context.Background(), s)
			if err != nil {
				t.Fatalf("%s: run %d: %v", tc.desc, i, err)
			}
			if diff := cmp.Diff(run.want, got); diff != "" {
				t.Errorf("%s: run %d: outcome mismatch (-want, +got): %v", tc.desc, i, diff)
			}
			if diff := cmp.Diff(run.received, r.types()); diff != "" {
				t.Errorf("%s: run %d: received mismatch (-want, +got): %v", tc.desc, i, diff)
			}
			if s.LastError != run.failing {
				t.Errorf("%s: run %d: lastError got: %q, want: %q", tc.desc, i, s.LastError, run.failing)
			}
			wantNext := now.Add(runInterval)
			if run.failing != "" {
				wantNext = now.Add(backoff(s.Attempts))
			}
			if !run.want.Ended && !s.NextRun.Equal(wantNext) {
				t.Errorf("%s: run %d: nextRun got: %v, want: %v", tc.desc, i, s.NextRun, wantNext)
			}
			if err := store.Put(context.Background(), s); err != nil {
				t.Fatalf("%s: put: %v", tc.desc, err)
			}
		}
	}
}

func TestDeliverCarriesCursorAndSubscription(t *testing.T) {
	r := newReceiver(t)
	store := newMemStore()
	search := &fakeSearch{}
	now := t0
	h := NewHandler(search, WithClock(func() time.Time { return now }), WithStore(store, testSender(r)))
	sub, e := h.Subscribe(principalContext("user1"), subscribeParams(r.srv.URL))
	if e != nil {
		t.Fatalf("subscribe: %+v", e)
	}
	r.received = nil
	search.indexed = []searchItem{alert("a", t0.Add(10*time.Second))}
	now = t0.Add(time.Minute)
	s, _, err := store.Get(context.Background(), sub.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if _, err := h.Deliver(context.Background(), s); err != nil {
		t.Fatalf("deliver: %v", err)
	}
	got := r.received[0]
	if got["subscription"] != sub.ID || got["cursor"] != s.Cursor || got["name"] != monitorAlert {
		t.Errorf("delivery got: subscription %v, cursor %v, name %v, want: %v, %v, %v", got["subscription"], got["cursor"], got["name"], sub.ID, s.Cursor, monitorAlert)
	}
}

func TestDeliverDualSigns(t *testing.T) {
	r := newReceiver(t)
	store := newMemStore()
	search := &fakeSearch{}
	now := t0
	h := NewHandler(search, WithClock(func() time.Time { return now }), WithStore(store, testSender(r)))
	sub, e := h.Subscribe(principalContext("user1"), subscribeParams(r.srv.URL))
	if e != nil {
		t.Fatalf("subscribe: %+v", e)
	}
	rotated := "whsec_" + base64.StdEncoding.EncodeToString([]byte("a rotated secret of 24 b"))
	p := subscribeParams(r.srv.URL)
	p.Delivery.Secret = rotated
	if _, e := h.Subscribe(principalContext("user1"), p); e != nil {
		t.Fatalf("rotate: %+v", e)
	}

	tests := []struct {
		desc string
		at   time.Duration
		want []string
	}{
		{desc: "old secret verifies within the overlap", at: 5 * time.Minute, want: []string{"a"}},
		{desc: "only the new secret after it", at: 20 * time.Minute, want: []string{}},
	}
	for i, tc := range tests {
		now = t0.Add(tc.at)
		search.indexed = append(search.indexed, alert(string(rune('a'+i)), now.Add(-time.Second)))
		r.mu.Lock()
		r.received, r.secrets = nil, []string{testSecret}
		r.mu.Unlock()
		s, _, err := store.Get(context.Background(), sub.ID)
		if err != nil {
			t.Fatalf("%s: get: %v", tc.desc, err)
		}
		if _, err := h.Deliver(context.Background(), s); err != nil {
			t.Fatalf("%s: deliver: %v", tc.desc, err)
		}
		if err := store.Put(context.Background(), s); err != nil {
			t.Fatalf("%s: put: %v", tc.desc, err)
		}
		if diff := cmp.Diff(tc.want, r.types()); diff != "" {
			t.Errorf("%s: received mismatch (-want, +got): %v", tc.desc, diff)
		}
	}
}

func TestBackoff(t *testing.T) {
	tests := []struct {
		attempts int
		want     time.Duration
	}{
		{attempts: 1, want: 30 * time.Second},
		{attempts: 2, want: time.Minute},
		{attempts: 5, want: 8 * time.Minute},
		{attempts: 6, want: maxBackoff},
		{attempts: 60, want: maxBackoff},
	}
	for _, tc := range tests {
		if got := backoff(tc.attempts); got != tc.want {
			t.Errorf("attempts %d: got: %v, want: %v", tc.attempts, got, tc.want)
		}
	}
}
