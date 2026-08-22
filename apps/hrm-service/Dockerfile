FROM golang:1.26-alpine AS builder
WORKDIR /src
COPY . .
WORKDIR /src/apps/hrm-service
RUN go mod download
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/bin/hrm-service ./cmd/hrm-service

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/hrm-service /app/hrm-service
EXPOSE 8080
ENTRYPOINT ["/app/hrm-service"]
