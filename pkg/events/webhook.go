package events

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	maxBodyBytes    = 256 << 10
	deliveryTimeout = 10 * time.Second
)

// Failure reasons, as the SEP's deliveryStatus.lastError and CallbackEndpointError name them.
const (
	reasonRefused   = "connection_refused"
	reasonTimeout   = "timeout"
	reasonTLS       = "tls_error"
	reason4xx       = "http_4xx"
	reason5xx       = "http_5xx"
	reasonChallenge = "challenge_failed"
)

// parseSecret returns a whsec_ secret's key, which must be 24 to 64 bytes.
func parseSecret(s string) ([]byte, bool) {
	raw, ok := strings.CutPrefix(s, "whsec_")
	if !ok {
		return nil, false
	}
	key, err := base64.StdEncoding.DecodeString(raw)
	if err != nil || len(key) < 24 || len(key) > 64 {
		return nil, false
	}
	return key, true
}

// sign returns a Standard Webhooks signature header, one v1 signature per secret.
func sign(secrets []string, id string, at time.Time, body []byte) (string, error) {
	sigs := make([]string, 0, len(secrets))
	for _, s := range secrets {
		key, ok := parseSecret(s)
		if !ok {
			return "", fmt.Errorf("invalid secret")
		}
		mac := hmac.New(sha256.New, key)
		mac.Write(append(fmt.Appendf(nil, "%s.%d.", id, at.Unix()), body...))
		sigs = append(sigs, "v1,"+base64.StdEncoding.EncodeToString(mac.Sum(nil)))
	}
	return strings.Join(sigs, " "), nil
}

func randomHex() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Delivery is one webhook POST's outcome.
type Delivery struct {
	Status int
	Body   []byte
	// Reason names a failure, empty on a 2xx.
	Reason string
}

// Retryable reports whether a failed delivery is worth sending again: 410 and 413 are not.
func (d Delivery) Retryable() bool {
	return d.Reason != "" && d.Status != http.StatusGone && d.Status != http.StatusRequestEntityTooLarge
}

// Sender posts signed webhooks to public https endpoints.
type Sender struct {
	client       *http.Client
	allowPrivate bool
	now          func() time.Time
}

// SenderOption configures a Sender.
type SenderOption func(*http.Transport)

// WithRootCAs makes the Sender trust the pool's CAs instead of the system's, for private deployments and tests.
func WithRootCAs(pool *x509.CertPool) SenderOption {
	return func(t *http.Transport) {
		t.TLSClientConfig = &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}
	}
}

// NewSender returns a Sender that refuses redirects and addresses that aren't globally routable, checked as
// it dials so DNS rebinding can't get past it. allowPrivate turns that check off, for tests.
func NewSender(allowPrivate bool, opts ...SenderOption) *Sender {
	dialer := &net.Dialer{Timeout: deliveryTimeout}
	if !allowPrivate {
		dialer.Control = refusePrivate
	}
	transport := &http.Transport{
		DialContext:         dialer.DialContext,
		TLSHandshakeTimeout: deliveryTimeout,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
		TLSClientConfig:     &tls.Config{MinVersion: tls.VersionTLS12},
	}
	for _, opt := range opts {
		opt(transport)
	}
	return &Sender{
		client: &http.Client{
			Transport:     transport,
			Timeout:       deliveryTimeout,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
		allowPrivate: allowPrivate,
		now:          time.Now,
	}
}

func refusePrivate(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return err
	}
	if !public(ip.Unmap()) {
		return fmt.Errorf("%s is not a public address", ip)
	}
	return nil
}

var nonPublic = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001:db8::/32"),
}

func public(ip netip.Addr) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast() {
		return false
	}
	for _, p := range nonPublic {
		if p.Contains(ip) {
			return false
		}
	}
	return true
}

// check says why a callback URL can't take deliveries, before any is tried.
func (s *Sender) check(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("delivery.url is not a URL")
	}
	if u.Scheme != "https" || u.Host == "" {
		return fmt.Errorf("delivery.url must be an https URL")
	}
	if u.User != nil {
		return fmt.Errorf("delivery.url must not carry credentials")
	}
	if ip, err := netip.ParseAddr(strings.Trim(u.Hostname(), "[]")); err == nil && !s.allowPrivate && !public(ip.Unmap()) {
		return fmt.Errorf("delivery.url must be a public address")
	}
	return nil
}

// Post signs body with each secret, the current one first, and posts it to the subscription's URL. id is the
// webhook-id, which must stay the same when the message is tried again, for receivers to dedupe.
func (s *Sender) Post(ctx context.Context, subscriptionID, id, target string, secrets []string, body []byte) (Delivery, error) {
	if len(body) > maxBodyBytes {
		return Delivery{Status: http.StatusRequestEntityTooLarge, Reason: reason4xx}, nil
	}
	at := s.now()
	signature, err := sign(secrets, id, at, body)
	if err != nil {
		return Delivery{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return Delivery{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Edge Delta MCP Events")
	req.Header.Set("webhook-id", id)
	req.Header.Set("webhook-timestamp", strconv.FormatInt(at.Unix(), 10))
	req.Header.Set("webhook-signature", signature)
	req.Header.Set("X-MCP-Subscription-Id", subscriptionID)
	resp, err := s.client.Do(req)
	if err != nil {
		return Delivery{Reason: failureReason(err)}, nil
	}
	answer, readErr := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err := resp.Body.Close(); err != nil && readErr == nil {
		readErr = err
	}
	d := Delivery{Status: resp.StatusCode, Body: answer}
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300 && readErr == nil:
	case resp.StatusCode >= 500 || readErr != nil:
		d.Reason = reason5xx
	default:
		d.Reason = reason4xx
	}
	return d, nil
}

func failureReason(err error) string {
	var netErr net.Error
	var verify *tls.CertificateVerificationError
	var authority x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	var record tls.RecordHeaderError
	switch {
	case errors.As(err, &verify), errors.As(err, &authority), errors.As(err, &hostname),
		errors.As(err, &invalid), errors.As(err, &record):
		return reasonTLS
	case errors.As(err, &netErr) && netErr.Timeout():
		return reasonTimeout
	default:
		return reasonRefused
	}
}

// challenge asks the endpoint to echo a nonce, proving it wants the subscription's deliveries.
func (s *Sender) challenge(ctx context.Context, subscriptionID, target, secret string) (string, bool, error) {
	nonce, err := randomHex()
	if err != nil {
		return "", false, err
	}
	body, err := json.Marshal(map[string]string{"type": "verification", "challenge": nonce})
	if err != nil {
		return "", false, err
	}
	d, err := s.Post(ctx, subscriptionID, "msg_verification_"+nonce, target, []string{secret}, body)
	if err != nil {
		return "", false, err
	}
	if d.Reason != "" {
		return d.Reason, false, nil
	}
	var echo struct {
		Challenge string `json:"challenge"`
	}
	if err := json.Unmarshal(d.Body, &echo); err != nil || subtle.ConstantTimeCompare([]byte(echo.Challenge), []byte(nonce)) != 1 {
		return reasonChallenge, false, nil
	}
	return "", true, nil
}
