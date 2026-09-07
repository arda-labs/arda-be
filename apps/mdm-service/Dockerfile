FROM golang:1.27.1-alpine AS builder
WORKDIR /src
COPY . .
WORKDIR /src/apps/mdm-service
RUN go mod download
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/bin/mdm-service ./cmd/mdm-service

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/mdm-service /app/mdm-service
EXPOSE 8080 9090
ENTRYPOINT ["/app/mdm-service"]
