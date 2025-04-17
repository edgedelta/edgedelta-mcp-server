package core

import (
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"time"
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

type Client struct {
	orgID       string
	bearerToken string
	apiBaseURL  string
	cl          *http.Client
}

func NewClient(orgID string, apiBaseURL string, bearerToken string) *Client {
	return &Client{
		orgID:       orgID,
		bearerToken: bearerToken,
		apiBaseURL:  apiBaseURL,
		cl:          newHTTPClientFunc(),
	}
}

func (c *Client) createRequest(reqUrl *url.URL, opts ...QueryParamOption) (*http.Request, error) {
	queryValues := url.Values{}
	for _, opt := range opts {
		opt(queryValues)
	}

	reqUrl.RawQuery = queryValues.Encode()

	req, err := http.NewRequest(http.MethodGet, reqUrl.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("X-ED-API-Token", c.bearerToken)
	return req, nil
}

func (c *Client) GetLogs(opts ...QueryParamOption) (*LogSearchResponse, error) {
	url, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/logs/log_search/search", c.apiBaseURL, c.orgID))
	if err != nil {
		return nil, err
	}

	req, err := c.createRequest(url, opts...)
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

	records := LogSearchResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		return nil, fmt.Errorf("failed to decode body into json for url: %s, err: %v", req.URL.RequestURI(), err)
	}
	return &records, nil
}

func (c *Client) GetEvents(opts ...QueryParamOption) (*EventResponse, error) {
	url, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/events/search", c.apiBaseURL, c.orgID))
	if err != nil {
		return nil, err
	}

	req, err := c.createRequest(url, opts...)
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

	records := EventResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		return nil, fmt.Errorf("failed to decode body into json for url: %s, err: %v", req.URL.RequestURI(), err)
	}
	return &records, nil
}

func (c *Client) GetPatternStats(opts ...QueryParamOption) (*PatternStatsResponse, error) {
	url, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/clustering/stats", c.apiBaseURL, c.orgID))
	if err != nil {
		return nil, err
	}

	req, err := c.createRequest(url, opts...)
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

	records := PatternStatsResponse{}
	if err := json.NewDecoder(resp.Body).Decode(&records); err != nil {
		return nil, fmt.Errorf("failed to decode body into json for url: %s, err: %v", req.URL.RequestURI(), err)
	}
	return &records, nil
}
