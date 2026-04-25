package tools

import (
	"net/http"
	"net/url"
)

const (
	// StorageTimeFormat is used to filter out pipelines that are not updated in the last lookback days
	StorageTimeFormat = "2006-01-02 15:04:05.999999999 -0700 MST"
)

type QueryParamOption func(url.Values)

type Client interface {
	Do(req *http.Request) (*http.Response, error)
	Get(url string) (*http.Response, error)
	APIURL() string
}

// EnvironmentType represents the deployment environment
type EnvironmentType string

const (
	KubernetesEnvironmentType EnvironmentType = "Kubernetes"
	HelmEnvironmentType       EnvironmentType = "Helm"
	DockerEnvironmentType     EnvironmentType = "Docker"
	MacOSEnvironmentType      EnvironmentType = "MacOS"
	LinuxEnvironmentType      EnvironmentType = "Linux"
	WindowsEnvironmentType    EnvironmentType = "Windows"
)

// FleetSubtype represents the subtype of agent fleet
type FleetSubtype string

const (
	EdgeFleetSubtype        FleetSubtype = "Edge"
	CoordinatorFleetSubtype FleetSubtype = "Coordinator"
	GatewayFleetSubtype     FleetSubtype = "Gateway"
)

// FleetType represents the type of agent fleet
type FleetType string

const (
	EdgeFleetType              FleetType = "Edge"
	CloudFleetType             FleetType = "Cloud"
	GatewayFleetType           FleetType = "Gateway"
	IngestionPipelineFleetType FleetType = "IngestionPipeline"
)

// FleetStatus represents the status of a fleet
type FleetStatus string

const (
	FleetRunning   FleetStatus = "running"
	FleetSuspended FleetStatus = "suspended"
)

// PipelineSummary represents a pipeline summary
type PipelineSummary struct {
	ID          string          `json:"id"`
	Tag         string          `json:"tag"`
	ClusterName string          `json:"cluster_name,omitempty"`
	Creator     string          `json:"creator"`
	Created     string          `json:"created"`
	Updater     string          `json:"updater,omitempty"`
	Updated     string          `json:"updated,omitempty"`
	Environment EnvironmentType `json:"environment,omitempty"`
	FleetType   FleetType       `json:"fleet_type,omitempty"`
	Status      FleetStatus     `json:"status,omitempty"`
}

// IngestionEndpointsResponse mirrors the backend response from
// GET /v1/orgs/{org_id}/ingestion_endpoints
type IngestionEndpointsResponse struct {
	HTTPS *HTTPSIngestionEndpoints `json:"https,omitempty"`
}

type HTTPSIngestionEndpoints struct {
	BaseURL         string            `json:"base_url"`
	PathForDataType map[string]string `json:"path_for_data_type"`
	SampleData      map[string]string `json:"sample_data"`
	TestCommands    map[string]string `json:"test_commands"`
}

// IngestionTokenResponse mirrors the backend response from
// GET /v1/orgs/{org_id}/ingestion_token
type IngestionTokenResponse struct {
	RawToken string `json:"raw_token"`
	TokenID  string `json:"token_id"`
	OrgID    string `json:"org_id"`
	ConfID   string `json:"conf_id"`
	NodeName string `json:"node_name"`
}

// ConfSummary mirrors the fields of backend/core.Conf that this package uses.
// The backend endpoint returns more fields; we only decode what we need.
type ConfSummary struct {
	ID        string    `json:"id"`
	Tag       string    `json:"tag"`
	FleetType FleetType `json:"fleet_type"`
}
