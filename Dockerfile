# Build
FROM golang:1.25-alpine AS builder
WORKDIR /build
COPY . .
RUN go build -o relay ./cmd/relay/main.go

# Run
FROM alpine:3.19
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /build/relay .
EXPOSE 8080
ENTRYPOINT ["./relay"]
