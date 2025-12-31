# Build stage
FROM golang:1.24-alpine AS builder

WORKDIR /build

# Install git for go mod download
RUN apk add --no-cache git

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o mycel ./cmd/mycel

# Final stage
FROM alpine:3.19

# Add ca-certificates for HTTPS and tzdata for timezones
RUN apk add --no-cache ca-certificates tzdata

# Create non-root user
RUN adduser -D -u 1000 mycel

# Create config directory
RUN mkdir -p /config && chown mycel:mycel /config

WORKDIR /app

# Copy binary from builder
COPY --from=builder /build/mycel /app/mycel

# Switch to non-root user
USER mycel

# Default config directory
ENV MYCEL_CONFIG=/config

# Expose common ports
EXPOSE 3000 4000 50051

# Health check
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD wget --no-verbose --tries=1 --spider http://localhost:3000/health || exit 1

# Default command
ENTRYPOINT ["/app/mycel"]
CMD ["start", "--config", "/config"]
