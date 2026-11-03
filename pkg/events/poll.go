package events

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/tools"
)

const (
	timeFormat = "2006-01-02T15:04:05.000Z"

	defaultMaxEvents = 50
	maxMaxEvents     = 100
	// maxSeen bounds the IDs a cursor carries, so a search stays under the API's limit of 1000.
	maxSeen    = 500
	nextPollMs = 30_000
	cursorTag  = "v1."
)

// cursor is a poll position: everything before Watermark was delivered or skipped, and Seen holds what was
// delivered at or after it, so a search from Watermark finds late-indexed events without repeating any.
type cursor struct {
	Watermark int64      `json:"w"`
	Seen      []seenItem `json:"s,omitempty"`
}

type seenItem struct {
	T  int64  `json:"t"`
	ID string `json:"i"`
}

func (c cursor) encode() (string, error) {
	b, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return cursorTag + base64.RawURLEncoding.EncodeToString(b), nil
}

func decodeCursor(s string) (cursor, bool) {
	raw, ok := strings.CutPrefix(s, cursorTag)
	if !ok {
		return cursor{}, false
	}
	b, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return cursor{}, false
	}
	var c cursor
	if err := json.Unmarshal(b, &c); err != nil || c.Watermark <= 0 || len(c.Seen) > maxSeen {
		return cursor{}, false
	}
	return c, true
}

// PollParams are events/poll's params.
type PollParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
	Cursor    *string        `json:"cursor"`
	MaxAgeMs  *int64         `json:"maxAgeMs"`
	MaxEvents *int           `json:"maxEvents"`
}

// PollResult is events/poll's result.
type PollResult struct {
	ResultType string       `json:"resultType"`
	Events     []Occurrence `json:"events"`
	Cursor     string       `json:"cursor"`
	Truncated  bool         `json:"truncated"`
	HasMore    bool         `json:"hasMore"`
	NextPollMs int          `json:"nextPollMs"`
}

// Poll answers events/poll: the subscription's events since its cursor, oldest first.
func (h *Handler) Poll(ctx context.Context, p PollParams) (*PollResult, *Error) {
	t, ok := lookupType(p.Name)
	if !ok {
		return nil, notFound(p.Name)
	}
	query, err := t.query(p.Arguments)
	if err != nil {
		return nil, invalidParams(err.Error())
	}
	maxEvents := defaultMaxEvents
	if p.MaxEvents != nil {
		if *p.MaxEvents < 1 {
			return nil, invalidParams("maxEvents must be at least 1")
		}
		maxEvents = min(*p.MaxEvents, maxMaxEvents)
	}

	now := h.now().UnixMilli()
	result := &PollResult{ResultType: "complete", Events: []Occurrence{}, NextPollMs: nextPollMs}
	if p.Cursor == nil {
		return result.withCursor(cursor{Watermark: now})
	}
	c, ok := decodeCursor(*p.Cursor)
	if !ok {
		return nil, invalidParams("cursor is not one this server issued")
	}
	c, truncated, e := h.rebase(ctx, query, c, p.MaxAgeMs)
	if e != nil {
		return nil, e
	}
	pg, e := h.page(ctx, query, c, maxEvents)
	if e != nil {
		return nil, e
	}
	for _, it := range pg.items {
		result.Events = append(result.Events, it.occurrence(t.Name, h.appURL))
	}
	result.Truncated = truncated || pg.skipped
	result.HasMore = pg.more
	next, _ := pg.after(len(pg.items))
	return result.withCursor(next)
}

// page is a batch of events found from a cursor.
type page struct {
	start cursor
	items []searchItem
	more  bool
	// next is the timestamp of the first event waiting past items, while more wait.
	next int64
	// settled is how far behind now the cursor may move: indexing is eventually consistent.
	settled int64
	// skipped is set when start had to move past events never delivered, to make room in a full Seen.
	skipped bool
}

func (h *Handler) page(ctx context.Context, query string, c cursor, maxEvents int) (page, *Error) {
	pg, e := h.fetch(ctx, query, c, maxEvents)
	if e != nil || len(pg.items) > 0 || !pg.more {
		return pg, e
	}
	// Seen is full of events at the next one's timestamp, so the cursor can't hold another there.
	pg, e = h.fetch(ctx, query, c.advance(max(pg.next+1, c.room())), maxEvents)
	pg.skipped = true
	return pg, e
}

// fetch finds the events after a cursor, as many as the cursor can take without moving past one undelivered.
func (h *Handler) fetch(ctx context.Context, query string, c cursor, maxEvents int) (page, *Error) {
	now := h.now().UnixMilli()
	items, e := h.search(ctx, query, c.Watermark, now, maxEvents+len(c.Seen)+1)
	if e != nil {
		return page{}, e
	}
	fresh := unseen(items, c)
	pg := page{start: c, items: fresh[:min(len(fresh), maxEvents)], settled: now - h.lag.Milliseconds()}
	if len(fresh) > len(pg.items) {
		pg.more, pg.next = true, fresh[len(pg.items)].Timestamp
	}
	for n := 1; n <= len(pg.items); n++ {
		if _, skips := pg.after(n); skips {
			pg.more, pg.next, pg.items = true, pg.items[n-1].Timestamp, pg.items[:n-1]
			break
		}
	}
	return pg, nil
}

