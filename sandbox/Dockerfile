# Diane Sandbox — Self-contained MCP server for memory operations
#
# Build: docker build -t diane-sandbox .
# Run:   docker run --rm \
#          -e MEMORY_SERVER_URL=https://memory.emergent-company.ai \
#          -e MEMORY_PROJECT_ID=your-project-id \
#          -e MEMORY_API_KEY=your-emt-token \
#          diane-sandbox
#
# The container starts as a stdio MCP server.
# Pipe JSON-RPC requests to stdin, receive responses on stdout.

FROM alpine:3.21

# Minimal runtime deps for Go binary (statically linked, no libc needed)
RUN apk add --no-cache ca-certificates tzdata

# Copy the diane-runner MCP server binary
COPY diane-runner /usr/local/bin/diane-runner

# Copy the memory CLI binary (needed by diane-runner for tool execution)
COPY memory /usr/local/bin/memory

# Copy skills markdown files for agent guidance
COPY skills/ /skills/

# Create a non-root user for sandbox security
RUN addgroup -S diane && adduser -S diane -G diane
USER diane

# The container entrypoint starts as an MCP stdio server.
# MP executor connects to it and discovers tools.
ENTRYPOINT ["diane-runner"]

# Metadata
LABEL org.opencontainers.image.title="Diane Sandbox"
LABEL org.opencontainers.image.description="Self-contained MCP server for Diane memory operations in sandbox containers"
LABEL org.opencontainers.image.version="1.0.0"
