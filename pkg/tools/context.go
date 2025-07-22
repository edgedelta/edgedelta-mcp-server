package tools

import (
	"context"
	"fmt"
)

// ContextKey is a custom type for context keys to avoid collisions.
type ContextKey string

const (
	OrgIDKey  ContextKey = "orgID"
	TokenKey  ContextKey = "token"
	APIURLKey ContextKey = "apiURL"
)

func FetchContextKeys(ctx context.Context) (string, string, error) {
	orgID, ok := ctx.Value(OrgIDKey).(string)
	if !ok {
		return "", "", fmt.Errorf("orgID not found in context")
	}
	token, ok := ctx.Value(TokenKey).(string)
	if !ok {
		return "", "", fmt.Errorf("token not found in context")
	}
	return orgID, token, nil
}
