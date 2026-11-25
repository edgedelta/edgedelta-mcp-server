# Choose collection by source capability

Use these categories to investigate a system, not as an exhaustive integration list.
Check the actual runtime's supported sources and destinations before selecting a path;
provider names alone do not establish protocol, authentication or format compatibility.

| Source capability | Investigate and prefer | Verify before claiming coverage |
|---|---|---|
| Application emits telemetry | Native SDK/export to a compatible Edge Delta receiver; use a nearby agent when useful for collection/processing | Export protocol, authentication, signal types and measured application activity |
| Host exposes local files or system statistics | Edge Delta agent with supported local sources | OS/architecture, readable paths, rotation, permissions and host metrics |
| Containers or Kubernetes | Supported node collection, shared files or application export according to the platform | Pod/task isolation, mounts, discovery/include filters, RBAC and each execution mode |
| Managed database/service | Supported native export/push, or a maintained Edge Delta pull source | Available engine/service logs and infrastructure metrics separately from client telemetry |
| Object storage, queues or event streams | Matching native source with event/queue delivery where appropriate | Object format/compression, notifications, permissions, offsets and duplicate behavior |
| SaaS, appliances or custom systems | Discover supported export protocol, webhook or maintained source | Authentication, payload limits, retries, backpressure and supported signals |

For each candidate path, consider source access, network reachability, delivery semantics,
processing needs, recurring cost and the customer's operational ownership. Reuse existing
egress and keep receivers appropriately scoped. Avoid adding paid storage, queues or
another collector when a supported direct path meets the requirements. When an intermediary
is necessary, describe why and include its cost in the chosen path.

If no supported integration exists, state the precise capability gap and possible supported
alternatives. Do not turn a one-off polling/resubmission script into an undocumented collector.
Do not infer that application traces cover server logs, or that workload logs cover a
managed platform's control-plane logs. Ask for additional signal scope only when it matters.

For deployment, discover the applicable permissions, identity delivery, network access,
runtime/version compatibility and resource budget. Check rendered manifests and actual
source selection rather than assuming an installed agent will collect everything.
