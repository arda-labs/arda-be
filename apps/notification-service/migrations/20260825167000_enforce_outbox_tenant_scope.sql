-- +goose Up
-- Outbox rows are tenant-owned event records. Do not allow a new event to
-- silently enter the shared stream without an authoritative tenant.
ALTER TABLE noti_outbox
    ALTER COLUMN tenant_id DROP DEFAULT,
    ADD CONSTRAINT noti_outbox_tenant_id_nonempty
    CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;

ALTER TABLE noti_notifications
    ADD CONSTRAINT noti_notifications_tenant_id_nonempty
    CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;
ALTER TABLE noti_deliveries
    ADD CONSTRAINT noti_deliveries_tenant_id_nonempty
    CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;
ALTER TABLE noti_inbox
    ADD CONSTRAINT noti_inbox_tenant_id_nonempty
    CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;
ALTER TABLE noti_push_subscriptions
    ADD CONSTRAINT noti_push_subscriptions_tenant_id_nonempty
    CHECK (btrim(tenant_id) <> '' AND lower(btrim(tenant_id)) <> 'default') NOT VALID;

-- +goose Down
ALTER TABLE noti_outbox
    DROP CONSTRAINT IF EXISTS noti_outbox_tenant_id_nonempty;
ALTER TABLE noti_push_subscriptions
    DROP CONSTRAINT IF EXISTS noti_push_subscriptions_tenant_id_nonempty;
ALTER TABLE noti_inbox
    DROP CONSTRAINT IF EXISTS noti_inbox_tenant_id_nonempty;
ALTER TABLE noti_deliveries
    DROP CONSTRAINT IF EXISTS noti_deliveries_tenant_id_nonempty;
ALTER TABLE noti_notifications
    DROP CONSTRAINT IF EXISTS noti_notifications_tenant_id_nonempty;
