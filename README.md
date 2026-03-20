# Registry Manager

A vibe-coded management tool for a private [Docker registry](https://hub.docker.com/_/registry). Provides a web UI and a CLI that share a common registry API client. Neither interface can push or pull images; they exist solely to inspect and delete them. Also included is the registry image itself enhanced with an automatic garbage collection run every night.

---

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) with the Compose plugin

The tools are written in Go, but the build is setup to run in a container so you don't need Go installed locally.

---

## Quick Start

### 1. Clone the repo

```bash
git clone https://github.com/cseelye/registry-mgr.git
cd registry-mgr
```

### 2. Set credentials (optional)

If you want the registry to require authentication, export `REGISTRY_CREDENTIALS` before starting the stack:

```bash
export REGISTRY_CREDENTIALS=admin:mysecretpassword
```

The registry container generates an `htpasswd` file from this at startup. The web UI and CLI containers use the same variable to authenticate their API calls. If `REGISTRY_CREDENTIALS` is not set, the registry runs without authentication.

### 3. Start the stack

```bash
docker compose up -d
```

This builds the images (if not already built) and starts:
- `registry` — the private registry on port `5000`
- `webui` — the web UI on port `5080`

### 4. Open the web UI

Navigate to **http://localhost:5080** in your browser.

### 5. Stop the stack

```bash
docker compose down
```

Registry data is stored in a named Docker volume (`registry_mgr_registry-data`) and persists across restarts.

---

## Custom Registry Image

The registry image (`docker/registry/`) extends the official `registry` image with:

- **Basic auth** (optional) — if `REGISTRY_CREDENTIALS` is set, `htpasswd` credentials are generated at startup; otherwise the registry runs without authentication
- **Delete enabled** — `storage.delete.enabled: true` is set in the registry config
- **Automated garbage collection** — a nightly GC run is built into the container's entrypoint

### Garbage collection

The entrypoint script manages the registry process in a keep-alive loop and handles nightly GC without an external cron daemon:

1. Starts the registry in a background keep-alive loop (restarts on unexpected exit)
2. Main loop sleeps until the configured GC time
3. At GC time: kills the registry, runs `registry garbage-collect` with `REGISTRY_STORAGE_MAINTENANCE_READONLY_ENABLED=true` (puts the registry in read-only mode for the duration), then restarts it

Configure GC via environment variables in `docker-compose.yml`:

| Variable | Default | Description |
|---|---|---|
| `GC_ENABLED` | `true` | Enable/disable automatic GC |
| `GC_TIME` | `03:00` | Time of day to run GC (24h `HH:MM`) |

### Registry version

The base registry version is configurable at build time:

```bash
docker build --build-arg REGISTRY_VERSION=3 docker/registry/
```

---

## Configuration

Both binaries share the same configuration system. Settings are resolved in this order (highest priority first):

| Priority | Source |
|---|---|
| 1 | CLI flags |
| 2 | Environment variables |
| 3 | YAML config file |

### YAML config file

```yaml
registry_url: http://localhost:5000
username: admin
password: secret

# Web UI only
port: 5080
listen_addr: 0.0.0.0
```

Pass the config file path with `--config /path/to/config.yaml`.

### Environment variables

| Variable | Description |
|---|---|
| `REGISTRY_URL` | Registry URL |
| `REGISTRY_CREDENTIALS` | Credentials as `username:password` |
| `WEBUI_PORT` | Web UI listen port (default `5080`) |
| `WEBUI_LISTEN` | Web UI listen address (default `0.0.0.0`) |

---

## Using the Web UI

Open **http://localhost:5080** in your browser.

- **Expand image details** — click a tag name to expand its digest, OS/architecture, and labels
- **Delete a single tag** — click the Delete button in the row; confirm in the dialog
- **Delete multiple tags** — check the boxes, click Delete Selected; confirm in the dialog
- **Refresh** — click the Refresh button to reload the image list without a full browser refresh

---

## Using the CLI

### Via Docker Compose

```bash
docker compose run --rm cli <command> [flags]

# Examples
docker compose run --rm cli list
docker compose run --rm cli list -l
docker compose run --rm cli inspect alpine:3.19
docker compose run --rm cli delete --dry-run 'alpine:*'
docker compose run --rm cli delete -f busybox:latest
```

### As a native binary

```bash
make dist-darwin-arm64  # or the appropriate platform target

./dist/darwin-arm64/registry-cli --registry http://localhost:5000 \
  --username admin --password mysecretpassword \
  list
```

### Commands

#### `list`

List all repositories and their tags.

```
registry-cli list [flags]

Flags:
  -l, --long    show detailed info (digest, size, arch, labels)
```

#### `inspect <repo:tag>`

Show full details for a single image.

```
registry-cli inspect alpine:3.19
```

#### `delete <pattern>`

Delete images matching a pattern. Supports `*` as a wildcard in either the repository or tag component.

```
registry-cli delete <pattern> [flags]

Flags:
      --dry-run   show what would be deleted without deleting
  -f, --force     skip confirmation prompt

Pattern examples:
  alpine:3.19       delete a specific tag
  alpine:*          delete all tags in a repository
  *:latest          delete the latest tag from every repository
  *:3.*             delete all tags starting with "3." from every repository
  *:*               delete everything
```

Without `--force`, the CLI will list the matching images and prompt `[y/N]` before deleting.

---

## Architecture

```
registry_mgr/
├── cmd/
│   ├── cli/          # CLI binary entry point
│   └── webui/        # Web UI binary entry point + HTML templates
├── internal/
│   ├── config/       # Layered config loading (flags > env > yaml)
│   ├── models/       # Shared data types
│   └── registry/     # Docker Registry HTTP API V2 client
├── docker/
│   ├── cli/          # Dockerfile for CLI scratch container
│   ├── registry/     # Custom registry image (Dockerfile, config, entrypoint)
│   └── webui/        # Dockerfile for web UI scratch container
├── docker-compose.yml
└── Makefile
```

Both `registry-cli` and `registry-webui` are compiled as fully static Go binaries and deployed in `scratch` containers. The registry image is based on `registry:3` and extended with automated garbage collection.

### Registry API

The tool speaks the [Docker Registry HTTP API V2](https://distribution.github.io/distribution/spec/api/). It supports both OCI (`application/vnd.oci.image.manifest.v1+json`) and Docker v2 (`application/vnd.docker.distribution.manifest.v2+json`) manifest formats.

---

## Building

### Docker images

```bash
make docker-build
```

This builds all three images: `registry_mgr-registry`, `registry_mgr-webui`, and `registry_mgr-cli`.

### Native binaries

```bash
make dist               # all platforms (linux + darwin, amd64 + arm64)
make dist-linux         # linux/amd64 and linux/arm64
make dist-darwin        # darwin/amd64 and darwin/arm64
make dist-linux-amd64   # single platform
make dist-linux-arm64
make dist-darwin-amd64
make dist-darwin-arm64
```

Binaries are written to `dist/<os>-<arch>/` (e.g. `dist/darwin-arm64/registry-cli`). Builds run inside a Docker container so no local Go install is required. Binaries are fully static (`CGO_ENABLED=0`) and suitable for running natively on any Linux/macOS host or copying into a container.

---

## Development

A [Dev Container](https://containers.dev/) configuration is included for VS Code (`.devcontainer/devcontainer.json`). It provides:

- Go 1.25 toolchain
- Go and Docker VS Code extensions
- Host Docker socket mounted (so `docker compose` works inside the container)
- Ports 5000 and 5080 forwarded
- `go mod tidy` run automatically on container creation
