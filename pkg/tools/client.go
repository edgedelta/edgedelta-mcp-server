package tools

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
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

type HTTPClient struct {
	cl *http.Client
}

func NewHTTPlient() *HTTPClient {
	return &HTTPClient{
		cl: newHTTPClientFunc(),
	}
}

func (c *HTTPClient) Do(req *http.Request) (*http.Response, error) {
	return c.cl.Do(req)
}

func (c *HTTPClient) Get(url string) (*http.Response, error) {
	return c.cl.Get(url)
}

func WithKeyword(keyword string) QueryParamOption {
	return func(v url.Values) {
		if keyword != "" {
			v.Add("keyword", keyword)
		}
	}
}

func WithLimit(limit string) QueryParamOption {
	return func(v url.Values) {
		if limit != "" {
			v.Add("limit", limit)
		}
	}
}

func (c *HTTPClient) GetPipelines(ctx context.Context, opts ...QueryParamOption) ([]PipelineSummary, error) {
	apiURL, orgID, token, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	pipelineURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/pipelines", apiURL, orgID))
	if err != nil {
		return nil, err
	}

	req, err := c.createRequest(ctx, pipelineURL, token)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipelines request, err: %v", err)
	}
	resp, err := c.cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch payload from url: %s, status code %d", req.URL.RequestURI(), resp.StatusCode)
	}

	var pipelines []PipelineSummary
	if err := json.NewDecoder(resp.Body).Decode(&pipelines); err != nil {
		return nil, fmt.Errorf("failed to decode body into json for url: %s, err: %v", req.URL.RequestURI(), err)
	}

	// sort pipelines by updated date, recent updated first
	sort.Slice(pipelines, func(i, j int) bool {
		return pipelines[i].Updated > pipelines[j].Updated
	})

	queryValues := url.Values{}
	for _, opt := range opts {
		opt(queryValues)
	}
	keyword := queryValues.Get("keyword")
	limitStr := queryValues.Get("limit")
	limit, err := strconv.Atoi(limitStr)
	if err != nil {
		limit = 5
	} else if limit > 10 {
		limit = 10
	}

	forcedAdd := make(map[string]bool)
	for _, pipeline := range pipelines {
		if keyword != "" && strings.Contains(pipeline.Tag, keyword) {
			forcedAdd[pipeline.ID] = true
		}
	}

	returnPipelines := make([]PipelineSummary, 0)
	for _, pipeline := range pipelines {
		if forcedAdd[pipeline.ID] {
			returnPipelines = append(returnPipelines, PipelineSummary{
				ID:          pipeline.ID,
				Tag:         pipeline.Tag,
				ClusterName: pipeline.ClusterName,
				Creator:     pipeline.Creator,
				Created:     pipeline.Created,
				Updater:     pipeline.Updater,
				Updated:     pipeline.Updated,
				Environment: pipeline.Environment,
				FleetType:   pipeline.FleetType,
				Status:      pipeline.Status,
			})
			continue
		}

		if keyword != "" && !strings.Contains(pipeline.Tag, keyword) {
			continue
		}

		if pipeline.Status == FleetSuspended || pipeline.Updated == "" {
			continue
		}

		// filter out not updated in last 7 days
		if pipeline.Updated < time.Now().UTC().AddDate(0, 0, -7).Format(URLTimeFormat) {
			continue
		}

		returnPipelines = append(returnPipelines, PipelineSummary{
			ID:          pipeline.ID,
			Tag:         pipeline.Tag,
			ClusterName: pipeline.ClusterName,
			Creator:     pipeline.Creator,
			Created:     pipeline.Created,
			Updater:     pipeline.Updater,
			Updated:     pipeline.Updated,
			Environment: pipeline.Environment,
			FleetType:   pipeline.FleetType,
			Status:      pipeline.Status,
		})
	}

	// limit the number of pipelines to return
	if len(returnPipelines) > limit {
		return returnPipelines[:limit], nil
	}

	return returnPipelines, nil
}

func (c *HTTPClient) createRequest(ctx context.Context, reqUrl *url.URL, token string, opts ...QueryParamOption) (*http.Request, error) {
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

func (c *HTTPClient) SavePipeline(ctx context.Context, confID, description, pipeline, content string) (map[string]interface{}, error) {
	apiURL, orgID, token, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	saveURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/pipelines/%s/save", apiURL, orgID, confID))
	if err != nil {
		return nil, err
	}

	// Prepare request payload
	payload := map[string]any{
		"description": description,
	}

	if content != "" {
		payload["content"] = content
	} else if pipeline != "" {
		// Parse pipeline JSON string
		var pipelineObj map[string]any
		if err := json.Unmarshal([]byte(pipeline), &pipelineObj); err != nil {
			return nil, fmt.Errorf("failed to parse pipeline JSON, err: %v", err)
		}
		payload["pipeline"] = pipelineObj
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payload: %v", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, saveURL.String(), strings.NewReader(string(payloadBytes)))
	if err != nil {
		return nil, fmt.Errorf("failed to create save pipeline request: %v", err)
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("X-ED-API-Token", token)

	resp, err := c.cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to save pipeline, status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode save pipeline response: %v", err)
	}

	return result, nil
}
