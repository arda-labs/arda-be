FROM golang:1.27.2-alpine AS builder
WORKDIR /src
COPY . .
WORKDIR /src/apps/document-service
RUN go mod download
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/bin/document-service ./cmd/document-service

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/document-service /app/document-service
COPY apps/document-service/configs /app/configs
EXPOSE 8080
# Run as nobody; the pod securityContext may override with its own UID.
USER 65534:65534
ENTRYPOINT ["/app/document-service"]
