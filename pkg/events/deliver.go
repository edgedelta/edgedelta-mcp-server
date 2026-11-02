package events

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/tools"
)

const (
	deliverBatch = 100
	// suspendAfter is how long deliveries may keep failing before they stop until the client's next refresh.
	suspendAfter = 24 * time.Hour
	runInterval  = 30 * time.Second
	maxBackoff   = 15 * time.Minute
)

// Outcome is what a delivery run did to a subscription.
type Outcome struct {
	Delivered int
	// More is set when events are still waiting, so the next run should come at once.
	More bool
	// Ended is set when the subscription is over and should be deleted: expired, gone (410) or terminated.
	Ended bool
}

type webhookBody struct {
	Occurrence
	Cursor string `json:"cursor"`
}

// Deliver posts the subscription's events since its cursor, oldest first, and moves its cursor past each one
// the endpoint takes. It stops at the first failure worth retrying, which the next run tries again, and before
// ctx's deadline while a post still has time to finish. The caller saves s afterwards with Put, error or not, or
// deletes it when the outcome has ended it; s.NextRun says when to run again. ErrConflict there means a client
// changed s meanwhile: drop the run, and its events go again under the same webhook-ids. ctx carries no caller:
// the Handler's client authenticates searches as the server itself.
func (h *Handler) Deliver(ctx context.Context, s *Subscription) (Outcome, error) {
	now := h.now()
	if !s.RefreshBefore.After(now) {
		return Outcome{Ended: true}, nil
	}
	if !s.Active {
		s.NextRun = s.RefreshBefore
		return Outcome{}, nil
	}
	ctx = context.WithValue(ctx, tools.OrgIDKey, s.OrgID)
	secrets := []string{s.Secret}
	if s.PreviousSecret != "" && s.PreviousUntil.After(now) {
		secrets = append(secrets, s.PreviousSecret)
	}

	t, ok := lookupType(s.Name)
	if !ok {
		return h.terminate(ctx, s, secrets, notFound(s.Name))
	}
	query, err := t.query(s.Arguments)
	if err != nil {
		return h.terminate(ctx, s, secrets, &Error{Code: codeUnsupported, Message: "the event type no longer takes these arguments"})
	}
	c, ok := decodeCursor(s.Cursor)
	if !ok {
		return Outcome{}, fmt.Errorf("subscription %s has an invalid cursor", s.ID)
	}

	c, truncated, e := h.rebase(ctx, query, c, nil)
	if e != nil {
		return h.searchFailed(s, e)
	}
	pg, e := h.page(ctx, query, c, deliverBatch)
	if e != nil {
		return h.searchFailed(s, e)
	}
	if truncated || pg.skipped {
		d, err := h.postGap(ctx, s, secrets, pg.start)
		if err != nil {
			return Outcome{}, err
		}
		if d.Status == http.StatusGone {
			return Outcome{Ended: true}, nil
		}
		if d.Retryable() {
			h.failed(s, d.Reason)
			return Outcome{}, nil
		}
	}

	out := Outcome{More: pg.more}
	n := 0
	for ; n < len(pg.items); n++ {
		if deadline, ok := ctx.Deadline(); ok && time.Until(deadline) < deliveryTimeout {
			s.NextRun, out.More = now, true
			break
		}
		c, _ := pg.after(n + 1)
		next, err := c.encode()
		if err != nil {
			return out, err
		}
		it := pg.items[n]
		body, err := json.Marshal(webhookBody{Occurrence: it.occurrence(s.Name, h.appURL), Cursor: next})
		if err != nil {
			return out, err
		}
		d, err := h.sender.Post(ctx, s.ID, "msg_"+s.ID+"_"+it.id(), s.URL, secrets, body)
		if err != nil {
			return out, err
		}
		if d.Status == http.StatusGone {
			return Outcome{Delivered: out.Delivered, Ended: true}, nil
		}
		if d.Retryable() {
			h.failed(s, d.Reason)
			out.More = false
			break
		}
		if d.Reason == "" {
			out.Delivered++
		}
	}
	if n == len(pg.items) {
		s.LastError, s.FailedSince, s.Attempts = "", time.Time{}, 0
		s.NextRun = now.Add(runInterval)
		if out.More {
			s.NextRun = now
		}
	}
	c, _ = pg.after(n)
	if s.Cursor, err = c.encode(); err != nil {
		return out, err
	}
	return out, nil
}

func (h *Handler) failed(s *Subscription, reason string) {
	s.LastError = reason
	if s.FailedSince.IsZero() {
		s.FailedSince = h.now()
	}
	s.Attempts++
	s.NextRun = h.now().Add(backoff(s.Attempts))
	if h.now().Sub(s.FailedSince) > suspendAfter {
		s.Active = false
	}
}

// searchFailed waits to retry. Searches run as the server, so even a refusal says nothing about the principal.
func (h *Handler) searchFailed(s *Subscription, e *Error) (Outcome, error) {
	s.Attempts++
	s.NextRun = h.now().Add(backoff(s.Attempts))
	return Outcome{}, fmt.Errorf("searching events for %s: %s", s.ID, e.Message)
}

// backoff doubles from the run interval with each failed attempt, up to maxBackoff.
func backoff(attempts int) time.Duration {
	d := runInterval
	for i := 1; i < attempts && d < maxBackoff; i++ {
		d *= 2
	}
	return min(d, maxBackoff)
}

// postGap tells the endpoint that events before c were never delivered, and moves s's cursor to c once it has.
func (h *Handler) postGap(ctx context.Context, s *Subscription, secrets []string, c cursor) (Delivery, error) {
	next, err := c.encode()
	if err != nil {
		return Delivery{}, err
	}
	body, err := json.Marshal(map[string]string{"type": "gap", "cursor": next})
	if err != nil {
		return Delivery{}, err
	}
	id, err := digest(next)
	if err != nil {
		return Delivery{}, err
	}
	d, err := h.sender.Post(ctx, s.ID, "msg_"+s.ID+"_gap_"+id, s.URL, secrets, body)
	if err != nil {
		return Delivery{}, err
	}
	if d.Reason == "" {
		s.Cursor = next
	}
	return d, nil
}

// terminate tells the endpoint the subscription is over, as well as it can, and ends it. The error goes to a
// third party, so it carries only its code and message.
func (h *Handler) terminate(ctx context.Context, s *Subscription, secrets []string, e *Error) (Outcome, error) {
	body, err := json.Marshal(map[string]any{"type": "terminated", "error": map[string]any{"code": e.Code, "message": e.Message}})
	if err != nil {
		return Outcome{}, err
	}
	if _, err := h.sender.Post(ctx, s.ID, "msg_"+s.ID+"_terminated", s.URL, secrets, body); err != nil {
		return Outcome{}, err
	}
	return Outcome{Ended: true}, nil
}
