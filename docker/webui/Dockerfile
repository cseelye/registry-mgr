FROM golang:1.25-alpine AS builder

WORKDIR /build
COPY . .
RUN go mod tidy && CGO_ENABLED=0 GOOS=linux go build \
    -ldflags="-s -w" \
    -o registry-webui \
    ./cmd/webui

FROM scratch
COPY --from=builder /build/registry-webui /registry-webui
EXPOSE 5080
ENTRYPOINT ["/registry-webui"]
