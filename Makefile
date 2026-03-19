.PHONY: build build-webui build-cli docker-build up down run-cli \
        dist dist-linux dist-linux-amd64 dist-linux-arm64 \
        dist-darwin dist-darwin-amd64 dist-darwin-arm64 \
        clean

GOLANG_IMAGE      := golang:1.25-alpine
DIST_DIR          := dist

# Override the registry host port for local dev (default in docker-compose.yml is 5000)
export REGISTRY_HOST_PORT ?= 5500

# Build both binaries as static executables (requires Go installed locally)
build: build-webui build-cli

build-webui:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/registry-webui ./cmd/webui

build-cli:
	CGO_ENABLED=0 go build -ldflags="-s -w" -o bin/registry-cli ./cmd/cli

# Build all Docker images (including CLI profile)
docker-build:
	docker compose --profile cli build

# Start registry and webui
up:
	docker compose up -d

# Stop all services
down:
	docker compose down

# Run CLI via docker compose (pass ARGS="<command> [flags]")
# Example: make run-cli ARGS="list"
# Example: make run-cli ARGS="delete myrepo:* --dry-run"
run-cli:
	docker compose run --rm cli $(ARGS)

# ---------------------------------------------------------------------------
# Dist targets: cross-compile via Docker and copy binaries to dist/
# Single quotes around the sh -c body preserve inner double quotes in
# -ldflags="-s -w" while still allowing Make to expand $(DIST_DIR).
# ---------------------------------------------------------------------------

dist: dist-linux dist-darwin

dist-linux: dist-linux-amd64 dist-linux-arm64

dist-darwin: dist-darwin-amd64 dist-darwin-arm64

dist-linux-amd64:
	@mkdir -p $(DIST_DIR)/linux-amd64
	docker run --rm -v "$(CURDIR):/build" -w /build $(GOLANG_IMAGE) sh -c \
		'go mod tidy && \
		 GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o $(DIST_DIR)/linux-amd64/registry-cli   ./cmd/cli && \
		 GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o $(DIST_DIR)/linux-amd64/registry-webui ./cmd/webui'

dist-linux-arm64:
	@mkdir -p $(DIST_DIR)/linux-arm64
	docker run --rm -v "$(CURDIR):/build" -w /build $(GOLANG_IMAGE) sh -c \
		'go mod tidy && \
		 GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o $(DIST_DIR)/linux-arm64/registry-cli   ./cmd/cli && \
		 GOOS=linux GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o $(DIST_DIR)/linux-arm64/registry-webui ./cmd/webui'

dist-darwin-amd64:
	@mkdir -p $(DIST_DIR)/darwin-amd64
	docker run --rm -v "$(CURDIR):/build" -w /build $(GOLANG_IMAGE) sh -c \
		'go mod tidy && \
		 GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o $(DIST_DIR)/darwin-amd64/registry-cli   ./cmd/cli && \
		 GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -ldflags="-s -w" -o $(DIST_DIR)/darwin-amd64/registry-webui ./cmd/webui'

dist-darwin-arm64:
	@mkdir -p $(DIST_DIR)/darwin-arm64
	docker run --rm -v "$(CURDIR):/build" -w /build $(GOLANG_IMAGE) sh -c \
		'go mod tidy && \
		 GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o $(DIST_DIR)/darwin-arm64/registry-cli   ./cmd/cli && \
		 GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -ldflags="-s -w" -o $(DIST_DIR)/darwin-arm64/registry-webui ./cmd/webui'

# Remove build artifacts
clean:
	rm -rf bin/ $(DIST_DIR)/
