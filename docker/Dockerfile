# Diane Test Node — Docker image for running CI tests against diane serve
#
# Build: docker build -t diane-test-node -f docker/Dockerfile .
# Run:   docker run -d --name diane-test \
#          -e MEMORY_SERVER_URL=https://memory.emergent-company.ai \
#          -e MEMORY_PROJECT_ID=<project-uuid> \
#          -e MEMORY_API_KEY=<emt-token> \
#          -p 8890:8890 \
#          diane-test-node
#
# Uses multi-stage build: Go builder → minimal alpine runtime.

# ── Stage 1: Build ───────────────────────────────────────────────────────────
FROM golang:1.25-alpine AS builder

RUN apk add --no-cache git ca-certificates

WORKDIR /build
COPY server/ server/

# Build statically linked binary with version info
WORKDIR /build/server/cmd/diane
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -ldflags="-s -w -X main.Version=${VERSION}" -o /diane .

# ── Stage 2: Runtime ─────────────────────────────────────────────────────────
FROM alpine:3.21

# Runtime dependencies
RUN apk add --no-cache ca-certificates tzdata gettext-envsubst curl

# Create non-root user
RUN addgroup -S diane && adduser -S diane -G diane

# Copy binary
COPY --from=builder /diane /usr/local/bin/diane

# Copy entrypoint and config template
COPY docker/entrypoint.sh /entrypoint.sh
COPY docker/config.tmpl.yml /etc/diane/config.tmpl.yml
RUN chmod +x /entrypoint.sh

# Data directory
RUN mkdir -p /var/lib/diane && chown diane:diane /var/lib/diane

USER diane

EXPOSE 8890

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=3 \
  CMD curl -sf http://localhost:8890/api/status > /dev/null 2>&1

ENTRYPOINT ["/entrypoint.sh"]

LABEL org.opencontainers.image.title="Diane Test Node"
LABEL org.opencontainers.image.description="Diane slave node for integration testing in CI"
LABEL org.opencontainers.image.version="1.0.0"
