FROM golang:1.27.1-alpine AS builder
WORKDIR /src
COPY . .
WORKDIR /src/apps/ai-service
RUN go mod download
# Keep the image build fail-closed when a migration is accidentally omitted
# from the build context. These two migrations are both required by the
# current schema history and are embedded into the service binary below.
RUN test -f migrations/20260916090000_ai_provider_presets.sql \
    && test -f migrations/20260916100000_ai_approval_arguments.sql
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/bin/ai-service ./cmd/ai-service

FROM alpine:3.21
ARG SOURCE_COMMIT=unknown
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/ai-service /app/ai-service
LABEL org.opencontainers.image.revision="$SOURCE_COMMIT"
EXPOSE 8080 9090
ENTRYPOINT ["/app/ai-service"]
