package http

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/edgedelta/edgedelta-mcp-server/pkg/core"
)

var (
	newHTTPClientFunc = func() *http.Client {
		t := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
				DualStack: true,
			}).DialContext,
			// MaxIdleConnsPerHost does not work as expected
			// https://github.com/golang/go/issues/13801
			// https://github.com/OJ/gobuster/issues/127
			// Improve connection re-use
			MaxIdleConns: 256,
			// Observed rare 1 in 100k connection reset by peer error with high number MaxIdleConnsPerHost
			// Most likely due to concurrent connection limit from server side per host
			// https://edgedelta.atlassian.net/browse/ED-663
			MaxIdleConnsPerHost:   128,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		}
		return &http.Client{Transport: t}
	}
)

type HTTPClient struct {
	cl *http.Client
}

func NewClient() *HTTPClient {
	return &HTTPClient{
		cl: newHTTPClientFunc(),
	}
}

func (c *HTTPClient) createRequest(ctx context.Context, reqUrl *url.URL, token string, opts ...core.QueryParamOption) (*http.Request, error) {
	queryValues := url.Values{}
	for _, opt := range opts {
		opt(queryValues)
	}

	reqUrl.RawQuery = queryValues.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqUrl.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("X-ED-API-Token", token)
	return req, nil
}

func (c *HTTPClient) GetLogs(ctx context.Context, opts ...core.QueryParamOption) (*core.LogSearchResponse, error) {
	apiURL, orgID, token, err := core.FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	url, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/logs/log_search/search", apiURL, orgID))
	if err != nil {
		return nil, err
	}

	req, err := c.createRequest(ctx, url, token, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create log_search search query, err: %v", err)
	}
	resp, err := c.cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch payload from url: %s, status code %d", req.URL.RequestURI(), resp.StatusCode)
	}

	records := core.LogSearchResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		return nil, fmt.Errorf("failed to decode body into json for url: %s, err: %v", req.URL.RequestURI(), err)
	}
	return &records, nil
}

func (c *HTTPClient) GetEvents(ctx context.Context, opts ...core.QueryParamOption) (*core.EventResponse, error) {
	apiURL, orgID, token, err := core.FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	url, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/events/search", apiURL, orgID))
	if err != nil {
		return nil, err
	}

	req, err := c.createRequest(ctx, url, token, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create events search query, err: %v", err)
	}
	resp, err := c.cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch payload from url: %s, status code %d", req.URL.RequestURI(), resp.StatusCode)
	}

	records := core.EventResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		return nil, fmt.Errorf("failed to decode body into json for url: %s, err: %v", req.URL.RequestURI(), err)
	}
	return &records, nil
}

func (c *HTTPClient) GetPatternStats(ctx context.Context, opts ...core.QueryParamOption) (*core.PatternStatsResponse, error) {
	apiURL, orgID, token, err := core.FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}
	url, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/clustering/stats", apiURL, orgID))
	if err != nil {
		return nil, err
	}

	req, err := c.createRequest(ctx, url, token, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create pattern stats query, err: %v", err)
	}
	resp, err := c.cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch payload from url: %s, status code %d", req.URL.RequestURI(), resp.StatusCode)
	}

	records := core.PatternStatsResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		return nil, fmt.Errorf("failed to decode body into json for url: %s, err: %v", req.URL.RequestURI(), err)
	}
	return &records, nil
}
