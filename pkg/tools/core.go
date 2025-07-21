package tools

import (
	"net/http"
	"net/url"
)

const (
	// URLTimeFormat is used to parse date time query parameters
	URLTimeFormat = "2006-01-02T15:04:05.000Z"
)

type QueryParamOption func(url.Values)

type Client interface {
	Do(req *http.Request) (*http.Response, error)
	Get(url string) (*http.Response, error)
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
	EdgeFleetType    FleetType = "Edge"
	CloudFleetType   FleetType = "Cloud"
	GatewayFleetType FleetType = "Gateway"
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
