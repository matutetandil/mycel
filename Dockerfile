# Build stage
# Go 1.25: go.mod requires >= 1.25 (grpc / golang.org/x/* pulled in by the
# OpenTelemetry deps declare go 1.25). Keep in sync with the go directive.
FROM golang:1.25-alpine AS builder

WORKDIR /build

# Install git for go mod download
RUN apk add --no-cache git

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary.
#
# COVERAGE is empty for every normal build, which leaves the command below
# byte-identical to what it was. Setting it to -cover produces a binary that
# writes coverage counters to GOCOVERDIR on graceful shutdown, which is how the
# integration suite's real coverage gets measured — those packages talk to
# RabbitMQ, Postgres and the rest, so `go test` alone reports them near zero
# while the integration suite exercises them heavily.
ARG COVERAGE=""
RUN CGO_ENABLED=0 GOOS=linux go build ${COVERAGE:+-cover -coverpkg=./...} \
    -ldflags="-s -w" -o mycel ./cmd/mycel

# Final stage
FROM alpine:3.22

# Add ca-certificates for HTTPS and tzdata for timezones
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN adduser -D -u 1000 mycel

# Create config directory (standard Linux config path)
RUN mkdir -p /etc/mycel && chown mycel:mycel /etc/mycel

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/mycel /app/mycel

# Switch to non-root user
USER mycel

# Environment variables with sensible defaults for production
# MYCEL_ENV: development, staging, production (default: development)
# MYCEL_LOG_LEVEL: debug, info, warn, error (default: info)
# MYCEL_LOG_FORMAT: text, json (default: text, use json for production)
ENV MYCEL_ENV=production
ENV MYCEL_LOG_LEVEL=info
ENV MYCEL_LOG_FORMAT=json

# Expose common ports
EXPOSE 3000 4000 50051

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:3000/health || exit 1

# Default command - config always at /etc/mycel
ENTRYPOINT ["/app/mycel"]
CMD ["start", "--config", "/etc/mycel"]
