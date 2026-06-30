FROM golang:1.26-alpine AS builder
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/bin/iam-service ./cmd/iam-service

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/iam-service /app/iam-service
COPY config /app/config
EXPOSE 8080
ENTRYPOINT ["/app/iam-service"]
