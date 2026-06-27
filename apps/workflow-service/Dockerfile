FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/bin/workflow-service ./cmd/workflow-service

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/workflow-service /app/workflow-service
EXPOSE 8080
ENTRYPOINT ["/app/workflow-service"]
