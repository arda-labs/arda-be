-- +goose Up
-- The CNPG PostgreSQL 18 image exposes pgvector 0.8.1. Enable the extension
-- in the service-owned database, but leave the embedding column/index pending
-- until a provider, dimension, and retrieval benchmark are approved.
CREATE EXTENSION IF NOT EXISTS vector;

-- +goose Down
-- This is intentionally forward-only in production. Dropping an extension can
-- invalidate future vector columns/indexes and is not a safe rollback action.
SELECT 1;
