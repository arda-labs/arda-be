-- +goose Up

-- Align the notification admin menu with the API contract: the notification
-- templates/senders/events/DLQ endpoints are guarded by `notification.*`
-- (auth-gateway policy + iam 20260825100000), so the menu must require
-- `notification.manage` instead of `platform.read` — otherwise a platform
-- reader sees the menu and gets 403, and a notification admin never sees it.

UPDATE plt_menus
SET required_permission = 'notification.manage',
    updated_at = now()
WHERE code = 'platform.notifications'
  AND tenant_id IS NULL;

-- +goose Down

UPDATE plt_menus
SET required_permission = 'platform.read',
    updated_at = now()
WHERE code = 'platform.notifications'
  AND tenant_id IS NULL;
