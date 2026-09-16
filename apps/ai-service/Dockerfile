FROM golang:1.27.1-alpine AS builder
WORKDIR /src
COPY . .
WORKDIR /src/apps/ai-service
RUN go mod download
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/bin/ai-service ./cmd/ai-service
# ai-eval powers the in-cluster regression CronJob (arda-infra
# k8s/apps/ai-eval-cronjob.yaml); it runs the bundled golden set against the
# deployed service. The set is a verified copy of scripts/ai-dev-corpus
# (scripts/check-ai-eval-set.mjs enforces the sync).
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/bin/ai-eval ./cmd/ai-eval

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/ai-service /app/ai-service
COPY --from=builder /app/bin/ai-eval /app/ai-eval
COPY --from=builder /src/apps/ai-service/eval/evaluation-set.yaml /app/eval/evaluation-set.yaml
EXPOSE 8080 9090
ENTRYPOINT ["/app/ai-service"]
