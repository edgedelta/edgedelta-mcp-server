---
name: ed-pipeline-tuning
description: Tune pipeline data quality - parse, structure, enrich, mask and roll logs up to metrics/patterns, with offline dry-run and live-capture verification.
metadata:
  version: "1.0.0"
  author: edgedelta
  repository: https://github.com/edgedelta/agent-skills
  tags: edgedelta,pipelines,data-quality,ottl,parsing,enrichment,masking,log-to-metric
  globs: "**/edgedelta*.yaml,**/pipeline*.yaml"
  alwaysApply: "false"
---

# Edge Delta Pipeline Tuning (Data Quality)

Improve the *content* of telemetry: parse unstructured logs into fields, set
severity and timestamps, mask PII, drop noise, and roll logs up into metrics and
patterns. **ed-pipelines** covers how to ship a change (save/deploy/rollback);
this skill covers *what* to change and how to express it, and how to prove it
worked.

## Prerequisites

`edx` installed and authenticated (see **ed-edx**). Requires edx >= 0.8.0 for
`edx pipelines test`.

## The golden rule

**A valid config is not a working transform.** `validate` only checks that the
YAML is well-formed; it does NOT prove your rule changed the data. Always:

1. **`edx pipelines test`** - dry-run the rule on sample logs (offline, no deploy)
2. **`edx capture`** - after deploy, confirm the transform on live data

Skipping these is the #1 cause of "I deployed it but nothing changed" (e.g. an
OTTL statement that references the wrong field passes validation and silently
no-ops).

## The tuning loop

```bash
edx pipelines get <pipeline-id> | jq -r .content > pipeline.yaml   # 1. current config
edx pipelines test ottl <pipeline-id> -f samples.jsonl -s '<ottl>' # 2. dry-run offline
# 3. edit pipeline.yaml to add the processor
edx pipelines validate -f pipeline.yaml                            # 4. validate
edx pipelines save <pipeline-id> -f pipeline.yaml -d "why"         # 5. save version
edx pipelines deploy <pipeline-id> <version> --yes                 # 6. deploy
edx capture start <pipeline-id> --duration 2m && edx capture results <pipeline-id>  # 7. verify live
```

`samples.jsonl` is one JSON-encoded OTEL log per line, e.g.
`{"body":"...","attributes":{},"resource":{},"timestamp":1}`.

To watch step 7 as it happens instead of re-running `results`, tail it - one
line of decoded JSON per newly captured item (see **ed-pipelines** for the
caveats):

```bash
edx capture start <pipeline-id> --duration 10m --nodes <node>
edx capture results <pipeline-id> --follow --param nodes=<node>
```

## Pipeline structure and display names

Follow **ed-pipelines** for attached multiprocessors: use `type: sequence` nodes named
exactly `<source>_multiprocessor` immediately after application sources and
`<destination>_multiprocessor` immediately before destinations, including empty
attachments. Standalone intermediate sequences do not replace them. Preserve direct
agent self-telemetry/internal-statistics routing and existing processing semantics.

Name every nested processor by its purpose, including Custom OTTL. Store its display
name in JSON-encoded `metadata`, for example
`metadata: '{"name":"Mask authorization header"}'`, preserving other metadata keys.
Do not leave the name blank or "Custom"; nested YAML `name`/`user_description` fields
do not control this label. Review names alongside transform correctness before saving.

## Processor toolbox

Processors live in a `sequence` multiprocessor node (`processors:` list) or, for
log->metric/pattern, as top-level nodes wired via `links`.

| Goal | Node / OTTL |
|------|-------------|
| Transform anything | `ottl_transform` (`statements:`) |
| Parse JSON body | `merge_maps(attributes, ParseJSON(body), "upsert")` |
| Parse key=value / logfmt | `merge_maps(attributes, ParseKeyValue(body), "upsert")` |
| Extract fields by regex | `merge_maps(attributes, ExtractPatterns(body, "(?P<f>...)"), "upsert")` |
| Parse a known format | `grok` node (`pattern: %{COMMONAPACHELOG}`) |
| Mask PII | `replace_pattern(body, "<regex>", "[REDACTED]")` or a `mask` node |
| Drop noise | `filter`, `delete_key(attributes, "k")` |
| Logs -> metric | `log_to_metric` node (`pattern`, `metric_name`, `dimension_groups`) |
| Logs -> patterns | `log_to_pattern` node (`num_of_clusters`, `field_path`) |

