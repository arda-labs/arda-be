# notification-service

Arda notification microservice.

Browser inbox/SSE and push APIs remain HTTP. Internal notification acceptance is
gRPC-only on `GRPC_ADDR`, protected by mTLS and the signed workload identity
contract; durable delivery continues through the outbox/NATS flow. It does not
connect to Zeebe or host Zeebe workers.

## Controlled DLQ replay

Replay is deliberately one event per invocation and records the operator in
`notification_outbox_dlq.replayed_by`. It requires an explicit confirmation
token and reads the database DSN only from the environment; it never prints the
event payload:

```bash
DATABASE_DSN='...' go run ./cmd/notification-replay \
  --outbox-id '<outbox-id>' \
  --operator '<operator-id>' \
  --confirm REPLAY_ONE
```

Inspect the DLQ row, tenant and event ID before invoking it. Bulk replay,
cross-tenant selection and replay without an operator identity are not
supported.

Planning:

- [P13 Notification Service](../../../docs/roadmap/13-notification-service.md)
- [Notification Service Execution Checklist](../../../docs/planning/notification-service-execution-checklist.md)
