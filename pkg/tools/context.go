package tools

import (
	"context"
	"fmt"
)

// ContextKey is a custom type for context keys to avoid collisions.
type ContextKey string

const (
	OrgIDKey       ContextKey = "orgID"
	BearerTokenKey ContextKey = "bearerToken"
	EDTokenKey     ContextKey = "edToken"
	APIURLKey      ContextKey = "apiURL"
)

type ContextKeys struct {
	OrgID       string
	EDToken     string
	BearerToken string
}

func FetchContextKeys(ctx context.Context) (*ContextKeys, error) {
	orgID, ok := ctx.Value(OrgIDKey).(string)
	if !ok {
		return nil, fmt.Errorf("orgID not found in context")
	}

	var edToken string
	if val := ctx.Value(EDTokenKey); val != nil {
		edToken = val.(string)
	}

	var bearerToken string
	if val := ctx.Value(BearerTokenKey); val != nil {
		bearerToken = val.(string)
	}

	return &ContextKeys{
		OrgID:       orgID,
		EDToken:     edToken,
		BearerToken: bearerToken,
	}, nil
}
