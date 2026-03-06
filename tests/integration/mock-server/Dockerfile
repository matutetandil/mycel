FROM golang:1.24-alpine AS builder
WORKDIR /build
COPY main.go .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o mock-server main.go

FROM alpine:3.19
COPY --from=builder /build/mock-server /app/mock-server
EXPOSE 8888
CMD ["/app/mock-server"]