// after is the cursor once the page's first n events are delivered, and whether it moved past one still
// waiting. While more wait, it stops at the last one delivered.
func (p page) after(n int) (cursor, bool) {
	c := p.start
	c.Seen = slices.Clone(c.Seen)
	for _, it := range p.items[:n] {
		c.Seen = append(c.Seen, seenItem{T: it.Timestamp, ID: it.id()})
	}
	waiting := p.more || n < len(p.items)
	next := p.settled
	if waiting {
		next = c.Watermark
		if n > 0 {
			next = min(p.settled, p.items[n-1].Timestamp)
		}
	}
	c = c.advance(max(c.Watermark, next))
	if len(c.Seen) > maxSeen {
		c = c.advance(c.Seen[len(c.Seen)-maxSeen-1].T + 1)
	}
	if !waiting {
		return c, false
	}
	first := p.next
	if n < len(p.items) {
		first = p.items[n].Timestamp
	}
	return c, c.Watermark > first
}

// room is the lowest watermark that leaves Seen room for one more event.
func (c cursor) room() int64 {
	if len(c.Seen) < maxSeen {
		return c.Watermark
	}
	return c.Seen[len(c.Seen)-maxSeen].T + 1
}

func (r *PollResult) withCursor(c cursor) (*PollResult, *Error) {
	s, err := c.encode()
	if err != nil {
		return nil, internal(fmt.Sprintf("encoding cursor: %v", err))
	}
	r.Cursor = s
	return r, nil
}

// rebase moves a cursor up to the replay floor, now less maxAgeMs or the server's ceiling, and reports whether
// that skipped an event.
func (h *Handler) rebase(ctx context.Context, query string, c cursor, maxAgeMs *int64) (cursor, bool, *Error) {
	now := h.now().UnixMilli()
	floor := now - h.maxReplay.Milliseconds()
	if maxAgeMs != nil && *maxAgeMs > 0 {
		floor = max(floor, now-*maxAgeMs)
	}
	if floor <= c.Watermark {
		return c, false, nil
	}
	skipped, e := h.anyUnseen(ctx, query, c, floor-1)
	if e != nil {
		return cursor{}, false, e
	}
	return c.advance(floor), skipped, nil
}

// advance moves the watermark to w and forgets what was delivered before it.
func (c cursor) advance(w int64) cursor {
	seen := slices.DeleteFunc(slices.Clone(c.Seen), func(s seenItem) bool { return s.T < w })
	slices.SortStableFunc(seen, func(a, b seenItem) int { return int(a.T - b.T) })
	return cursor{Watermark: w, Seen: seen}
}

func unseen(items []searchItem, c cursor) []searchItem {
	out := make([]searchItem, 0, len(items))
	for _, it := range items {
		id := it.id()
		if slices.ContainsFunc(c.Seen, func(s seenItem) bool { return s.ID == id }) {
			continue
		}
		out = append(out, it)
	}
	return out
}

// anyUnseen reports whether events between the cursor and to were never delivered.
func (h *Handler) anyUnseen(ctx context.Context, query string, c cursor, to int64) (bool, *Error) {
	items, e := h.search(ctx, query, c.Watermark, to, len(c.Seen)+1)
	if e != nil {
		return false, e
	}
	return len(unseen(items, c)) > 0, nil
}

type searchResponse struct {
	Items []searchItem `json:"items"`
}

// search finds the events in [from, to], both in milliseconds and inclusive, oldest first.
func (h *Handler) search(ctx context.Context, query string, from, to int64, limit int) ([]searchItem, *Error) {
	if to < from {
		return nil, nil
	}
	// The API's from is exclusive in ascending order, so the search starts a millisecond early.
	items, e := h.searchRange(ctx, query, from-1, to, limit)
	return slices.DeleteFunc(items, func(it searchItem) bool { return it.Timestamp < from }), e
}

func (h *Handler) searchRange(ctx context.Context, query string, from, to int64, limit int) ([]searchItem, *Error) {
	keys, err := tools.FetchContextKeys(ctx)
	if err != nil {
		return nil, &Error{Code: codeForbidden, Message: "Forbidden", Data: map[string]any{"reason": "no organization"}}
	}
	u, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/events/search", h.client.APIURL(), url.PathEscape(keys.OrgID)))
	if err != nil {
		return nil, internal(err.Error())
	}
	q := url.Values{}
	q.Set("query", query)
	q.Set("from", time.UnixMilli(from).UTC().Format(timeFormat))
	q.Set("to", time.UnixMilli(to).UTC().Format(timeFormat))
	q.Set("limit", strconv.Itoa(limit))
	q.Set("order", "asc")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, internal(err.Error())
	}
	req.Header.Set("Accept", "application/json")
	if keys.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+keys.BearerToken)
	} else if keys.EDToken != "" {
		req.Header.Set("X-ED-API-Token", keys.EDToken)
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, internal(fmt.Sprintf("searching events: %v", err))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 32<<20))
	if closeErr := resp.Body.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		return nil, internal(fmt.Sprintf("reading events: %v", err))
	}
	body = bytes.TrimSpace(body)
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return nil, &Error{Code: codeForbidden, Message: "Forbidden", Data: map[string]any{"reason": string(body)}}
	case resp.StatusCode == http.StatusBadRequest:
		return nil, invalidParams(fmt.Sprintf("the event search refused the query %s: %s", query, body))
	case resp.StatusCode != http.StatusOK:
		return nil, internal(fmt.Sprintf("event search answered %d: %s", resp.StatusCode, body))
	}
	var r searchResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, internal(fmt.Sprintf("decoding events: %v", err))
	}
	return r.Items, nil
}
