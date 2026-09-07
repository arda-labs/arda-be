FROM golang:1.27.1-alpine AS builder
WORKDIR /src
COPY . .
WORKDIR /src/apps/deposit-service
RUN go mod download
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/bin/deposit-service ./cmd/deposit-service

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/deposit-service /app/deposit-service
EXPOSE 8080 9090
ENTRYPOINT ["/app/deposit-service"]
