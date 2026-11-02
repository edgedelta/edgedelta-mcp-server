package events

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/tools"
)

const (
	codeCallbackEndpoint = -32027

	defaultTTL = time.Hour
	minTTL     = time.Minute
	// maxTTL is short so a revoked principal, who can't refresh, stops getting events within the hour.
	maxTTL          = time.Hour
	verifiedFor     = 24 * time.Hour
	rotationOverlap = 10 * time.Minute
)

type principalKey struct{}

// WithPrincipal names who is calling, which webhook subscriptions are keyed by. It must stay the same across
// the caller's token refreshes, such as a user ID or an API token's ID.
func WithPrincipal(ctx context.Context, principal string) context.Context {
	return context.WithValue(ctx, principalKey{}, principal)
}

// Principal returns who WithPrincipal named, or "".
func Principal(ctx context.Context) string {
	p, ok := ctx.Value(principalKey{}).(string)
	if !ok {
		return ""
	}
	return p
}

// Subscription is a webhook subscription, as its Store keeps it.
type Subscription struct {
	ID        string         `json:"id"`
	OrgID     string         `json:"orgId"`
	Principal string         `json:"principal"`
	URL       string         `json:"url"`
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
	Secret    string         `json:"secret"`
	// PreviousSecret also signs deliveries until PreviousUntil, while the client rotates.
	PreviousSecret string    `json:"previousSecret,omitempty"`
	PreviousUntil  time.Time `json:"previousUntil,omitzero"`
	RefreshBefore  time.Time `json:"refreshBefore"`
	// Cursor is where delivery has reached: every event before it was delivered or given up on.
	Cursor      string    `json:"cursor"`
	Active      bool      `json:"active"`
	LastError   string    `json:"lastError,omitempty"`
	FailedSince time.Time `json:"failedSince,omitzero"`
	// NextRun is when Deliver should next run for it, and Attempts how many runs in a row failed.
	NextRun  time.Time `json:"nextRun"`
	Attempts int       `json:"attempts,omitempty"`
}

// Store keeps webhook subscriptions, since the server holds no state of its own.
type Store interface {
	Get(ctx context.Context, id string) (*Subscription, bool, error)
	Put(ctx context.Context, s *Subscription) error
	Delete(ctx context.Context, id string) error
	// VerifiedUntil and Verify keep when a principal's callback URL passed the challenge, by verificationKey.
	VerifiedUntil(ctx context.Context, key string) (time.Time, error)
	Verify(ctx context.Context, key string, until time.Time) error
}

// WithStore turns on webhook delivery, keeping subscriptions in store and posting through sender.
func WithStore(store Store, sender *Sender) Option {
	return func(h *Handler) {
		h.store = store
		h.sender = sender
	}
}

// canonical is JSON with object keys sorted, which encoding/json already does for maps.
func canonical(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func digest(parts ...any) (string, error) {
	s, err := canonical(parts)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8]), nil
}

func subscriptionID(principal, url, name string, args map[string]any) (string, error) {
	d, err := digest(principal, url, name, args)
	if err != nil {
		return "", err
	}
	return "sub_" + d, nil
}

func verificationKey(principal, url string) (string, error) {
	return digest(principal, url)
}

type deliveryParams struct {
	Mode   string `json:"mode"`
	URL    string `json:"url"`
	Secret string `json:"secret"`
}

// SubscribeParams are events/subscribe's params.
type SubscribeParams struct {
	Name      string          `json:"name"`
	Arguments map[string]any  `json:"arguments"`
	Delivery  deliveryParams  `json:"delivery"`
	Cursor    *string         `json:"cursor"`
	TTLMs     json.RawMessage `json:"ttlMs"`
	MaxAgeMs  *int64          `json:"maxAgeMs"`
}

// DeliveryStatus is how a subscription's deliveries are going.
type DeliveryStatus struct {
	Active      bool    `json:"active"`
	LastError   *string `json:"lastError"`
	FailedSince *string `json:"failedSince"`
}

