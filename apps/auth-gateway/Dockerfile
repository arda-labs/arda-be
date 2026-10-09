FROM golang:1.27.2-alpine AS builder
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
EXPOSE 8080 9090

# Run unprivileged. alpine:3.21 already ships `nobody` (65534), so this needs no
# extra build step and cannot fail on a busybox flag difference. The uid that
# actually runs in the cluster is pinned by runAsUser in the pod manifest; this
# directive exists so the image is not root-by-default in any other context.
USER 65534:65534
ENTRYPOINT ["/app/auth-gateway"]
