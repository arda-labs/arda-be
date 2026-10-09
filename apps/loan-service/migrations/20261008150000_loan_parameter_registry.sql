-- +goose Up
CREATE TABLE IF NOT EXISTS parameter (
    tenant_id text,
    module text NOT NULL,
    code text NOT NULL,
    scope text NOT NULL CHECK (scope IN ('GLOBAL','TENANT','ORG')),
    org_code text,
    value_type text NOT NULL CHECK (value_type IN ('STRING','INT','DECIMAL','BOOL','DATE','JSON')),
    value text NOT NULL,
    unit text NOT NULL DEFAULT '',
    effective_from date NOT NULL,
    effective_to date,
    CHECK ((scope = 'GLOBAL' AND tenant_id IS NULL AND org_code IS NULL)
        OR (scope = 'TENANT' AND tenant_id IS NOT NULL AND org_code IS NULL)
        OR (scope = 'ORG' AND tenant_id IS NOT NULL AND org_code IS NOT NULL AND org_code <> '')),
    CHECK (effective_to IS NULL OR effective_to >= effective_from)
);
CREATE UNIQUE INDEX IF NOT EXISTS parameter_scope_effective_uq
    ON parameter (tenant_id, module, code, scope, org_code, effective_from) NULLS NOT DISTINCT;
CREATE INDEX IF NOT EXISTS parameter_resolve_idx
    ON parameter (module, code, scope, effective_from DESC);

CREATE TABLE IF NOT EXISTS code_set (
    code text PRIMARY KEY,
    name text NOT NULL DEFAULT '',
    description text NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS code_item (
    set_code text NOT NULL REFERENCES code_set(code) ON DELETE CASCADE,
    code text NOT NULL,
    parent_code text,
    name text NOT NULL,
    sort integer NOT NULL DEFAULT 0,
    active boolean NOT NULL DEFAULT true,
    PRIMARY KEY (set_code, code),
    FOREIGN KEY (set_code, parent_code) REFERENCES code_item(set_code, code) DEFERRABLE INITIALLY DEFERRED
);

-- The legacy table has no tenant or effective-date columns. Refuse to guess a
-- tenant mapping; only its current global '%' row can be migrated safely.
-- +goose StatementBegin
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM lnm_general_provision_rates WHERE org_code <> '%') THEN
        RAISE EXCEPTION 'cannot migrate org-specific lnm_general_provision_rates without tenant/effective-date ownership';
    END IF;
    IF NOT EXISTS (SELECT 1 FROM lnm_general_provision_rates WHERE org_code = '%') THEN
        RAISE EXCEPTION 'required global general provision rate is missing';
    END IF;
END $$;
-- +goose StatementEnd

INSERT INTO parameter (tenant_id,module,code,scope,org_code,value_type,value,unit,effective_from,effective_to)
SELECT NULL,'loan','LNM_GENERAL_PROVISION_RATE','GLOBAL',NULL,'DECIMAL',rate_percent::text,'percent',DATE '0001-01-01',NULL
FROM lnm_general_provision_rates WHERE org_code='%'
ON CONFLICT DO NOTHING;

-- +goose Down
DELETE FROM parameter WHERE module='loan' AND code='LNM_GENERAL_PROVISION_RATE' AND scope='GLOBAL';
DROP TABLE IF EXISTS code_item;
DROP TABLE IF EXISTS code_set;
DROP INDEX IF EXISTS parameter_resolve_idx;
DROP INDEX IF EXISTS parameter_scope_effective_uq;
DROP TABLE IF EXISTS parameter;
