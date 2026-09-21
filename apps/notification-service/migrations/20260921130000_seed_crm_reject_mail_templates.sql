-- +goose Up

-- Email templates for the CRM reject mail (W4). EPAS CRM.200.01 / CRM.201.01
-- only send mail on the reject branch (sendEmailListener + event_code
-- CRM.200.01.001 / CRM.201.01.001); the approve path has no mail. The delivery
-- worker looks the template up by notification event_type, so event_code here
-- is the EPAS mail event code carried by the workflow notification.
--
-- Seeded for the current tenant and the ngv-pilot tenant. SMTP sender config is
-- intentionally NOT seeded: the password is encrypted with the service secret
-- and must be created through the notification API/UI.

INSERT INTO noti_templates (tenant_id, event_code, channel, locale, subject, body, is_active)
VALUES
    ('00000000-0000-0000-0000-000000000010', 'CRM.200.01.001', 'email', 'vi-VN',
     '[Arda] Hồ sơ đăng ký khách hàng bị từ chối',
     'Hồ sơ {{caseCode}} đã bị từ chối phê duyệt.
Lý do: {{comment}}
Vui lòng chỉnh sửa và trình lại.', true),
    ('00000000-0000-0000-0000-000000000010', 'CRM.201.01.001', 'email', 'vi-VN',
     '[Arda] Hồ sơ điều chỉnh khách hàng bị từ chối',
     'Hồ sơ điều chỉnh {{caseCode}} đã bị từ chối phê duyệt.
Lý do: {{comment}}
Vui lòng chỉnh sửa và trình lại.', true),
    ('00000000-0000-0000-0000-000000000020', 'CRM.200.01.001', 'email', 'vi-VN',
     '[Arda] Hồ sơ đăng ký khách hàng bị từ chối',
     'Hồ sơ {{caseCode}} đã bị từ chối phê duyệt.
Lý do: {{comment}}
Vui lòng chỉnh sửa và trình lại.', true),
    ('00000000-0000-0000-0000-000000000020', 'CRM.201.01.001', 'email', 'vi-VN',
     '[Arda] Hồ sơ điều chỉnh khách hàng bị từ chối',
     'Hồ sơ điều chỉnh {{caseCode}} đã bị từ chối phê duyệt.
Lý do: {{comment}}
Vui lòng chỉnh sửa và trình lại.', true)
ON CONFLICT (tenant_id, event_code, channel, locale) DO UPDATE SET
    subject = EXCLUDED.subject,
    body = EXCLUDED.body,
    is_active = EXCLUDED.is_active,
    updated_at = now();

-- +goose Down

DELETE FROM noti_templates
WHERE channel = 'email'
  AND locale = 'vi-VN'
  AND event_code IN ('CRM.200.01.001', 'CRM.201.01.001')
  AND tenant_id IN ('00000000-0000-0000-0000-000000000010', '00000000-0000-0000-0000-000000000020');
