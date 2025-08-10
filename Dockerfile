# Build stage
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

# Install ca-certificates and git for go mod
RUN apk --no-cache add ca-certificates git

# Build arguments for cross-compilation
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
ARG GITHUB_REPOSITORY=user/repo

WORKDIR /app

# Copy go mod files
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build the binary with static linking
RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-w -s -X main.Version=${VERSION}" \
    -o hcloud-minecraft-bot \
    .

# Final stage - scratch image
FROM scratch

# Copy ca-certificates from builder
COPY --from=builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

# Copy the binary
COPY --from=builder /app/hcloud-minecraft-bot /hcloud-minecraft-bot

# Add metadata
LABEL org.opencontainers.image.source="https://github.com/$GITHUB_REPOSITORY"
LABEL org.opencontainers.image.description="Telegram bot for managing Minecraft servers on Hetzner Cloud"
LABEL org.opencontainers.image.licenses="MIT"

# Run the binary
ENTRYPOINT ["/hcloud-minecraft-bot"]