FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY . .
WORKDIR /src/apps/auth-gateway
RUN go mod download
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/bin/auth-gateway ./cmd/auth-gateway

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/auth-gateway /app/auth-gateway
COPY apps/auth-gateway/configs /app/configs
EXPOSE 8080
ENTRYPOINT ["/app/auth-gateway"]
