FROM golang:1.27.1-alpine AS builder
WORKDIR /src
COPY . .
WORKDIR /src/apps/workflow-service
RUN go mod download
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/bin/workflow-service ./cmd/workflow-service

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/workflow-service /app/workflow-service
EXPOSE 8080 9090
ENTRYPOINT ["/app/workflow-service"]
