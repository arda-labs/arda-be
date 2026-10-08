-- +goose Up

CREATE OR REPLACE FUNCTION fin_guard_journal_entry_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    mutable_columns TEXT[];
BEGIN
    IF TG_OP = 'DELETE' THEN
        IF OLD.status IN ('POSTED', 'REVERSED') THEN
            RAISE EXCEPTION 'posted journal entries are immutable'
                USING ERRCODE = '55000';
        END IF;
        RETURN OLD;
    END IF;

    IF OLD.status = 'PENDING' AND NEW.status = 'POSTED' THEN
        mutable_columns := ARRAY['status', 'posted_at', 'updated_by', 'updated_at'];
    ELSIF OLD.status = 'PENDING' AND NEW.status = 'VOID' THEN
        mutable_columns := ARRAY['status', 'void_reason', 'idempotency_key', 'updated_by', 'updated_at'];
    ELSIF OLD.status = 'POSTED' AND NEW.status = 'REVERSED'
          AND OLD.reversed_by_entry_id IS NULL AND NEW.reversed_by_entry_id IS NOT NULL THEN
        mutable_columns := ARRAY['status', 'reversed_by_entry_id', 'updated_at'];
    ELSE
        RAISE EXCEPTION 'invalid journal entry mutation from % to %', OLD.status, NEW.status
            USING ERRCODE = '55000';
    END IF;

    IF (to_jsonb(NEW) - mutable_columns) IS DISTINCT FROM (to_jsonb(OLD) - mutable_columns) THEN
        RAISE EXCEPTION 'journal entry transition changed immutable columns'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE OR REPLACE FUNCTION fin_guard_journal_line_mutation()
RETURNS trigger
LANGUAGE plpgsql
AS $$
DECLARE
    entry_status TEXT;
BEGIN
    SELECT status INTO entry_status
    FROM fin_journal_entries
    WHERE tenant_id = OLD.tenant_id AND id = OLD.entry_id;

    -- The parent is absent only during its ON DELETE CASCADE for a mutable
    -- PENDING/VOID entry; direct orphan rows remain prevented by the FK.
    IF NOT FOUND THEN
        IF TG_OP = 'DELETE' THEN
            RETURN OLD;
        END IF;
        RETURN NEW;
    END IF;

    IF entry_status <> 'PENDING' THEN
        RAISE EXCEPTION 'journal lines are immutable after posting begins'
            USING ERRCODE = '55000';
    END IF;
    IF TG_OP = 'DELETE' THEN
        RETURN OLD;
    END IF;
    IF NEW.tenant_id IS DISTINCT FROM OLD.tenant_id OR NEW.entry_id IS DISTINCT FROM OLD.entry_id THEN
        RAISE EXCEPTION 'journal line parent is immutable'
            USING ERRCODE = '55000';
    END IF;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_fin_journal_entries_immutable
BEFORE UPDATE OR DELETE ON fin_journal_entries
FOR EACH ROW EXECUTE FUNCTION fin_guard_journal_entry_mutation();

CREATE TRIGGER trg_fin_journal_lines_immutable
BEFORE UPDATE OR DELETE ON fin_journal_lines
FOR EACH ROW EXECUTE FUNCTION fin_guard_journal_line_mutation();

-- +goose Down
DROP TRIGGER IF EXISTS trg_fin_journal_lines_immutable ON fin_journal_lines;
DROP TRIGGER IF EXISTS trg_fin_journal_entries_immutable ON fin_journal_entries;
DROP FUNCTION IF EXISTS fin_guard_journal_line_mutation();
DROP FUNCTION IF EXISTS fin_guard_journal_entry_mutation();
