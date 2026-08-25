# Versioned event contracts

`arda-events-v1.json` is the registry for event subjects and their stable
envelope metadata. Payload schemas are owned by the producer and must be added
before a consumer is enabled; a subject/event-code pair is not sufficient
evidence for a production consumer.

The registry intentionally distinguishes `reference-only` events from enabled
consumers. Notification currently has durable publisher/consumer primitives,
but its business handler remains disabled until an event-specific decoder and
integration fixture are assigned. The notification database now provides the
`noti_event_inbox` dedupe table and `ProcessEventOnce` transaction primitive;
adding those primitives alone does not count as consumer runtime evidence.
