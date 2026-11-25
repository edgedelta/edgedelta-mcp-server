---
name: ed-rehydrations
description: Rehydrations - replay archived data (S3, GCS, ...) back through a pipeline to a destination.
metadata:
  version: "1.0.0"
  author: edgedelta
  repository: https://github.com/edgedelta/agent-skills
  tags: edgedelta,rehydration,archives,replay
  alwaysApply: "false"
---

# Edge Delta Rehydrations

Rehydration replays data that was archived to object storage (S3, GCS, ...)
back through a pipeline to a destination — typically to backfill an index or
re-forward logs that were only archived. Requires `edx` >= 0.16.0.

## Prerequisites

The `edx` CLI must be installed and authenticated. See the **ed-edx** skill.

## Inspect Jobs

```bash
edx rehydrations list --output table --columns rehydration_id,status,percentage,from,to,destination
edx rehydrations list -q 'rehydration.status:"in-progress"' --lookback 24h
edx rehydrations get <rehydration-id>       # full job + progress percentage
```

Job lifecycle: `created` → `invoked` → `in-progress` → `completed` /
`failed` / `cancelled` / `marked-for-delete`. A `failed` job carries the
reason in its `error` field.

## Discover What Can Be Rehydrated

A CQL filter plus a time range resolves into the eligible archive-source /
destination job combinations ("available rehydrations" in the web UI):

```bash
edx rehydrations validate -q 'service.name:"api"' --lookback 1h
```

Each entry reports an `efficiency_level` (`fast`/`moderate`/`slow`): filters
that match the archive's path-prefix structure scan far less data. An entry
with `filter_error_message` cannot be created as a job.

## Estimate Volume Before Creating

Estimate replay volume the way the web UI's Analysis panel does — from the
rehydration metrics:

```bash
edx metrics query --name ed.rehydration.bytes --agg sum --filter '<same cql>' --lookback 1h
edx metrics query --name ed.rehydration.count --agg sum --filter '<same cql>' --lookback 1h
```

`edx rehydrations analyze` calls the server-side estimator instead, but that
endpoint only accepts archives backed by a **legacy archive integration**
(`--source <integration>`); for pipeline archives (`archive_source` entries
from `validate`) it returns 400 "Given integration is not found" — use the
metric queries above.

## Create

Creation mirrors the web UI: validate the filter and time range, review the
eligible jobs, then submit them as one batch:

```bash
# Preview only - shows the eligible jobs, creates nothing
edx rehydrations create -q 'service.name:"api"' --lookback 1h --dry-run

# Create (prompts with the job list; --yes for non-interactive use)
edx rehydrations create -q 'service.name:"api"' \
  --from 2026-08-15T08:00:00.000Z --to 2026-08-15T09:00:00.000Z --yes
```

- `--exclude-overlap` (default true) skips data already rehydrated by
  previous jobs — keep it on to avoid duplicate delivery.
- `--source` / `--destination` narrow which eligible combos are created.
- Start with a small time window; a wide range over a busy archive is a
  large, costly job. Estimate first (see above).

## Cancel / Delete

```bash
edx rehydrations cancel <rehydration-id> --yes   # stop a running job
edx rehydrations delete <rehydration-id> --yes   # running jobs are marked-for-delete first
```

## Good Practices

- Filter as close to the archive's path structure as possible (`ed.tag`,
  time) — `slow` efficiency means full content scanning.
- Watch progress with `edx rehydrations get <id>` (`percentage`, `metrics`,
  `process_stats`); a stuck job's `error` field says why.
- Rehydrated data is billed like new ingestion — estimate volume before
  creating large jobs.
