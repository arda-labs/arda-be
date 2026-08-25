FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY . .
WORKDIR /src/apps/ai-service
RUN go mod download
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/bin/ai-service ./cmd/ai-service

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/ai-service /app/ai-service
EXPOSE 8080
ENTRYPOINT ["/app/ai-service"]