Dry-run non-OTTL nodes with `edx pipelines test node <id> -f samples.jsonl --node node.json`,
where node.json is `{"id":"...","type":"log_to_metric","configuration":{...}}`.

## Recipes (verified OTTL)

**Auto-detect format and parse** (guard each parser so it only runs on matching lines):
```
merge_maps(attributes, ParseJSON(body), "upsert") where IsMatch(body, "^\\s*[{]")
merge_maps(attributes, ExtractPatterns(body, "^(?P<client_ip>\\S+) \\S+ \\S+ \\[[^\\]]+\\] \"(?P<method>\\S+) (?P<path>\\S+) [^\"]+\" (?P<status>\\d+) (?P<bytes>\\d+)"), "upsert") where IsMatch(body, "HTTP/")
merge_maps(attributes, ParseKeyValue(body), "upsert") where not IsMatch(body, "^\\s*[{]") and IsMatch(body, "[a-zA-Z0-9_]+=")
```

**Severity - explicit level tag wins over free-text keywords** (a `<Warning>` whose
message contains the word "error" must stay WARN):
```
set(severity_text, ToUpperCase(attributes["level"])) where EDXCoalesce(attributes["level"], "") != ""
set(severity_text, "ERROR") where EDXCoalesce(severity_text, "") == "" and IsMatch(body, "(?i)<(error|fault|critical)>")
set(severity_text, "WARN")  where EDXCoalesce(severity_text, "") == "" and IsMatch(body, "(?i)<warning>")
set(severity_text, "ERROR") where EDXCoalesce(severity_text, "") == "" and IsMatch(body, "(?i)(\\berror\\b|\\bfatal\\b|\\bpanic\\b)")
set(severity_text, "WARN")  where EDXCoalesce(severity_text, "") == "" and IsMatch(body, "(?i)(\\bwarn\\b|\\bwarning\\b)")
set(severity_text, "INFO")  where EDXCoalesce(severity_text, "") == ""
```

**Timestamp from an embedded string** (the field is `timestamp`; extract to
seconds precision first if the format has sub-seconds):
```
set(timestamp, UnixMilli(Time(attributes["ts"], "%Y-%m-%dT%H:%M:%SZ"))) where EDXCoalesce(attributes["ts"], "") != ""
```

**Mask PII (IPv4) + drop empty/redundant fields:**
```
replace_pattern(body, "\\b(?:[0-9]{1,3}\\.){3}[0-9]{1,3}\\b", "[REDACTED_IP]")
delete_key(attributes, "pid") where EDXCoalesce(attributes["pid"], "") == ""
```

**Logs -> metric** (count errors, dimensioned) as a top-level node:
```yaml
- name: error_count
  type: log_to_metric
  pattern: (?i)(error|fatal|panic)
  interval: 1m0s
  metric_name: error_count
  dimension_groups:
    - field_dimensions: [ item["resource"]["service.name"] ]
      enabled_stats: [ count ]
```

**Patterns from structured (JSON / key=value) logs.** Clustering the raw body
fails: compact JSON has no spaces, so the whole line is one digit-bearing token
and masks to a single `*` pattern; `=` is not a token separator, so `k=v` pairs
fragment into many micro-patterns. Cluster on a projection of stable fields
instead, and keep the original log as the sample (agent >= v2.23.0 for
`sample_field_path` / `max_token_count`):
```yaml
- type: ottl_transform
  metadata: '{"name":"Project stable fields for patterns"}'
  data_types: [log]
  statements: |-
    merge_maps(attributes, ParseJSON(body), "upsert") where IsMatch(body, "^\\s*[{]")
    set(attributes["status_class"], "success") where attributes["status_code"] != nil and Int(attributes["status_code"]) < 400
    set(attributes["status_class"], "client_error") where attributes["status_code"] != nil and Int(attributes["status_code"]) >= 400 and Int(attributes["status_code"]) < 500
    set(attributes["status_class"], "server_error") where attributes["status_code"] != nil and Int(attributes["status_code"]) >= 500
    set(attributes["pattern_input"], Concat(["route", attributes["route"], "method", attributes["method"], "status", attributes["status_class"]], " "))
- type: log_to_pattern_metric
  metadata: '{"name":"Patterns from projection"}'
  keep_item: true
  data_types: [log]
  field_path: attributes["pattern_input"]
  sample_field_path: body
  similarity_threshold: 0.9
```
Path syntax depends on the node: `log_to_pattern_metric` inside a sequence takes
OTTL paths (`attributes["x"]`, `body`); the top-level `log_to_pattern` node takes
CEL paths (`item["attributes"]["x"]`, `item["body"]`). The agent accepts either
form: a wrong `field_path` emits no patterns (agent log: `resulted in type:
<nil>`), and a wrong `sample_field_path` falls back to the projected line.
Projection rules: numbers never anchor a pattern (any digit-bearing token is
masked), so map them to word classes; leave IDs (user_id, request_id) out;
keep field order and count fixed; prefer `key: value` over `key=value`; with N
tokens, lines differing in one token merge only if (N-1)/N >= the threshold.
Zero-config fallback: `set(attributes["pattern_input"], body)` then
`replace_pattern(attributes["pattern_input"], "[{}\",:]+", " ")`, and raise
`max_token_count` (10-200, default 50) when logs carry more than ~25 pairs.
Per-entity counts belong in extract/aggregate metrics or `group_by`, not patterns.

