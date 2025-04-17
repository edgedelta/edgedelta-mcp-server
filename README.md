# Edge Delta MCP Server

The Edge Delta MCP Server is a [Model Context Protocol (MCP)](https://modelcontextprotocol.io/introduction)
server that provides seamless integration with Edge Delta APIs, enabling advanced
automation and interaction capabilities for developers and tools.

## Use Cases

- Extracting and analyzing data from Edge Delta.
- Building AI powered tools and applications that interact with Edge Delta's observability.

## Prerequisites

1. To run the server in a container, you will need to have [Docker](https://www.docker.com/) installed.
2. Once Docker is installed, you will also need to ensure Docker is running.
3. You will need to [Create a Edge Delta API Token](https://docs.edgedelta.com/api-tokens/). The MCP server can use many of the Edge Delta APIs, you will need to grant necessary access to API token.
4. You will need to [Fetch the organization id](https://docs.edgedelta.com/my-organization/)

## Build

Build mcp docker container.
```
docker build -t mcp/edgedelta -f Dockerfile .
```

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
        "-e",
        "ED_ORG_ID",
        "-e",
        "ED_API_TOKEN",
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

The exported Go API of this module should currently be considered unstable, and subject to breaking changes. In the future, we may offer stability; please file an issue if there is a use case where this would be valuable.

## License

This project is licensed under the terms of the MIT open source license. Please refer to [MIT](./LICENSE) for the full terms.