// SubscribeResult is events/subscribe's result.
type SubscribeResult struct {
	ResultType     string          `json:"resultType"`
	ID             string          `json:"id"`
	RefreshBefore  string          `json:"refreshBefore"`
	Cursor         string          `json:"cursor"`
	Truncated      bool            `json:"truncated"`
	DeliveryStatus *DeliveryStatus `json:"deliveryStatus,omitempty"`
}

func grantTTL(raw json.RawMessage) (time.Duration, *Error) {
	if len(raw) == 0 || string(raw) == "null" {
		// No expiry isn't granted: a finite grant is the most a client asking for none can get.
		if string(raw) == "null" {
			return maxTTL, nil
		}
		return defaultTTL, nil
	}
	var ms int64
	if err := json.Unmarshal(raw, &ms); err != nil || ms <= 0 {
		return 0, invalidParams("ttlMs must be a positive integer")
	}
	return min(max(time.Duration(ms)*time.Millisecond, minTTL), maxTTL), nil
}

func (h *Handler) subscriber(ctx context.Context) (string, string, *Error) {
	if h.store == nil {
		return "", "", &Error{Code: codeMethodNotFound, Message: "webhook delivery is not offered here"}
	}
	keys, err := tools.FetchContextKeys(ctx)
	principal := Principal(ctx)
	if err != nil || principal == "" {
		return "", "", &Error{Code: codeForbidden, Message: "Forbidden", Data: map[string]any{"reason": "webhooks need an authenticated principal"}}
	}
	return keys.OrgID, principal, nil
}

// Subscribe answers events/subscribe: it creates or refreshes a webhook subscription, challenging a callback
// URL the principal hasn't verified before.
func (h *Handler) Subscribe(ctx context.Context, p SubscribeParams) (*SubscribeResult, *Error) {
	orgID, principal, e := h.subscriber(ctx)
	if e != nil {
		return nil, e
	}
	t, ok := h.lookupType(p.Name)
	if !ok {
		return nil, notFound(p.Name)
	}
	if p.Delivery.Mode != "" && p.Delivery.Mode != "webhook" {
		return nil, &Error{Code: codeUnsupported, Message: "Unsupported", Data: map[string]any{"feature": "deliveryMode", "value": p.Delivery.Mode}}
	}
	if p.Arguments == nil {
		p.Arguments = map[string]any{}
	}
	query, err := t.query(p.Arguments)
	if err != nil {
		return nil, invalidParams(err.Error())
	}
	if err := h.sender.check(p.Delivery.URL); err != nil {
		return nil, invalidParams(err.Error())
	}
	if _, ok := parseSecret(p.Delivery.Secret); !ok {
		return nil, invalidParams("delivery.secret must be whsec_ and base64 of 24 to 64 bytes")
	}
	ttl, e := grantTTL(p.TTLMs)
	if e != nil {
		return nil, e
	}
	id, err := subscriptionID(principal, p.Delivery.URL, p.Name, p.Arguments)
	if err != nil {
		return nil, internal(err.Error())
	}

	existing, found, err := h.store.Get(ctx, id)
	if err != nil {
		return nil, internal(fmt.Sprintf("reading the subscription: %v", err))
	}
	if e := h.verify(ctx, id, principal, p.Delivery.URL, p.Delivery.Secret); e != nil {
		return nil, e
	}

	now := h.now()
	s := &Subscription{
		ID: id, OrgID: orgID, Principal: principal, URL: p.Delivery.URL, Name: p.Name, Arguments: p.Arguments,
		Secret: p.Delivery.Secret, RefreshBefore: now.Add(ttl), Active: true, NextRun: now,
	}
	result := &SubscribeResult{ResultType: "complete", ID: id, RefreshBefore: s.RefreshBefore.UTC().Format(time.RFC3339Nano)}
	live := found && existing.RefreshBefore.After(now)
	switch {
	case live:
		s.Cursor = existing.Cursor
		s.LastError, s.FailedSince = existing.LastError, existing.FailedSince
		if existing.Active {
			s.NextRun, s.Attempts = existing.NextRun, existing.Attempts
		}
		if existing.Secret != s.Secret {
			s.PreviousSecret, s.PreviousUntil = existing.Secret, now.Add(rotationOverlap)
		} else if existing.PreviousUntil.After(now) {
			s.PreviousSecret, s.PreviousUntil = existing.PreviousSecret, existing.PreviousUntil
		}
		result.DeliveryStatus = existing.status()
	case p.Cursor != nil:
		c, ok := decodeCursor(*p.Cursor)
		if !ok {
			return nil, invalidParams("cursor is not one this server issued")
		}
		c, result.Truncated, e = h.rebase(ctx, query, c, p.MaxAgeMs)
		if e != nil {
			return nil, e
		}
		if s.Cursor, err = c.encode(); err != nil {
			return nil, internal(err.Error())
		}
	default:
		if s.Cursor, err = (cursor{Watermark: now.UnixMilli()}).encode(); err != nil {
			return nil, internal(err.Error())
		}
	}
	if err := h.store.Put(ctx, s); err != nil {
		return nil, internal(fmt.Sprintf("saving the subscription: %v", err))
	}
	result.Cursor = s.Cursor
	return result, nil
}