When the user asks to demo or reproduce this, generate both pieces: a small
script that writes compact JSON lines (~20 keys including route, method,
numeric status_code, and volatile IDs) to a file, and the pipeline nodes above
behind a `file_input` on that path. Dry-run the OTTL with `edx pipelines test
ottl`; pattern nodes cannot be dry-run (`test node` returns 400), so verify
after deploy with `edx patterns list -q 'service.name:"<svc>"' --lookback 15m`
once one `reporting_frequency` has passed. Full guide:
https://docs.edgedelta.com/structured-log-patterns/

Notes: guards matter - `ParseJSON` on a body that starts with `{` but is
malformed errors the statement (skipped at runtime, so the log passes through
unparsed). OTTL string literals need doubled backslashes (`\\b` in YAML = `\b`
regex). `EDXCoalesce(x, "")` safely treats a missing/nil field as empty.

## Where to put processors (pre vs post — avoid the middle)

A pipeline has two natural processing stages. Put each processor in the one that
matches its scope; a processor that sits in neither is a smell.

```
input ──▶ [PRE: per-source MP] ──▶ (rare middle) ──▶ [POST: pre-output MP] ──▶ output
```

- **PRE — the per-source multiprocessor** (`<source>_multiprocessor`, right after
  each input). Everything **specific to that source**, because each source has one
  known shape: parse/structure it, set severity and timestamp from *its* format,
  drop noise (`filter`), sample low-value logs, and branch to `log_to_metric` /
  `log_to_pattern`. Also clean up the fields *this parser* created (e.g.
  `delete_key(attributes, "log_ts")` right after using it to set `timestamp`).

- **POST — the destination-attached multiprocessor** (`<destination>_multiprocessor`,
  immediately before its output; destination-bound sources converge).
  Only things **common to every source**: destination tagging (e.g. everything
  bound for Splunk gets a `resource` tag), org-wide PII masking, a severity
  fallback. This is the "output node" stage.

- **Middle — anything between pre and post — should be rare.** If a step is
  source-specific it belongs in PRE; if it's universal it belongs in POST. A
  source-specific rule stranded in the shared stage (e.g. macOS `<Level>` severity
  parsing applied to every source) is the most common mistake — move it into the
  source's own multiprocessor.

Rule of thumb: *does this apply to one source or all of them?* One → PRE. All →
POST. Neither cleanly → reconsider the design.

## Troubleshooting

| Problem | Fix |
|---------|-----|
| Deployed but data unchanged | You skipped dry-run/capture. `edx pipelines test ottl` the statement; check it references the right field. |
| `test` rejects the statement | Read the error - usually an undefined OTTL function or a bad regex escape (`\\` in YAML). |
| Non-OTTL node (log_to_metric, ...) won't dry-run in `test ottl` | Use `edx pipelines test node` with the node as `{"id","type","configuration":{...}}`. |
| Severity misclassified | Set from the explicit level tag/field *before* free-text keyword matching. |
| Pattern node emits nothing; agent log says `resulted in type: <nil>` | `field_path` uses the wrong path syntax for the node type - see "Path syntax depends on the node" above. |
| Structured logs collapse into one `*` pattern | Cluster on a projection, not the raw body - see "Patterns from structured logs" above. |
| Metric/pattern shows nothing in capture | They emit on an interval; extend `--duration` past one `interval`/reporting window. |
