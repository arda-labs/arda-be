-- +goose Up
-- Keep notification records in the same explicit tenant namespace as their
-- source events. Empty/default values are the old single-tenant placeholder.
UPDATE noti_notifications SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');
UPDATE noti_deliveries SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');
UPDATE noti_inbox SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');
UPDATE noti_outbox SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');
UPDATE noti_push_subscriptions SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');
UPDATE notification_outbox_dlq SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');
UPDATE noti_event_inbox SET tenant_id = '00000000-0000-0000-0000-000000000010' WHERE lower(btrim(tenant_id)) IN ('', 'default');

ALTER TABLE noti_outbox VALIDATE CONSTRAINT noti_outbox_tenant_id_nonempty;
ALTER TABLE noti_notifications VALIDATE CONSTRAINT noti_notifications_tenant_id_nonempty;
ALTER TABLE noti_deliveries VALIDATE CONSTRAINT noti_deliveries_tenant_id_nonempty;
ALTER TABLE noti_inbox VALIDATE CONSTRAINT noti_inbox_tenant_id_nonempty;
ALTER TABLE noti_push_subscriptions VALIDATE CONSTRAINT noti_push_subscriptions_tenant_id_nonempty;

-- +goose Down
UPDATE noti_notifications SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE noti_deliveries SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE noti_inbox SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE noti_outbox SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE noti_push_subscriptions SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE notification_outbox_dlq SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
UPDATE noti_event_inbox SET tenant_id = 'default' WHERE tenant_id = '00000000-0000-0000-0000-000000000010';
