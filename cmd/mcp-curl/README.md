# mcpcurl

A CLI tool that dynamically builds commands based on schemas retrieved from MCP servers that can
be executed against the configured MCP server.

## Overview

`mcpcurl` is a command-line interface that:

1. Connects to an MCP server via stdio
2. Dynamically retrieves the available tools schema
3. Generates CLI commands corresponding to each tool
4. Handles parameter validation based on the schema
5. Executes commands and displays responses

## Installation

## Usage

```bash
mcpcurl --stdio-server-cmd="<command to start MCP server>" <command> [flags]
```

The `--stdio-server-cmd` flag is required for all commands and specifies the command to run the MCP server.
Only the `docker` and `edgedelta-mcp-server` executables are supported, and the command string must not contain shell metacharacters such as `|`, `;`, or `&`.

### Available Commands

- `tools`: Contains all dynamically generated tool commands from the schema
- `schema`: Fetches and displays the raw schema from the MCP server
- `help`: Shows help for any command

### Examples

List available tools in MCP server:

```bash
% ./mcpcurl --stdio-server-cmd "docker run -i --rm -e ED_ORG_ID=<org_id> -e ED_API_TOKEN=<token> mcp/edgedelta" tools --help
Contains all dynamically generated tool commands from the schema

Usage:
  mcpcurl tools [command]

Available Commands:
  events_search Search for Edge Delta events
  log_search    Search for Edge Delta logs
  ...

Flags:
  -h, --help   help for tools

Global Flags:
      --pretty                    Pretty print MCP response (only for JSON responses) (default true)
      --stdio-server-cmd string   Command to invoke MCP server via stdio (docker or edgedelta-mcp-server) (required)

Use "mcpcurl tools [command] --help" for more information about a command.
```

Get help for a specific tool:

```bash
% ./mcpcurl --stdio-server-cmd "docker run -i --rm -e ED_ORG_ID=<org_id> -e ED_API_TOKEN=<token> mcp/edgedelta" tools log_search --help
Search for Edge Delta logs

Usage:
  mcpcurl tools log_search [flags]

Flags:
      --cursor string     Cursor provided from previous response, pass it to next request so that we can move the cursor with given limit. (optional)
      --from string       Start time in 2006-01-02T15:04:05.000Z format (optional)
      -h, --help              help for log_search
      --limit string      Limit number of results (optional)
      --lookback string   Lookback time in duration format (e.g. 60s, 15m, 1h, 1d, 1w) (optional)
      --order string      Sort order ('asc' or 'desc') (optional)
      --query string      Search query using Edge Delta log search syntax (optional)
      --to string         End time in 2006-01-02T15:04:05.000Z format (optional)

Global Flags:
      --pretty                    Pretty print MCP response (only for JSON responses) (default true)
      --stdio-server-cmd string   Command to invoke MCP server via stdio (docker or edgedelta-mcp-server) (required)
```

Use one of the tools:

```bash
 % ./mcpcurl --stdio-server-cmd "docker run -i --rm -e ED_ORG_ID=<org_id> -e ED_API_TOKEN=<token> mcp/edgedelta" tools log_search --lookback 15m --query "error"
{
  "items": [
    {
      "attributes": {},
      "body": "compactor: RPC stream closed, host: ip-10-0-244-233.us-west-2.compute.internal, err: rpc error: code = Canceled desc = context canceled",
      "id": "f1682495-d58d-4e2f-b952-e0f0e7fb25fd",
      "resource": {
        "ed.agent.build.version": "v1.35.0",
        "ed.agent.identifier": "edgedelta-confv3-compactor-2",
        "ed.agent.org.id": "2d6be233-f7bb-4fe1-90a5-28a95c86ec9c",
        "ed.agent.tag": "dogfood-confv3",
        "ed.agent.type": "",
        "ed.tag": "agent_self_log",
        "host.name": "edgedelta-confv3-compactor-2"
      },
      "severity_text": "error",
      "timestamp": 1744675526789
    },
    ...
  ]
}
```

## Dynamic Commands

All tools provided by the MCP server are automatically available as subcommands under the `tools` command. Each generated command has:

- Appropriate flags matching the tool's input schema
- Validation for required parameters
- Type validation
- Enum validation (for string parameters with allowable values)
- Help text generated from the tool's description

## How It Works

1. `mcpcurl` makes a JSON-RPC request to the server using the `tools/list` method
2. The server responds with a schema describing all available tools
3. `mcpcurl` dynamically builds a command structure based on this schema
4. When a command is executed, arguments are converted to a JSON-RPC request
5. The request is sent to the server via stdin, and the response is printed to stdout