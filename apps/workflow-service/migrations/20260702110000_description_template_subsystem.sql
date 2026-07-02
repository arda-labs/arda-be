-- +goose Up
ALTER TABLE business_description_templates
    ADD COLUMN business_subsystem VARCHAR(20) NOT NULL DEFAULT 'FAC';

UPDATE business_description_templates
SET business_subsystem = 'CRM'
WHERE case_type LIKE 'CUSTOMER_%';

UPDATE business_description_templates
SET business_subsystem = 'FAC'
WHERE case_type LIKE 'FINANCE_%';

CREATE INDEX business_description_templates_subsystem_idx
    ON business_description_templates(business_subsystem);

-- +goose Down
DROP INDEX IF EXISTS business_description_templates_subsystem_idx;

ALTER TABLE business_description_templates
    DROP COLUMN IF EXISTS business_subsystem;
