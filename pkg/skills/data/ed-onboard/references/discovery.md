# Discovery scope and target selection

## Find environment candidates

Use the user's context first. When the environment is unknown, inspect installed CLIs
and nonsecret configuration metadata to offer choices. `command -v` can check tools
such as aws, gcloud, az, kubectl, vercel and doctl without invoking cloud discovery.
Look for configured profile/project/subscription/context names through the tool's safe
metadata commands; do not dump credential files, tokens or unrelated environment values.
Tools, providers and resource types not listed here are equally eligible.

Do not automatically install a CLI, initiate login, switch the customer's default
account/context or scan every accessible environment. A missing CLI is not evidence
that a provider is absent: use authorized connectors, APIs or existing IaC information,
or ask which environment the customer wants to connect.

Offer a concise scope choice, for example: "I found two AWS profiles, one Google Cloud
configuration and three Kubernetes contexts. Which should I inspect?" Use nonsecret
labels/identifiers so the customer can distinguish them. Include regions and environment
boundaries when relevant. An explicit request to inspect all of the presented scope is
sufficient; do not ask about each environment again. Existing scope takes precedence.

## Inventory without changing collection

Resolve actual account/project/subscription/cluster identity within the chosen scope,
and bind each command to that context explicitly. Follow inventory pagination. Record
what was inspected, what was empty and what was inaccessible or incomplete; do not turn
partial discovery into a claim of complete coverage. Discover existing agents, telemetry
routes and IaC ownership alongside resources to avoid duplicate collection or drift.

For each useful candidate, identify logs, metrics and traces separately. Distinguish
application/client signals from host, service/server and control-plane signals. Determine
whether the required native input/export and runtime compatibility are confirmed, still
need investigation, or are unsupported. Discovering a resource does not prove that every
signal can be piped to Edge Delta. Do not enable exports or alter verbosity to investigate.

## Present choices and carry them forward

Use a compact table or grouped list with stable resource IDs and readable names:

| Resource and environment | Signals / existing coverage | Candidate path | Support status | Changes and cost implications |
|---|---|---|---|---|
| Identified resource | Requested/available signals; already collected signals | Supported push or Edge Delta pull/agent path | Confirmed, needs investigation, or unsupported | Agent/config changes and necessary intermediaries |

Default each row to all signals the resource can produce and recommend onboarding all
of them; offer "all" first. Let the customer choose all presented candidates, selected
IDs, signal opt-outs, or a clear constraint. Do not include resources found later in an
earlier "all" selection without checking whether they fall within the user's stated
scope. If only discovery was requested, stop with the inventory and candidates; no
installation or collection changes follow.

Carry selections, exclusions and unresolved choices into the plan. If an exception would
violate a selection constraint, explain the gap rather than silently relaxing it. Before
execution, recheck material assumptions and environment identity; seek a new decision
only if the changed plan exceeds existing authorization. Preserve the inventory locally
for resuming work, without inventing a required backend object or deployment framework.
