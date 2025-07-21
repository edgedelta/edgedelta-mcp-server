package server

import (
	"context"
	"net/http"
)

type Key string

var APITokenKey Key = "apiToken"

type AuthTransport struct {
	RoundTripper   http.RoundTripper
	APITokenHeader string
}

func (t *AuthTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if token, ok := TokenKeyFromContext(req.Context()); ok {
		req.Header.Set(t.APITokenHeader, token)
	}
	return t.RoundTripper.RoundTrip(req)
}

func TokenKeyFromContext(ctx context.Context) (string, bool) {
	value := ctx.Value(APITokenKey)
	if value == nil {
		return "", false
	}

	token, ok := value.(string)
	if !ok {
		return "", false
	}

	return token, true
}

func SetTokenInContext(ctx context.Context, apiToken string) context.Context {
	return context.WithValue(ctx, APITokenKey, apiToken)
}
