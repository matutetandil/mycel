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
# The current stable branch. Which one hardly matters for what a scanner
# reports — with the upgrade below, 3.22 and 3.24 both come out clean — but a
# newer branch has a longer runway of patches ahead of it, and once a branch
# stops receiving them there is nothing left for `apk upgrade` to fetch.
FROM alpine:3.24

# Upgrade before installing anything.
#
# A base image is built once and then sits there: alpine:3.22 currently bakes
# in libcrypto3 3.5.7-r0 while the repository it points at has 3.5.8-r0, the
# one that fixes CVE-2026-14456. Nothing here used that repository for the
# packages already present, so the image shipped a vulnerability whose patch
# was one fetch away — twenty findings, two of them HIGH, in an image that
# scans clean the moment the packages it already has are brought up to date.
#
# The cost is that two builds a week apart are not byte-identical, which was
# already true: apk add fetches current versions for everything below.
#
# ca-certificates for HTTPS, tzdata for timezones, and the ssh client for
# `exec { driver = "ssh" }` — that connector runs the client rather than
# speaking the protocol itself, so without this the documented feature cannot
# work in the image Mycel is documented to run in.
RUN apk upgrade --no-cache && \
    apk add --no-cache ca-certificates tzdata openssh-client

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
