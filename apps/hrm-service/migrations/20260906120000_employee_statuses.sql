-- +goose Up

-- Employee working-status catalog (EPAS HrmCfgStatus parity): employees
-- reference these codes instead of free-text status values.
CREATE TABLE IF NOT EXISTS hrm_employee_statuses (
    id          text PRIMARY KEY,
    code        text NOT NULL,
    name        text NOT NULL,
    description text,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    tenant_id   text NOT NULL DEFAULT ''
);

INSERT INTO hrm_employee_statuses (id, code, name, description) VALUES
    ('status_seed_active', 'ACTIVE', 'Đang làm việc', 'Nhân viên đang làm việc bình thường'),
    ('status_seed_leave', 'ON_LEAVE', 'Nghỉ phép / tạm ngừng', 'Nghỉ thai sản, nghỉ không lương, tạm ngừng'),
    ('status_seed_suspended', 'SUSPENDED', 'Đình chỉ', 'Đình chỉ công việc theo quyết định'),
    ('status_seed_resigned', 'RESIGNED', 'Đã nghỉ việc', 'Nghỉ việc theo nguyện vọng hoặc chấm dứt HĐLĐ'),
    ('status_seed_retired', 'RETIRED', 'Nghỉ hưu', 'Chế độ nghỉ hưu')
ON CONFLICT DO NOTHING;

-- +goose Down

DELETE FROM hrm_employee_statuses WHERE id LIKE 'status_seed_%';
DROP TABLE IF EXISTS hrm_employee_statuses;
