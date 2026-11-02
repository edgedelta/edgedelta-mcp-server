# Edge Delta MCP Server

The **Edge Delta MCP Server** is a [Model Context Protocol (MCP)](https://modelcontextprotocol.io/introduction)
server that provides seamless integration with Edge Delta APIs, enabling advanced
automation and interaction capabilities for developers and tools.

## Use Cases

- Extract and analyse observability data from Edge Delta.
- Build AI‑powered tools and applications that interact with Edge Delta’s platform.


## Pipeline Configuration Tools

The server exposes tools to inspect and change Edge Delta pipelines. Write
operations follow a safe loop: **validate → save → deploy**, with rollback.

| Tool | What it does |
|------|--------------|
| `get_pipelines` / `get_pipeline_config` / `get_pipeline_history` | Discover pipelines and read their config and version history. |
| `validate_pipeline_config` | Validate a config YAML without saving it. Returns `{valid, reason}`. |
| `save_pipeline_config` | Save the full config YAML as a new version (does **not** deploy). |
| `deploy_pipeline` | Deploy a saved version to the fleet. |
| `revert_pipeline` | Roll back to a previously saved version. |
| `add_pipeline_source` | Add a data source node to a pipeline. |

Typical edit flow: `get_pipeline_config` → edit YAML → `validate_pipeline_config`
→ `save_pipeline_config` → `get_pipeline_history` → `deploy_pipeline`
(`revert_pipeline` to roll back).

### Live-capture tools

Validating that a config is *valid* is not the same as confirming a change
actually *transformed the data*. The live-capture tools sample real
before/after data flowing through pipeline nodes on running agents, so an agent
can verify a transformation took effect.

| Tool | What it does |
|------|--------------|
| `start_pipeline_capture` | Start a capture task on a pipeline (duration, optional node list, max items). |
| `get_pipeline_capture_status` | Poll per-agent status for a capture task. |
| `get_pipeline_capture_results` | Fetch captured before/after samples per node. |
| `get_pipeline_capture_task` | Show the currently active capture task, if any. |

Capture flow: `start_pipeline_capture` → `get_pipeline_capture_status` (poll until
an agent reports `success`) → `get_pipeline_capture_results` (compare each node's
`before` vs `after` to confirm the transformation).

### Processor dry-run (test) tools

Develop and verify a processor against sample logs **offline** — no save, no
deploy, no live traffic. The offline complement to live-capture: capture proves
what *did* happen, this proves what *will* happen.

| Tool | What it does |
|------|--------------|
| `test_pipeline_ottl` | Dry-run OTTL statements against sample OTEL log items; returns the transformed items. |
| `test_pipeline_node` | Dry-run a full processor node of any type (`log_to_metric`, `log_to_pattern`, `extract_metric`, `parse_json_attributes`, `grok`, `mask`, …); returns the items it emits per output path. |

Iterate until the output is right, then add the statements/node to the pipeline,
`save_pipeline_config` → `deploy_pipeline`, and confirm on live data with the
capture tools. Use `test_pipeline_ottl` for OTTL transforms (its `ExtractPatterns`
also covers regex field extraction); use `test_pipeline_node` for non-OTTL node
types such as log-to-metric and log-to-pattern.

## Events

The server offers Edge Delta's monitor events through the MCP Events extension,
`io.modelcontextprotocol/events`
([SEP-3415](https://github.com/modelcontextprotocol/modelcontextprotocol/pull/3415), not merged yet),
so a client can start work when a monitor alerts, with no model or session in the loop.
`initialize` advertises the extension, and the server answers `events/list` and `events/poll`
over both HTTP and stdio. Events are polled; webhook delivery isn't offered yet, so
`events/subscribe` and the other methods answer `-32601`.

| Event | Fires when | Arguments |
|-------|------------|-----------|
| `monitor.alert` | a monitor changes state: `ALERT`, `WARN`, `RECOVERED` or `NO DATA` | `monitorId`, `monitor` (name), `type`, `states`, `service`, `query` (more CQL) |
| `pattern.anomaly` | a pattern anomaly monitor changes state, with the patterns behind it | the same, without `type` |

Each event's `eventId` is Edge Delta's `event.id`, and its `data` has the monitor's
`title`, a `url` to the event in the app, `monitorName`, `state`, `priority`, `service`,
`message` and more; `events/list` gives the full payload schema.

Polling follows the SEP: a `null` cursor starts from now, `maxEvents` (default 50, at most
100) batches with `hasMore`, `maxAgeMs` bounds replay (as does a seven-day ceiling), and
`truncated` says when that skipped an event. Event search indexes events a little after they
happen, so a cursor stays two minutes behind now and remembers what it delivered since; an
event indexed late still arrives, once. Poll every 30 seconds (`nextPollMs`).

```bash
curl -s https://api.edgedelta.com/v1/orgs/$ED_ORG_ID/mcp \
  -H "X-ED-API-Token: $ED_API_TOKEN" -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' -H 'Mcp-Name: monitor.alert' \
  -d '{"jsonrpc":"2.0","id":1,"method":"events/poll","params":{"name":"monitor.alert","arguments":{"states":["ALERT"]},"cursor":null}}'
```

Errors use the SEP's provisional codes: `-32023` NotFound (`data.kind: "event"`) for an
unknown type, `-32602` InvalidParams for arguments, cursors or CQL the search refuses,
`-32024` Forbidden when the token can't search events, and `-32020` HeaderMismatch with HTTP 400
when an `Mcp-Name` header doesn't name the request's event. The header isn't required yet.

To serve events over HTTP, mount `MCPHTTPServer.Handler()`: `HTTPServer()` is mcp-go's own
server, which knows nothing of them.

## Prerequisites

1. **Docker Engine ≥ 20.10** installed *and running*.
2. **Docker Buildx plug‑in** available:
   - **macOS / Windows** – included with Docker Desktop.
   - **Debian / Ubuntu**
     ```bash
     sudo apt-get update && sudo apt-get install -y docker-buildx-plugin
     ```
   - **Fedora / RHEL / CentOS**
     ```bash
     sudo dnf install -y docker-buildx-plugin   # or yum install …
     ```
   - **Other distros (manual fallback)**
     ```bash
     mkdir -p ~/.docker/cli-plugins
     curl -sSL \
       https://github.com/docker/buildx/releases/latest/download/buildx-$(uname -s | tr '[:upper:]' '[:lower:]')-amd64 \
       -o ~/.docker/cli-plugins/docker-buildx
     chmod +x ~/.docker/cli-plugins/docker-buildx
     ```
3. An **Edge Delta API token** with the required scope – [create one here](https://docs.edgedelta.com/api-tokens/).
4. Your **Edge Delta organisation ID** – [find it here](https://docs.edgedelta.com/my-organization/).

## Build (container image)

First‑time setup (creates a multi‑platform builder and boots it):

```bash
docker buildx create --name edgedelta-builder --use
docker buildx inspect --bootstrap
```

Build the image and load it into the local Docker daemon:

```bash
docker buildx build --load -t mcp/edgedelta .
```

> ℹ️  The `--load` flag streams the image back to your local Docker engine so you can
> run it directly with `docker run mcp/edgedelta …`.

## Installation

### Usage with Cursor

```json
{
  "mcpServers": {
    "edgedelta": {
      "command": "docker",
      "args": [
        "run",
        "-i",
        "--rm",
        "-e ED_ORG_ID",
        "-e ED_API_TOKEN",
        "ghcr.io/edgedelta/edgedelta-mcp-server:latest"
      ],
      "env": {
        "ED_API_TOKEN": "<YOUR_TOKEN>",
        "ED_ORG_ID": "<YOUR_ORG_ID>"
      }
    }
  }
}
```

## Library Usage

The exported Go API of this module is **experimental** and may change without notice.
If you rely on it in production, please open an issue describing your use case so we
can stabilise the relevant surface.

## License

Licensed under the terms of the **MIT** licence. See [LICENSE](./LICENSE) for full details.

