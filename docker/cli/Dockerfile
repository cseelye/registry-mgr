FROM golang:1.25-alpine AS builder

WORKDIR /build
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o registry-cli \
    ./cmd/cli

FROM scratch
COPY --from=builder /build/registry-cli /registry-cli
ENTRYPOINT ["/registry-cli"]
