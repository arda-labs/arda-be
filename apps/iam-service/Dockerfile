FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY . .
WORKDIR /src/apps/iam-service
RUN go mod download
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/bin/iam-service ./cmd/iam-service

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/iam-service /app/iam-service
COPY apps/iam-service/config /app/config
EXPOSE 8080 9090
ENTRYPOINT ["/app/iam-service"]