func (s *Subscription) status() *DeliveryStatus {
	d := &DeliveryStatus{Active: s.Active}
	if s.LastError != "" {
		d.LastError = &s.LastError
	}
	if !s.FailedSince.IsZero() {
		f := s.FailedSince.UTC().Format(time.RFC3339)
		d.FailedSince = &f
	}
	return d
}

// verify makes sure the principal's callback URL wants deliveries, by challenge unless it did recently.
func (h *Handler) verify(ctx context.Context, id, principal, url, secret string) *Error {
	key, err := verificationKey(principal, url)
	if err != nil {
		return internal(err.Error())
	}
	until, err := h.store.VerifiedUntil(ctx, key)
	if err != nil {
		return internal(fmt.Sprintf("reading verification: %v", err))
	}
	if until.After(h.now()) {
		return nil
	}
	reason, ok, err := h.sender.challenge(ctx, id, url, secret)
	if err != nil {
		return internal(fmt.Sprintf("challenging the callback: %v", err))
	}
	if !ok {
		return &Error{Code: codeCallbackEndpoint, Message: "CallbackEndpointError", Data: map[string]any{"reason": reason}}
	}
	if err := h.store.Verify(ctx, key, h.now().Add(verifiedFor)); err != nil {
		return internal(fmt.Sprintf("saving verification: %v", err))
	}
	return nil
}

// UnsubscribeParams are events/unsubscribe's params.
type UnsubscribeParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
	Delivery  struct {
		URL string `json:"url"`
	} `json:"delivery"`
}

// Unsubscribe answers events/unsubscribe.
func (h *Handler) Unsubscribe(ctx context.Context, p UnsubscribeParams) (map[string]any, *Error) {
	_, principal, e := h.subscriber(ctx)
	if e != nil {
		return nil, e
	}
	if p.Arguments == nil {
		p.Arguments = map[string]any{}
	}
	id, err := subscriptionID(principal, p.Delivery.URL, p.Name, p.Arguments)
	if err != nil {
		return nil, internal(err.Error())
	}
	_, found, err := h.store.Get(ctx, id)
	if err != nil {
		return nil, internal(fmt.Sprintf("reading the subscription: %v", err))
	}
	if !found {
		return nil, &Error{Code: codeNotFound, Message: "no such subscription", Data: map[string]any{"kind": "subscription"}}
	}
	if err := h.store.Delete(ctx, id); err != nil {
		return nil, internal(fmt.Sprintf("deleting the subscription: %v", err))
	}
	return map[string]any{"resultType": "complete"}, nil
}
