-- +goose Up
ALTER TABLE customers ADD COLUMN IF NOT EXISTS customer_type VARCHAR(20) NOT NULL DEFAULT 'PERSONAL';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS mobile VARCHAR(50) NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS identity_no VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS address TEXT NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS segment VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS customer_rank VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS risk_level VARCHAR(64) NOT NULL DEFAULT '';
ALTER TABLE customers ADD COLUMN IF NOT EXISTS general_info JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS personal_info JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS business_info JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS extended_info JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE customers ADD COLUMN IF NOT EXISTS updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP NOT NULL;

CREATE INDEX IF NOT EXISTS idx_customers_type ON customers(customer_type);
CREATE INDEX IF NOT EXISTS idx_customers_status ON customers(status);
CREATE INDEX IF NOT EXISTS idx_customers_risk_level ON customers(risk_level);

CREATE TABLE IF NOT EXISTS customer_relationships (
    id VARCHAR(64) PRIMARY KEY,
    customer_id VARCHAR(255) NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    related_customer_id VARCHAR(255) NOT NULL REFERENCES customers(id),
    relation_type VARCHAR(64) NOT NULL,
    relation_code VARCHAR(64) NOT NULL,
    reciprocal_relation_code VARCHAR(64) NOT NULL,
    status VARCHAR(50) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP NOT NULL,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP NOT NULL,
    UNIQUE(customer_id, related_customer_id, relation_code)
);

CREATE INDEX IF NOT EXISTS idx_customer_relationships_customer ON customer_relationships(customer_id);

-- +goose Down
DROP TABLE IF EXISTS customer_relationships;
DROP INDEX IF EXISTS idx_customers_risk_level;
DROP INDEX IF EXISTS idx_customers_status;
DROP INDEX IF EXISTS idx_customers_type;
ALTER TABLE customers DROP COLUMN IF EXISTS updated_at;
ALTER TABLE customers DROP COLUMN IF EXISTS extended_info;
ALTER TABLE customers DROP COLUMN IF EXISTS business_info;
ALTER TABLE customers DROP COLUMN IF EXISTS personal_info;
ALTER TABLE customers DROP COLUMN IF EXISTS general_info;
ALTER TABLE customers DROP COLUMN IF EXISTS risk_level;
ALTER TABLE customers DROP COLUMN IF EXISTS customer_rank;
ALTER TABLE customers DROP COLUMN IF EXISTS segment;
ALTER TABLE customers DROP COLUMN IF EXISTS address;
ALTER TABLE customers DROP COLUMN IF EXISTS identity_no;
ALTER TABLE customers DROP COLUMN IF EXISTS mobile;
ALTER TABLE customers DROP COLUMN IF EXISTS customer_type;
