-- +goose Up

ALTER TABLE plt_system_parameters
    ADD COLUMN module VARCHAR(64),
    ADD COLUMN unit VARCHAR(32),
    ADD COLUMN effective_from DATE NOT NULL DEFAULT DATE '0001-01-01',
    ADD COLUMN effective_to DATE,
    ADD CONSTRAINT chk_plt_parameter_effective_range
        CHECK (effective_to IS NULL OR effective_to >= effective_from);

-- Only known namespace prefixes are assigned. Unknown existing keys abort the
-- migration so an owner can classify them explicitly rather than guessing.
UPDATE plt_system_parameters
SET module = CASE
    WHEN left(key, 4) = 'LNM_' THEN 'loan'
    WHEN left(key, 4) = 'DPM_' THEN 'deposit'
    WHEN left(key, 4) = 'FIN_' THEN 'finance'
    WHEN left(key, 4) = 'CAP_' THEN 'capital'
    WHEN left(key, 4) = 'CRM_' THEN 'crm'
    WHEN left(key, 4) = 'IAM_' THEN 'iam'
    WHEN left(key, 4) = 'PLT_' THEN 'platform'
    WHEN left(key, 9) = 'PLATFORM_' THEN 'platform'
    ELSE NULL
END
WHERE module IS NULL;

-- +goose StatementBegin
DO $$
DECLARE
    unresolved_keys TEXT;
BEGIN
    SELECT string_agg(key, ', ' ORDER BY key)
      INTO unresolved_keys
      FROM plt_system_parameters
     WHERE module IS NULL;
    IF unresolved_keys IS NOT NULL THEN
        RAISE EXCEPTION 'parameter module mapping required for existing keys: %', unresolved_keys;
    END IF;
END $$;
-- +goose StatementEnd

ALTER TABLE plt_system_parameters
    ALTER COLUMN module SET NOT NULL;

DROP INDEX IF EXISTS ux_plt_parameters_scope;
CREATE UNIQUE INDEX ux_plt_parameters_scope_effective
    ON plt_system_parameters (
        module, key, scope_type,
        COALESCE(scope_id, ''), COALESCE(tenant_id, ''), effective_from
    );
CREATE INDEX idx_plt_parameters_resolve
    ON plt_system_parameters (
        module, key, tenant_id, scope_type, scope_id, effective_from DESC
    );

-- +goose Down
DROP INDEX IF EXISTS idx_plt_parameters_resolve;
DROP INDEX IF EXISTS ux_plt_parameters_scope_effective;
CREATE UNIQUE INDEX ux_plt_parameters_scope
    ON plt_system_parameters (key, scope_type, COALESCE(scope_id, ''), COALESCE(tenant_id, ''));
ALTER TABLE plt_system_parameters
    DROP CONSTRAINT chk_plt_parameter_effective_range,
    DROP COLUMN effective_to,
    DROP COLUMN effective_from,
    DROP COLUMN unit,
    DROP COLUMN module;
