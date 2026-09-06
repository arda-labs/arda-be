FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY . .
WORKDIR /src/apps/loan-service
RUN go mod download
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/bin/loan-service ./cmd/loan-service

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/loan-service /app/loan-service
EXPOSE 8080 9090
ENTRYPOINT ["/app/loan-service"]
