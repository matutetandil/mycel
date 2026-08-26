FROM golang:1.25-alpine AS builder
WORKDIR /build
# The whole package, not one file: it grew a second one for the TLS listener,
# and copying main.go alone meant the image failed to build — which every
# service that waits on this one then failed with.
COPY *.go ./
# No go.mod here — the package is built by naming its files, the way the
# single-file version was.
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o mock-server *.go

FROM alpine:3.24
COPY --from=builder /build/mock-server /app/mock-server
EXPOSE 8888
EXPOSE 8443
CMD ["/app/mock-server"]
