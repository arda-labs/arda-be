"""CI-safe tests: eval dataset schema parsing/validation only.

No RAGAS or API calls — these run offline without any external dependency
or database. They validate the ``parse_dataset`` function in
scripts/eval_ragas.
"""
from __future__ import annotations

import base64
import json
import sys
import time
from pathlib import Path

import pytest

# ensure scripts/ is importable (plain dir — PEP 420 namespace package)
_SCRIPTS = Path(__file__).resolve().parent.parent.parent / "scripts"
if str(_SCRIPTS) not in sys.path:
    sys.path.insert(0, str(_SCRIPTS))

from app.domain import security  # noqa: E402
from scripts.eval_ragas import DatasetError, EvalDataset, EvalItem, build_service_token, parse_dataset  # noqa: E402


# ---------------------------------------------------------------------------
# Fixtures
# ---------------------------------------------------------------------------

@pytest.fixture()
def valid_rows() -> list[dict]:
    return [
        {"query": "Question 1?", "expected_answer": "Answer 1", "tenant_id": "t1", "tags": ["hrm"]},
        {"query": "Question 2?", "expected_answer": "Answer 2"},
        {"query": "Question 3?", "expected_answer": "Answer 3", "tags": ["crm", "my-domain"]},
    ]


@pytest.fixture()
def valid_json(tmp_path: Path, valid_rows: list[dict]) -> Path:
    path = tmp_path / "valid.json"
    path.write_text(json.dumps(valid_rows, ensure_ascii=False), encoding="utf-8")
    return path


# ---------------------------------------------------------------------------
# Happy path
# ---------------------------------------------------------------------------

class TestParseDataset:
    def test_valid_dataset(self, valid_json: Path, valid_rows: list[dict]) -> None:
        ds = parse_dataset(valid_json)
        assert isinstance(ds, EvalDataset)
        assert len(ds.items) == len(valid_rows)

    def test_returns_eval_items(self, valid_json: Path) -> None:
        ds = parse_dataset(valid_json)
        for item in ds.items:
            assert isinstance(item, EvalItem)
            assert isinstance(item.query, str)
            assert isinstance(item.expected_answer, str)

    def test_optional_fields_default(self, valid_json: Path) -> None:
        ds = parse_dataset(valid_json)
        # second row has no tenant_id and no tags
        row = ds.items[1]
        assert row.tenant_id is None
        assert row.tags == ()

    def test_tenant_id_present(self, valid_json: Path) -> None:
        ds = parse_dataset(valid_json)
        assert ds.items[0].tenant_id == "t1"

    def test_tags_present(self, valid_json: Path) -> None:
        ds = parse_dataset(valid_json)
        assert ds.items[0].tags == ("hrm",)
        assert ds.items[2].tags == ("crm", "my-domain")

    def test_domain_from_first_known_tag(self, valid_json: Path) -> None:
        ds = parse_dataset(valid_json)
        assert ds.items[0].domain == "hrm"
        assert ds.items[1].domain == "other"   # no tags
        assert ds.items[2].domain == "crm"     # first known tag

    def test_domains(self, valid_json: Path) -> None:
        ds = parse_dataset(valid_json)
        assert ds.domains() == ["crm", "hrm", "other"]  # row 2 has no tags

    def test_shipped_dataset_parses(self) -> None:
        """The committed scripts/eval_set.json must stay schema-valid."""
        ds = parse_dataset(_SCRIPTS / "eval_set.json")
        assert len(ds.items) > 0
        assert all(item.domain in ("hrm", "crm", "finance", "system") for item in ds.items)


# ---------------------------------------------------------------------------
# Schema violations
# ---------------------------------------------------------------------------

