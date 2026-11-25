# Optional environment hints

These examples record useful constraints, not the scope of the onboarding skill.
Use them only when the customer's environment matches. For any other provider or service,
discover its capabilities and choose a path using [collection paths](collection-paths.md).
Never infer current Edge Delta support from a provider's export option alone.

## Kubernetes on any provider or on premises

Distinguish node-backed workloads from environments where node agents/DaemonSets cannot
run. Confirm workload file access, RBAC, source include rules and infrastructure metric
endpoints; a healthy DaemonSet does not establish coverage for every workload or control
plane. Direct application export or a sidecar may be appropriate where node access is absent.
The Go v2.24.0 sandbox runs required explicit Kubernetes/kubelet/cAdvisor include rules;
empty selections validated but collected nothing. Confirm behavior for the deployed version.

## AWS examples

- Fargate sidecars do not automatically see other containers' stdout. Shared application
  files or application OTLP can support direct collection without a log router. Native
  task metadata can supply infrastructure statistics; distinguish one container's metrics
  from whole-task coverage.
- RDS is managed: an agent cannot be installed on its host. Engine logs, native database
  metrics and instrumented client traces are separate signals. A supported CloudWatch
  exception may be necessary when a direct source is unavailable; propose it explicitly.
  The tested paths used the official Edge Delta Lambda Forwarder for engine logs and
  CloudWatch Metric Streams → Firehose → S3/SQS → native Edge Delta S3 source for metrics.
  Investigate current support before reusing those paths; do not default every database
  to this architecture. Standard RDS metrics already publish to CloudWatch, while export
  and transport add their own charges. Stream name filters include all dimension
  combinations; downstream instance filtering does not eliminate upstream stream cost.

## Other cloud and hybrid environments

For GCP, Azure or other providers, bind discovery to the selected project/subscription,
region and cluster; for on-premises systems use the relevant hosts/datacenters and access
contexts. Inspect native export options, workload identity and connectivity. Do not
mechanically translate an AWS recipe or route everything through the provider's logging
service. Apply the same direct-path preference and verify provider-specific formats and
permissions with current documentation and the actual Edge Delta source configuration.
