package core

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

func FetchContextKeys(ctx context.Context) (string, string, string, error) {
	apiURL, ok := ctx.Value(APIURLKey).(string)
	if !ok {
		return "", "", "", fmt.Errorf("apiURL not found in context")
	}
	orgID, ok := ctx.Value(OrgIDKey).(string)
	if !ok {
		return "", "", "", fmt.Errorf("orgID not found in context")
	}
	token, ok := ctx.Value(TokenKey).(string)
	if !ok {
		return "", "", "", fmt.Errorf("token not found in context")
	}
	return apiURL, orgID, token, nil
}
