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

	floor := now - h.maxReplay.Milliseconds()
	if p.MaxAgeMs != nil && *p.MaxAgeMs > 0 {
		floor = max(floor, now-*p.MaxAgeMs)
	}
	if floor > c.Watermark {
		skipped, e := h.anyUnseen(ctx, query, c, floor-1)
		if e != nil {
			return nil, e
		}
		result.Truncated = skipped
		c = c.advance(floor)
	}

	limit := maxEvents + len(c.Seen) + 1
	items, e := h.search(ctx, query, c.Watermark, now, limit)
	if e != nil {
		return nil, e
	}
	fresh := unseen(items, c)
	batch := fresh[:min(len(fresh), maxEvents)]
	result.HasMore = len(fresh) > maxEvents

	for _, it := range batch {
		result.Events = append(result.Events, it.occurrence(t.Name, h.appURL))
		c.Seen = append(c.Seen, seenItem{T: it.Timestamp, ID: it.id()})
	}

	// Indexing is eventually consistent, so the watermark trails now by the lag, and
	// stops at the last delivered event while more are waiting.
	next := now - h.lag.Milliseconds()
	if result.HasMore {
		next = min(next, batch[len(batch)-1].Timestamp)
	}
	c = c.advance(max(c.Watermark, next))
	if len(c.Seen) > maxSeen {
		c = c.advance(c.Seen[len(c.Seen)-maxSeen-1].T + 1)
	}
	return result.withCursor(c)
}

func (r *PollResult) withCursor(c cursor) (*PollResult, *Error) {
	s, err := c.encode()
	if err != nil {
		return nil, internal(fmt.Sprintf("encoding cursor: %v", err))
	}
	r.Cursor = s
	return r, nil
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
