# Build stage
FROM golang:1.22-alpine AS builder

# Install git and ca-certificates (needed for fetching dependencies)
RUN apk add --no-cache git ca-certificates

WORKDIR /workspace

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY cmd/ cmd/
COPY pkg/ pkg/
COPY internal/ internal/

# Build controller binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o controller \
    ./cmd/controller

# Build CLI binary
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build \
    -ldflags="-w -s" \
    -o kubeassume \
    ./cmd/cli

# Runtime stage
FROM gcr.io/distroless/static:nonroot

WORKDIR /

# Copy binaries from builder
COPY --from=builder /workspace/controller /controller
COPY --from=builder /workspace/kubeassume /kubeassume

# Run as non-root user
USER 65532:65532

ENTRYPOINT ["/controller"]
