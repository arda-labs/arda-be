-- +goose Up
-- PostgreSQL 18: uuidv7() generates time-ordered UUID v7 (better B-tree performance than uuid v4)

CREATE TABLE IF NOT EXISTS iam_organizations (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    code VARCHAR(64) UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS iam_users (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    external_subject VARCHAR(255) UNIQUE,
    username VARCHAR(128) UNIQUE NOT NULL,
    email VARCHAR(255) UNIQUE NOT NULL,
    display_name VARCHAR(255),
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    tenant_id VARCHAR(64) NOT NULL DEFAULT 'default',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS iam_user_organizations (
    user_id UUID NOT NULL REFERENCES iam_users(id) ON DELETE CASCADE,
    organization_id UUID NOT NULL REFERENCES iam_organizations(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, organization_id)
);

CREATE TABLE IF NOT EXISTS iam_roles (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    code VARCHAR(128) UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL,
    status VARCHAR(32) NOT NULL DEFAULT 'ACTIVE',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS iam_permissions (
    id UUID PRIMARY KEY DEFAULT uuidv7(),
    code VARCHAR(255) UNIQUE NOT NULL,
    name VARCHAR(255) NOT NULL,
    module_code VARCHAR(64) NOT NULL,
    resource_code VARCHAR(64) NOT NULL,
    operation_code VARCHAR(64) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS iam_user_roles (
    user_id UUID NOT NULL REFERENCES iam_users(id) ON DELETE CASCADE,
    role_id UUID NOT NULL REFERENCES iam_roles(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role_id)
);

CREATE TABLE IF NOT EXISTS iam_role_permissions (
    role_id UUID NOT NULL REFERENCES iam_roles(id) ON DELETE CASCADE,
    permission_id UUID NOT NULL REFERENCES iam_permissions(id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (role_id, permission_id)
);

-- +goose Down
DROP TABLE IF EXISTS iam_role_permissions CASCADE;
DROP TABLE IF EXISTS iam_user_roles CASCADE;
DROP TABLE IF EXISTS iam_user_organizations CASCADE;
DROP TABLE IF EXISTS iam_permissions CASCADE;
DROP TABLE IF EXISTS iam_roles CASCADE;
DROP TABLE IF EXISTS iam_users CASCADE;
DROP TABLE IF EXISTS iam_organizations CASCADE;
