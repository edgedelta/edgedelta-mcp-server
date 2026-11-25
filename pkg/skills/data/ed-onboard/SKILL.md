---
name: ed-onboard
description: Connect customer systems to Edge Delta across cloud providers, on-premises and hybrid environments. Discover telemetry sources, choose supported collection paths, configure pipelines and agents, and verify logs, metrics and traces. Use for first-time onboarding or adding sources.
metadata:
  version: "1.0.0"
  author: edgedelta
  repository: https://github.com/edgedelta/agent-skills
  tags: edgedelta,onboarding,telemetry,integrations
---

# Onboard telemetry to Edge Delta

Start from the customer's systems and requested signals, not a cloud provider or a
fixed list of services. This applies to hosts, containers, Kubernetes, applications,
databases, managed services and other telemetry-producing systems. A source does not
need its own recipe in this skill to be onboarded.

Use **ed-edx** for Edge Delta operations and the environment's existing tools, provider
CLI, APIs or infrastructure as code for deployment. Use **ed-pipelines** for pipeline
lifecycle and **ed-pipeline-tuning** for processing. Keep environment credentials and
context separate from the Edge Delta organization/profile.

## Collection principles

- Minimize hops and duplicate ingestion. Prefer native push directly to a compatible
  Edge Delta receiver, or an Edge Delta agent close to the source when collection or
  local processing is needed. For pull sources, let a supported Edge Delta source pull.
- Prefer Edge Delta agents over another collector. Add an intermediary or collector
  only for a verified requirement, explaining its operational and cost implications.
  Existing use of a provider's logging service does not make it the preferred path.
- Guide toward full coverage. Ask which signals to onboard as a multi-select choice
  whose first option is "All signals (Recommended)" (logs, metrics, traces and, for
  Kubernetes, events), followed by Logs, Metrics and Traces so customers who want fewer
  can combine them. Kubernetes events ride with All; say so in its description, which
  also keeps the choice within tools that allow only four options. Show signals a
  resource cannot provide as gaps in the target table.
- Decide per signal. Application/client telemetry does not establish host, service or
  database-server coverage. An agent does not manufacture application traces or gain
  access to a managed service's private filesystem.
- A missing direct path is a gap to explain, not a reason to silently omit a requested
  signal. Propose the shortest supported alternative and honor existing authorization
  for exceptions. Do not promise an integration based only on a similar product name.
- Verify the real source path. Do not fetch infrastructure data in an ad-hoc script and
  resubmit it as proof that an Edge Delta source supports the integration. Application
  instrumentation may emit its own measured activity.
- Preserve existing collection until its replacement is verified. Retire duplicate
  paths only within scope and honor explicit retention instructions.

## Conversational execution flow

Use these stages to guide the work, not as nine mandatory approval gates. Reuse scope,
selections and authorization already supplied; ask only for missing decisions. A request
that already names the environment, resources and permission to deploy can proceed
without repeating those questions. Revisit the relevant stage if discovery changes the
available paths. No new edx command hierarchy or backend onboarding state is required.

1. **Identify environments.** Inspect available local tools and configured profile/context
   names, keeping credential values private. Examples include AWS CLI, gcloud, Azure CLI,
   kubectl, Vercel CLI and DigitalOcean doctl; this is not an exhaustive provider list.
   Use available connectors, APIs or existing environment information too. An installed
   tool is only a clue, not proof of authentication, available resources or authorization.
   Check the Edge Delta side with `edx auth status`. If no profile authenticates, ask
   (in the first selection question round) whether the customer is new to Edge Delta
   or already has an account. Then tell them plainly, in your reply rather than inside
   another question, to run the matching interactive command themselves: `edx signup`
   for new customers, `edx auth login` for existing ones (`--env staging` for
   staging), in a terminal on the same machine or through the assistant's own way of
   running a user command, if it has one. Read-only discovery may continue meanwhile,
   but re-check `edx auth status` and use the new profile as the destination before
   planning. Never create an account or log in on the customer's behalf.
2. **Choose discovery scope.** Present the identified environments and let the customer
   select some or all, with account/project/subscription, region, cluster, hosts or
   datacenter boundaries as applicable. Ask which signals to onboard alongside scope,
   with "All signals (Recommended)" first. If scope is already clear, use it. Read
   [discovery and selection](references/discovery.md) for inventory and selection details.
3. **Discover read-only.** Within that scope, inventory resources, existing collection,
   agents, pipelines and deployment ownership. Identify available signals and candidate
   integrations. Do not install agents, enable exports or change logging during discovery.
   Record inaccessible areas separately from empty ones; do not create demo resources
   merely because nothing was found.
4. **Select targets.** Present concrete resources, signals, current coverage, proposed
   collection paths, support confidence and material changes/costs. Let the customer
   choose all or selected resources/signals, or apply an explicit constraint such as
   direct paths only. Preserve exclusions. Discovery access does not authorize onboarding
   everything; resource selection does not silently authorize unresolved exceptions.
5. **Plan the selected changes.** Choose by [source capabilities](references/collection-paths.md),
   checking the actual runtime's supported inputs, protocols, authentication and formats.
   Describe pipeline/agent changes, permissions, network needs, ownership, rollout,
   recurring costs, unsupported signals and rollback. Scale detail to the task; a single
   source can use a short note. [Cloud hints](references/cloud-hints.md) are optional.
6. **Resolve exceptions and authorization.** Settle material choices such as a required
   paid intermediary or a change outside the original scope before applying it. Present
   concrete changes when authorization is missing. Existing deployment authorization is
   sufficient for covered actions; do not ask again merely because this stage exists.
7. **Execute.** Confirm the target environment and Edge Delta organization, then apply
   selected changes through their existing ownership tools. Reuse appropriate resources,
   reconcile uncertain creates before retrying, and record created IDs immediately,
   including partial failures. Deliver credentials through the customer's secret mechanism.
8. **Verify.** Use [verification](references/verification.md) to check source receipt,
   correct processing and indexed logs/metrics/traces for each selected resource/signal.
   Distinguish deployment health from telemetry coverage. Report partial, blocked,
   unsupported and unverified outcomes; do not silently expand targets to obtain a pass.
9. **Handoff.** Summarize connected resources, pipeline IDs, chosen paths, query evidence,
   remaining gaps and operational instructions. Keep a proportionate local
   [inventory](references/handoff.md) when work spans sessions or creates resources.

Retry, rollback and cleanup are conditional actions, not mandatory final stages. When
asked to retain a test, keep agents and workloads running; do not stop them to force a
verification flush. When cleanup is authorized, remove only owned resources in dependency
order and verify residuals. Deleting a pipeline does not uninstall agents or remove
external infrastructure. A verification failure alone does not override retention.

## Pipeline authoring

Follow **ed-pipelines** for directly attached multiprocessors: each application source
and destination gets a `type: sequence` named `<node-name>_multiprocessor`, even when
empty. Standalone intermediate processors do not replace these attachments. Preserve
default direct self-telemetry/internal-statistics routing.

Give nested processors meaningful display names through JSON-encoded `metadata.name`,
including Custom OTTL. Preserve existing metadata and processing semantics.

Validate configurations and test representative inputs and expected outputs before
promoting transformations. Check signal identity, value/unit, original timestamp,
filter exclusions and unintended extra outputs. Use existing edx preview/live-capture
capabilities; this skill does not require a bundled test runner or deployment script.
