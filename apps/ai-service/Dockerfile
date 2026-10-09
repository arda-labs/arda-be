FROM golang:1.27.2-alpine AS builder
WORKDIR /src
COPY . .
WORKDIR /src/apps/ai-service
RUN go mod download
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/bin/ai-service ./cmd/ai-service
# ai-eval powers the in-cluster regression CronJob (arda-infra
# k8s/apps/ai-eval-cronjob.yaml); it runs the bundled golden sets against the
# deployed service. The sets are verified copies of scripts/ai-dev-corpus
# (scripts/check-ai-eval-set.mjs enforces the sync).
RUN CGO_ENABLED=0 go build -ldflags="-w -s" -o /app/bin/ai-eval ./cmd/ai-eval

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=builder /app/bin/ai-service /app/ai-service
COPY --from=builder /app/bin/ai-eval /app/ai-eval
COPY --from=builder /src/apps/ai-service/eval/ /app/eval/
EXPOSE 8080 9090

# Run unprivileged. alpine:3.21 already ships `nobody` (65534), so this needs no
# extra build step and cannot fail on a busybox flag difference. The uid that
# actually runs in the cluster is pinned by runAsUser in the pod manifest; this
# directive exists so the image is not root-by-default in any other context.
USER 65534:65534
ENTRYPOINT ["/app/ai-service"]
