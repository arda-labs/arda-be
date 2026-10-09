-- +goose Up
CREATE TABLE doc_series (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64) NOT NULL,
    org_code TEXT NOT NULL DEFAULT '',
    document_type TEXT NOT NULL,
    version INTEGER NOT NULL CHECK (version > 0),
    effective_from DATE NOT NULL,
    effective_to DATE,
    reset_period TEXT NOT NULL CHECK (reset_period IN ('NONE','YEAR','MONTH','DAY')),
    pattern TEXT NOT NULL,
    sequence_start BIGINT NOT NULL DEFAULT 1 CHECK (sequence_start > 0),
    sequence_width SMALLINT NOT NULL CHECK (sequence_width BETWEEN 1 AND 18),
    overflow_policy TEXT NOT NULL DEFAULT 'REJECT' CHECK (overflow_policy = 'REJECT'),
    status TEXT NOT NULL DEFAULT 'DRAFT' CHECK (status IN ('DRAFT','ACTIVE','RETIRED')),
    created_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CHECK (effective_to IS NULL OR effective_to >= effective_from),
    CHECK (regexp_count(pattern, '[{]SEQ:[1-9][0-9]?[}]') = 1),
    CHECK (pattern ~ ('[{]SEQ:' || sequence_width::TEXT || '[}]')),
    CHECK (
        (reset_period = 'NONE' AND pattern !~ '[{]YYYY[}]' AND pattern !~ '[{]MM[}]' AND pattern !~ '[{]DD[}]') OR
        (reset_period = 'YEAR' AND pattern ~ '[{]YYYY[}]' AND pattern !~ '[{]MM[}]' AND pattern !~ '[{]DD[}]') OR
        (reset_period = 'MONTH' AND pattern ~ '[{]YYYY[}]' AND pattern ~ '[{]MM[}]' AND pattern !~ '[{]DD[}]') OR
        (reset_period = 'DAY' AND pattern ~ '[{]YYYY[}]' AND pattern ~ '[{]MM[}]' AND pattern ~ '[{]DD[}]')
    ),
    UNIQUE (tenant_id, id),
    UNIQUE (tenant_id, document_type, org_code, version)
);
CREATE TABLE doc_counter (
    series_id UUID NOT NULL REFERENCES doc_series(id),
    period_key TEXT NOT NULL,
    last_value BIGINT NOT NULL CHECK (last_value > 0),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (series_id, period_key)
);

CREATE TABLE document_number (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64) NOT NULL,
    document_id TEXT NOT NULL,
    series_id UUID NOT NULL REFERENCES doc_series(id),
    display_no TEXT NOT NULL,
    period_key TEXT NOT NULL,
    sequence_no BIGINT NOT NULL CHECK (sequence_no > 0),
    business_date DATE NOT NULL,
    status TEXT NOT NULL DEFAULT 'ACTIVE' CHECK (status IN ('ACTIVE','VOID')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, document_id),
    UNIQUE (tenant_id, display_no),
    UNIQUE (series_id, period_key, sequence_no)
);
CREATE INDEX idx_document_number_series ON document_number (tenant_id, series_id, business_date);

-- One namespace reserves active and retired display numbers alike, preventing
-- reuse across series and across renumber operations.
CREATE TABLE doc_number_alias (
    tenant_id VARCHAR(64) NOT NULL,
    display_no TEXT NOT NULL,
    document_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('ACTIVE','RETIRED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (tenant_id, display_no)
);

CREATE TABLE doc_gap (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64) NOT NULL,
    series_id UUID NOT NULL REFERENCES doc_series(id),
    period_key TEXT NOT NULL,
    sequence_no BIGINT NOT NULL,
    reason TEXT NOT NULL,
    created_by TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (series_id, period_key, sequence_no)
);

CREATE TABLE retired_no (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64) NOT NULL,
    display_no TEXT NOT NULL,
    document_id TEXT NOT NULL,
    retired_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, display_no)
);

CREATE TABLE doc_number_change (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64) NOT NULL,
    document_id TEXT NOT NULL,
    previous_display_no TEXT NOT NULL,
    requested_display_no TEXT NOT NULL,
    reason TEXT NOT NULL,
    maker_user_id TEXT NOT NULL,
    checker_user_id TEXT,
    status TEXT NOT NULL DEFAULT 'PENDING' CHECK (status IN ('PENDING','APPROVED','REJECTED')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    checked_at TIMESTAMPTZ,
    CHECK (checker_user_id IS NULL OR checker_user_id <> maker_user_id)
);
CREATE INDEX idx_doc_number_change_pending ON doc_number_change (tenant_id, created_at) WHERE status = 'PENDING';
CREATE UNIQUE INDEX uq_doc_number_change_one_pending
    ON doc_number_change (tenant_id, document_id)
    WHERE status = 'PENDING';

CREATE TABLE doc_type_rule (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    tenant_id VARCHAR(64) NOT NULL,
    org_code TEXT NOT NULL DEFAULT '',
    ledger TEXT NOT NULL,
    payment_method TEXT NOT NULL,
    entry_direction TEXT NOT NULL,
    document_type TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 100,
    is_active BOOLEAN NOT NULL DEFAULT true,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (tenant_id, org_code, ledger, payment_method, entry_direction, priority)
);

-- +goose Down
DROP TABLE doc_type_rule;
DROP TABLE doc_number_change;
DROP TABLE retired_no;
DROP TABLE doc_number_alias;
DROP TABLE doc_gap;
DROP TABLE document_number;
DROP TABLE doc_counter;
DROP TABLE doc_series;
