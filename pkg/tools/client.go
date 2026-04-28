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
	ctx := req.Context()
	if oauthToken, _ := ctx.Value(BearerTokenKey).(string); oauthToken != "" {
		req.Header.Set("Authorization", "Bearer "+oauthToken)
	} else if edToken, _ := ctx.Value(EDTokenKey).(string); edToken != "" {
		req.Header.Set("X-ED-API-Token", edToken)
	}

	return t.Transport.RoundTrip(req)
}

// applyAuthHeader sets the appropriate auth header on req. OAuth token takes precedence over ED token.
func applyAuthHeader(req *http.Request, keys *ContextKeys) {
	if keys.BearerToken != "" {
		req.Header.Set("Authorization", "Bearer "+keys.BearerToken)
	} else if keys.EDToken != "" {
		req.Header.Set("X-ED-API-Token", keys.EDToken)
	}
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

func GetPipelines(ctx context.Context, client Client, opts ...QueryParamOption) ([]PipelineSummary, error) {
	keys, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	pipelineURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/pipelines", client.APIURL(), keys.OrgID))
	if err != nil {
		return nil, err
	}

	req, err := createRequest(ctx, pipelineURL, keys)
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
		limit = 100
	} else if limit > 100 {
		limit = 100
	}

	offsetStr := queryValues.Get("offset")
	offset, err := strconv.Atoi(offsetStr)
	if err != nil {
		offset = 0
	} else if offset < 0 {
		offset = 0
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

	if offset > 0 {
		if offset >= len(returnPipelines) {
			return []PipelineSummary{}, nil
		}

		returnPipelines = returnPipelines[offset:]
	}

	// limit the number of pipelines to return
	if len(returnPipelines) > limit {
		return returnPipelines[:limit], nil
	}

	return returnPipelines, nil
}

func SavePipeline(ctx context.Context, client Client, confID, description, pipeline, content string) (map[string]any, error) {
	keys, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	saveURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/pipelines/%s/save", client.APIURL(), keys.OrgID, confID))
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
	applyAuthHeader(req, keys)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to save pipeline, status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("failed to decode save pipeline response: %v", err)
	}

	return result, nil
}

func GetFacets(ctx context.Context, client Client, opts ...QueryParamOption) ([]Facet, error) {
	keys, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	facetsURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/facets", client.APIURL(), keys.OrgID))
	if err != nil {
		return nil, err
	}

	req, err := createRequest(ctx, facetsURL, keys, opts...)
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
	keys, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	facetURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/facet_options", client.APIURL(), keys.OrgID))
	if err != nil {
		return nil, err
	}

	req, err := createRequest(ctx, facetURL, keys, opts...)
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

func createRequest(ctx context.Context, reqUrl *url.URL, keys *ContextKeys, opts ...QueryParamOption) (*http.Request, error) {
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
	applyAuthHeader(req, keys)
	return req, nil
}

func ListConfs(ctx context.Context, client Client) ([]*ConfSummary, error) {
	keys, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	confsURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/confs", client.APIURL(), keys.OrgID))
	if err != nil {
		return nil, err
	}

	req, err := createRequest(ctx, confsURL, keys, func(v url.Values) {
		v.Set("empty_contents", "true")
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create confs request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to list confs, status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var out []*ConfSummary
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to decode confs response: %v", err)
	}
	return out, nil
}

func GetIngestionEndpoints(ctx context.Context, client Client) (*IngestionEndpointsResponse, error) {
	keys, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	endpointsURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/ingestion_endpoints", client.APIURL(), keys.OrgID))
	if err != nil {
		return nil, err
	}

	req, err := createRequest(ctx, endpointsURL, keys)
	if err != nil {
		return nil, fmt.Errorf("failed to create ingestion_endpoints request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get ingestion endpoints, status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var out IngestionEndpointsResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to decode ingestion_endpoints response: %v", err)
	}
	return &out, nil
}

func GetIngestionToken(ctx context.Context, client Client, confID, nodeName string) (*IngestionTokenResponse, error) {
	keys, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	tokenURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/ingestion_token", client.APIURL(), keys.OrgID))
	if err != nil {
		return nil, err
	}

	req, err := createRequest(ctx, tokenURL, keys, func(v url.Values) {
		v.Set("conf_id", confID)
		v.Set("node_name", nodeName)
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ingestion_token request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get ingestion token, status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var out IngestionTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to decode ingestion_token response: %v", err)
	}
	return &out, nil
}

func GetConf(ctx context.Context, client Client, confID string) (*ConfSummary, error) {
	keys, err := FetchContextKeys(ctx)
	if err != nil {
		return nil, err
	}

	confURL, err := url.Parse(fmt.Sprintf("%s/v1/orgs/%s/confs/%s", client.APIURL(), keys.OrgID, confID))
	if err != nil {
		return nil, err
	}

	req, err := createRequest(ctx, confURL, keys)
	if err != nil {
		return nil, fmt.Errorf("failed to create get-conf request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("failed to get conf %s, status code %d: %s", confID, resp.StatusCode, string(bodyBytes))
	}

	var out ConfSummary
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("failed to decode conf response: %v", err)
	}
	return &out, nil
}