class TestParseDatasetErrors:
    def test_file_not_found(self, tmp_path: Path) -> None:
        missing = tmp_path / "nope.json"
        with pytest.raises(DatasetError, match="not found"):
            parse_dataset(missing)

    def test_invalid_json(self, tmp_path: Path) -> None:
        p = tmp_path / "bad.json"
        p.write_text("not json", encoding="utf-8")
        with pytest.raises(DatasetError, match="invalid JSON"):
            parse_dataset(p)

    def test_root_not_list(self, tmp_path: Path) -> None:
        p = tmp_path / "obj.json"
        p.write_text('{"query": "q"}', encoding="utf-8")
        with pytest.raises(DatasetError, match="must be a JSON array"):
            parse_dataset(p)

    def test_row_not_dict(self, tmp_path: Path) -> None:
        p = tmp_path / "bad-row.json"
        p.write_text('["string", 42]', encoding="utf-8")
        with pytest.raises(DatasetError, match="expected an object"):
            parse_dataset(p)

    def test_missing_required_key(self, tmp_path: Path) -> None:
        p = tmp_path / "no-query.json"
        p.write_text('[{"expected_answer": "a"}]', encoding="utf-8")
        with pytest.raises(DatasetError, match="missing required key.*query"):
            parse_dataset(p)

    def test_empty_query_string(self, tmp_path: Path) -> None:
        p = tmp_path / "empty-query.json"
        p.write_text('[{"query": "", "expected_answer": "a"}]', encoding="utf-8")
        with pytest.raises(DatasetError, match="must be a non-empty string"):
            parse_dataset(p)

    def test_whitespace_only_query(self, tmp_path: Path) -> None:
        p = tmp_path / "ws-query.json"
        p.write_text('[{"query": "   ", "expected_answer": "a"}]', encoding="utf-8")
        with pytest.raises(DatasetError, match="must be a non-empty string"):
            parse_dataset(p)

    def test_empty_expected_answer(self, tmp_path: Path) -> None:
        p = tmp_path / "empty-answer.json"
        p.write_text('[{"query": "q", "expected_answer": ""}]', encoding="utf-8")
        with pytest.raises(DatasetError, match="must be a non-empty string"):
            parse_dataset(p)

    def test_unknown_key(self, tmp_path: Path) -> None:
        p = tmp_path / "unknown-key.json"
        p.write_text('[{"query": "q", "expected_answer": "a", "extra_key": 1}]', encoding="utf-8")
        with pytest.raises(DatasetError, match="unknown key"):
            parse_dataset(p)

    def test_tags_not_array(self, tmp_path: Path) -> None:
        p = tmp_path / "bad-tags.json"
        p.write_text('[{"query": "q", "expected_answer": "a", "tags": "hrm"}]', encoding="utf-8")
        with pytest.raises(DatasetError, match="must be an array"):
            parse_dataset(p)

    def test_tags_not_array_of_strings(self, tmp_path: Path) -> None:
        p = tmp_path / "bad-tags2.json"
        p.write_text('[{"query": "q", "expected_answer": "a", "tags": [1, 2]}]', encoding="utf-8")
        with pytest.raises(DatasetError, match="must be an array"):
            parse_dataset(p)

    def test_tenant_id_non_string(self, tmp_path: Path) -> None:
        p = tmp_path / "bad-tenant.json"
        p.write_text('[{"query": "q", "expected_answer": "a", "tenant_id": 42}]', encoding="utf-8")
        with pytest.raises(DatasetError, match="must be a non-empty string or null"):
            parse_dataset(p)

    def test_tenant_id_empty_string(self, tmp_path: Path) -> None:
        p = tmp_path / "empty-tenant.json"
        p.write_text('[{"query": "q", "expected_answer": "a", "tenant_id": ""}]', encoding="utf-8")
        with pytest.raises(DatasetError, match="must be a non-empty string or null"):
            parse_dataset(p)

    def test_empty_dataset(self, tmp_path: Path) -> None:
        p = tmp_path / "empty.json"
        p.write_text("[]", encoding="utf-8")
        ds = parse_dataset(p)
        assert len(ds.items) == 0

    def test_single_item(self, tmp_path: Path) -> None:
        p = tmp_path / "single.json"
        p.write_text('[{"query": "q", "expected_answer": "a"}]', encoding="utf-8")
        ds = parse_dataset(p)
        assert len(ds.items) == 1
        assert ds.items[0].query == "q"
        assert ds.items[0].expected_answer == "a"


# ---------------------------------------------------------------------------
# x-service-auth signing — CI-safe round trip with the service's verifier.
# ---------------------------------------------------------------------------

SECRET = "a" * 32  # >= 32 chars per Go contract


class TestBuildServiceToken:
    def test_round_trip_verifies(self) -> None:
        """build_service_token output must be accepted by verify_service_token."""
        token = build_service_token(SECRET, "ai-service", "rag-service")
        claims = security.verify_service_token(token, SECRET, "rag-service")
        assert claims == security.VerifiedClaims(source="ai-service", audience="rag-service")

    def test_expiry_in_ttl_window(self) -> None:
        token = build_service_token(SECRET, "ai-service", "rag-service", ttl_seconds=120)
        payload = token.split(".")[1]
        claims = json.loads(base64.urlsafe_b64decode(payload + "=" * (-len(payload) % 4)))
        now = time.time()
        assert claims["iat"] <= now + 60
        assert claims["exp"] >= now + 60
        assert claims["exp"] <= now + 120 + 60  # generous skew margin

    def test_wrong_secret_rejected(self) -> None:
        token = build_service_token(SECRET, "ai-service", "rag-service")
        with pytest.raises(security.AuthenticationError):
            security.verify_service_token(token, "b" * 32, "rag-service")
