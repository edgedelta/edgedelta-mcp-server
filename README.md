# Edge Delta MCP Server

The **Edge Delta MCP Server** is a [Model Context Protocol (MCP)](https://modelcontextprotocol.io/introduction)
server that provides seamless integration with Edge Delta APIs, enabling advanced
automation and interaction capabilities for developers and tools.

## Use Cases

- Extract and analyse observability data from Edge Delta.
- Build AI‑powered tools and applications that interact with Edge Delta’s platform.

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

