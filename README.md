# Registry Manager

A vibe-coded management tool for a private [Docker registry](https://hub.docker.com/_/registry). Provides two interfaces — a web UI and a CLI — that share a common registry API client. Neither interface can push or pull images; they exist solely to inspect and delete them.

## Features

- List all repositories and tags in the registry
- Inspect detailed image metadata (digest, size, creation time, OS, architecture, labels)
- Delete images by tag, with wildcard pattern support
- Web UI with expandable image details, multi-select delete, and confirmation dialogs
- CLI with `--dry-run`, `--force`, and wildcard patterns (`repo:*`, `*:tag`, `repo:tag*`)
- Automated nightly garbage collection built into the registry container

---

## Architecture

```
registry_mgr/
├── cmd/
│   ├── cli/          # CLI binary entry point
│   └── webui/        # Web UI binary entry point + HTML templates
├── internal/
│   ├── config/       # Layered config loading (flags > env > secret file > yaml)
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

## Prerequisites

- [Docker](https://docs.docker.com/get-docker/) with the Compose plugin
- GNU Make

To build the CLI as a native binary, Go 1.25+ is also required.

---

## Configuration

Both binaries share the same configuration system. Settings are resolved in this order (highest priority first):

| Priority | Source |
|---|---|
| 1 | CLI flags |
| 2 | Environment variables |
| 3 | Credentials file (`username:password`) |
| 4 | YAML config file |

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
| `REGISTRY_MGR_USERNAME` | Username |
| `REGISTRY_MGR_PASSWORD` | Password |
| `REGISTRY_CREDENTIALS_FILE` | Path to `username:password` file |
| `WEBUI_PORT` | Web UI listen port (default `5080`) |
| `WEBUI_LISTEN` | Web UI listen address (default `0.0.0.0`) |

### Credentials file

A plain text file containing `username:password` on a single line:

```
admin:mysecretpassword
```

---

## Building

### Docker images (recommended)

```bash
make docker-build
```

This builds all three images: `registry_mgr-registry`, `registry_mgr-webui`, and `registry_mgr-cli`.

### Native CLI binary

Requires Go 1.25+.

```bash
make build          # builds both binaries to bin/
make build-cli      # CLI only  → bin/registry-cli
make build-webui    # web UI only → bin/registry-webui
```

The binaries are built with `CGO_ENABLED=0` and are fully static — suitable for running natively on any Linux/macOS host or copying into a container.

---

## Deployment

### 1. Create the credentials file

```bash
mkdir -p secrets
echo "admin:mysecretpassword" > secrets/registry_credentials
```

The credentials file is used in two ways:
- The registry container generates an `htpasswd` file from it for authentication
- The web UI and CLI containers read the plaintext credentials to authenticate their API calls

### 2. Start the stack

```bash
make up
```

This starts:
- `registry` — the custom registry on port `5000`
- `webui` — the web UI on port `5080`

The CLI service is excluded from `make up` (it uses a Compose profile). See [Using the CLI](#using-the-cli).

### 3. Stop the stack

```bash
make down
```

Registry data is stored in a named Docker volume (`registry_mgr_registry-data`) and persists across restarts.

---

## Custom Registry Image

The registry image (`docker/registry/`) extends the official `registry:3` image with:

- **Basic auth** — `htpasswd` credentials are generated at startup from the credentials file
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

### Build argument

The base registry version is configurable at build time:

```bash
docker build --build-arg REGISTRY_VERSION=3 docker/registry/
```

---

## Using the Web UI

Open **http://localhost:5080** in your browser.

- **Expand image details** — click a tag name to expand its digest, OS/architecture, and labels
- **Delete a single tag** — click the Delete button in the row; confirm in the dialog
- **Delete multiple tags** — check the boxes, click Delete Selected; confirm in the dialog
- **Refresh** — click the Refresh button to reload the image list without a full browser refresh

---

## Using the CLI

### Via Docker Compose (recommended)

```bash
make run-cli ARGS="<command> [flags]"

# Examples
make run-cli ARGS="list"
make run-cli ARGS="list -l"
make run-cli ARGS="inspect alpine:3.19"
make run-cli ARGS="delete --dry-run 'alpine:*'"
make run-cli ARGS="delete -f busybox:latest"
```

### Via docker exec

```bash
docker compose exec cli registry-cli list
```

### As a native binary

```bash
make build-cli

./bin/registry-cli --registry http://localhost:5000 \
  --credentials-file ./secrets/registry_credentials \
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

## Development

A [Dev Container](https://containers.dev/) configuration is included for VS Code (`.devcontainer/devcontainer.json`). It provides:

- Go 1.25 toolchain
- Go and Docker VS Code extensions
- Host Docker socket mounted (so `docker compose` works inside the container)
- Ports 5000 and 5080 forwarded
- `go mod tidy` run automatically on container creation
