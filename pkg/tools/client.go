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
	newHTTPClientFunc = func(apiTokenHeader string) *http.Client {
		t := &authedTransport{
			apiTokenHeader: apiTokenHeader,
			Transport: http.Transport{
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
			},
		}

		return &http.Client{Transport: t}
	}
)

type authedTransport struct {
	http.Transport
	apiTokenHeader string
}

func (t *authedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.apiTokenHeader == "" {
		return t.Transport.RoundTrip(req)
	}

	if token, ok := tokenKeyFromContext(req.Context()); ok {
		req.Header.Set(t.apiTokenHeader, token)
	}
	return t.Transport.RoundTrip(req)
}

type HTTPClient struct {
	cl             *http.Client
	apiTokenHeader string
	apiURL         string
}

func NewHTTPClient(apiURL, apiTokenHeader string) *HTTPClient {
	return &HTTPClient{
		cl:             newHTTPClientFunc(apiTokenHeader),
		apiURL:         apiURL,
		apiTokenHeader: apiTokenHeader,
	}
}

func (c *HTTPClient) Do(req *http.Request) (*http.Response, error) {
	return c.cl.Do(req)
}

func (c *HTTPClient) Get(url string) (*http.Response, error) {
	return c.cl.Get(url)
}

func (c *HTTPClient) APIURL() string {
	return c.apiURL
}

func GetPipelines(ctx context.Context, client Client, lookbackDays int, opts ...QueryParamOption) ([]PipelineSummary, error) {
	orgID, token, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	pipelineURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/pipelines", client.APIURL(), orgID))
	if err != nil {
		return nil, err
	}

	req, err := createRequest(ctx, pipelineURL, token)
	if err != nil {
		return nil, fmt.Errorf("failed to create pipelines request, err: %v", err)
	}
	resp, err := client.Do(req)
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

		// filter out not updated in last lookbackDays days
		if pipeline.Updated < time.Now().UTC().AddDate(0, 0, -lookbackDays).Format(URLTimeFormat) {
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

func SavePipeline(ctx context.Context, client Client, confID, description, pipeline, content string) (map[string]interface{}, error) {
	orgID, token, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	saveURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/pipelines/%s/save", client.APIURL(), orgID, confID))
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

	resp, err := client.Do(req)
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

func GetFacets(ctx context.Context, client Client, opts ...QueryParamOption) ([]Facet, error) {
	orgID, token, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	facetsURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/facets", client.APIURL(), orgID))
	if err != nil {
		return nil, err
	}

	req, err := createRequest(ctx, facetsURL, token, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create facets request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch facets, status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var response FacetsResponse
	if err := json.NewDecoder(resp.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to decode facets response: %v", err)
	}

	facets := make([]Facet, 0, len(response.Builtin)+len(response.UserDefined))
	facets = append(facets, response.Builtin...)
	facets = append(facets, response.UserDefined...)

	return facets, nil
}

func GetFacetOptions(ctx context.Context, client Client, opts ...QueryParamOption) (*Facet, error) {
	orgID, token, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	facetURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/facet_options", client.APIURL(), orgID))
	if err != nil {
		return nil, err
	}

	req, err := createRequest(ctx, facetURL, token, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to create facet options request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to fetch facet options, status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var facet Facet
	if err := json.NewDecoder(resp.Body).Decode(&facet); err != nil {
		return nil, fmt.Errorf("failed to decode facet options response: %v", err)
	}

	return &facet, nil
}

func tokenKeyFromContext(ctx context.Context) (string, bool) {
	value := ctx.Value(TokenKey)
	if value == nil {
		return "", false
	}

	token, ok := value.(string)
	if !ok {
		return "", false
	}

	return token, true
}

func createRequest(ctx context.Context, reqUrl *url.URL, token string, opts ...QueryParamOption) (*http.Request, error) {
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
