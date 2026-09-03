#!/usr/bin/env python3
"""P5.1: offline RAGAS eval harness over ``ai_rag_eval``-shaped datasets.

Runs a question set (scripts/eval_set.json) against a DEPLOYED rag-service
POST /api/rag/query, then scores retrieval + answer quality with RAGAS when
the library is installed. Run manually against the deployed service:

    RAG_SERVICE_URL=http://localhost:8082 RAG_AUTH_SECRET=... \\
        python scripts/eval_ragas.py

Never invoked by CI (ragas is not a project dependency); dataset parsing and
validation work without it and are covered by tests/unit/test_eval_parsing.py.

Domain of each sample is read from its ``tags`` (hrm/crm/finance/system); the
report is grouped per domain plus overall. Thresholds are flags, NOT
hard-coded (spec §8.3) — the P4 switch gate consumes calibrated values
(--min-faithfulness / --min-context-recall, default 0 = disabled).

Exit codes:
    0  ran OK — report printed (even if below thresholds)
    1  fatal error — dataset missing/malformed, API unreachable,
       missing optional deps
    2  ran OK but below a user-passed threshold flag

Run as ``python scripts/eval_ragas.py`` — the script imports this file's
module by path, NOT as ``app.*``, so it works in any venv without the
package installed. ``app.domain.security`` is a pure-stdlib module and is
imported lazily for signing only.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import hmac
import json
import logging
import os
import statistics
import sys
import time
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from pathlib import Path
from typing import Any

logger = logging.getLogger("eval_ragas")

SCRIPT_DIR = Path(__file__).resolve().parent
BASE_DIR = SCRIPT_DIR.parent  # apps/rag-service

MANDATORY_KEYS = ("query", "expected_answer")
OPTIONAL_KEYS = ("tenant_id", "tags")

# First tag in the list is the domain — one of the knowledge domains in the
# eval dataset; anything else is grouped under "other".
KNOWN_DOMAINS = ("hrm", "crm", "finance", "system")

RAGAS_METRICS = ("faithfulness", "context_precision", "context_recall", "answer_relevancy")


class DatasetError(ValueError):
    """Malformed eval dataset (schema or content)."""


# --------------------------------------------------------------------------
# Dataset parsing + validation — the only part covered by CI tests.
# --------------------------------------------------------------------------

@dataclass(frozen=True)
class EvalItem:
    """One question; mirrors one row of ai_rag_eval (without id/created_at)."""

    query: str
    expected_answer: str
    tenant_id: str | None = None
    tags: tuple[str, ...] = ()

    @property
    def domain(self) -> str:
        """First tag that is a known domain, else 'other'."""
        for tag in self.tags:
            if tag in KNOWN_DOMAINS:
                return tag
        return "other"


@dataclass(frozen=True)
class EvalDataset:
    items: tuple[EvalItem, ...]

    def domains(self) -> list[str]:
        return sorted({item.domain for item in self.items})


def parse_dataset(path: Path) -> EvalDataset:
    """Parse and schema-validate ``path`` (a JSON array of ai_rag_eval rows).

    Raises DatasetError on malformed JSON, non-list roots, and rows that
    violate the schema below. Schema mirrors migration
    20260903090001_rag_eval.sql:
      query           TEXT NOT NULL
      expected_answer TEXT NOT NULL
      tenant_id       TEXT (optional)
      tags            TEXT[] (optional)
    """
    if not path.is_file():
        raise DatasetError(f"eval dataset not found: {path}")
    try:
        with path.open(encoding="utf-8") as fh:
            raw = json.load(fh)
    except json.JSONDecodeError as exc:
        raise DatasetError(f"{path}: invalid JSON: {exc}") from None
    if not isinstance(raw, list):
        raise DatasetError(f"{path}: top level must be a JSON array of questions, got {type(raw).__name__}")
    return EvalDataset(tuple(_parse_row(path, row, i) for i, row in enumerate(raw)))


def _parse_row(path: Path, row: Any, index: int) -> EvalItem:
    where = f"{path}: row {index}"
    if not isinstance(row, dict):
        raise DatasetError(f"{where}: expected an object, got {type(row).__name__}")
    for key in MANDATORY_KEYS:
        if key not in row:
            raise DatasetError(f"{where}: missing required key {key!r}")
    query = row["query"]
    expected = row["expected_answer"]
    if not isinstance(query, str) or not query.strip():
        raise DatasetError(f"{where}: 'query' must be a non-empty string")
    if not isinstance(expected, str) or not expected.strip():
        raise DatasetError(f"{where}: 'expected_answer' must be a non-empty string")
    for key in row:
        if key not in MANDATORY_KEYS and key not in OPTIONAL_KEYS:
            raise DatasetError(f"{where}: unknown key {key!r}")

    tenant_id = row.get("tenant_id")
    if tenant_id is not None and (not isinstance(tenant_id, str) or not tenant_id.strip()):
        raise DatasetError(f"{where}: 'tenant_id' must be a non-empty string or null")
    tags = row.get("tags")
    if tags is None:
        tags = ()
    elif isinstance(tags, list) and all(isinstance(t, str) for t in tags):
        tags = tuple(t.strip() for t in tags if t.strip())
    else:
        raise DatasetError(f"{where}: 'tags' must be an array of strings")
    return EvalItem(
        query=query.strip(),
        expected_answer=expected.strip(),
        tenant_id=tenant_id,
        tags=tags,
    )


# --------------------------------------------------------------------------
# x-service-auth signing — reuses the service's own stdlib crypto contract
# (app.domain.security.verify_service_token is the verify side; the token
# shape here mirrors what Go identity.go issues).
# --------------------------------------------------------------------------

@dataclass(frozen=True)
class SignedToken:
    """Frozen dataclass so dataclasses.asdict() can rebuild a claim dict."""

    v: str
    src: str
    aud: str
    iat: int
    exp: int
    nonce: str


def build_service_token(secret: str, source: str, audience: str, ttl_seconds: int = 120) -> str:
    """Build a v1.{claims}.{hmac-sha256} token accepted by rag-service.

    Mirrors the exact claim structure + hmac construction that
    app.domain.security.verify_service_token expects and that the Go
    identity.go contract issues (see tests/unit/test_security.py).
    """
    now = int(time.time())
    nonce = base64.urlsafe_b64encode(os.urandom(12)).rstrip(b"=").decode()
    claims = SignedToken(
        v="v1",
        src=source,
        aud=audience,
        iat=now - 5,
        exp=now + ttl_seconds,
        nonce=nonce,
    )
    payload = base64.urlsafe_b64encode(
        json.dumps(claims.__dict__, separators=(",", ":")).encode()
    ).rstrip(b"=").decode()
    signing = hmac.new(secret.encode(), f"v1.{payload}".encode(), hashlib.sha256).digest()
    sig = base64.urlsafe_b64encode(signing).rstrip(b"=").decode()
    return f"v1.{payload}.{sig}"


# --------------------------------------------------------------------------
# Rag-service HTTP client (stdlib urllib — httpx is dev-only).
# --------------------------------------------------------------------------

class RagApiError(RuntimeError):
    """rag-service call failed at HTTP or transport level."""


def query_service(base_url: str, auth_secret: str, query: str, top_k: int = 3,
                  tenant_id: str | None = None) -> dict[str, Any]:
    """POST {base_url}/api/rag/query with a signed x-service-auth header.

    Returns the response JSON (run_id, hits[], ...). Raises RagApiError on
    transport failure or a non-2xx status.
    """
    endpoint = f"{base_url.rstrip('/')}/api/rag/query"
    body = json.dumps({"query": query, "top_k": top_k}).encode()
    headers = {
        "Content-Type": "application/json",
        "x-service-auth": build_service_token(auth_secret, "ai-service", "rag-service"),
        "Accept": "application/json",
    }
    if tenant_id:
        headers["X-Tenant-Id"] = tenant_id
    request = urllib.request.Request(endpoint, data=body, headers=headers, method="POST")
    try:
        with urllib.request.urlopen(request, timeout=60) as response:
            payload = json.loads(response.read().decode())
    except (urllib.error.HTTPError, urllib.error.URLError, TimeoutError) as exc:
        raise RagApiError(f"POST {endpoint} failed: {exc}") from None
    if not isinstance(payload, dict):
        raise RagApiError(f"POST {endpoint}: unexpected response shape: {type(payload).__name__}")
    return payload


# --------------------------------------------------------------------------
# RAGAS metrics — lazy import; dataset parsing must work without it.
# --------------------------------------------------------------------------

class RagasUnavailable(Exception):
    """ragas or its judge-LLM wiring is not importable."""


def compute_ragas_metrics(rows: list[dict[str, Any]]) -> dict[str, float]:
    """Score rows with the ragas library.

    Each row must carry: question, ground_truth, retrieved_contexts (list of
    chunk strings), answer. ragas is NOT a project dependency — import it
    lazily so parsing/validation and plain retrieval runs work without it;
    the judge LLM used by ragas is whatever the environment configures
    (see install guidance in the error).
    """
    try:
        from datasets import Dataset  # type: ignore
        from ragas import evaluate  # type: ignore
        from ragas.metrics import (  # type: ignore
            answer_relevancy,
            context_precision,
            context_recall,
            faithfulness,
        )
    except ImportError as exc:
        raise RagasUnavailable(
            "ragas is not installed. Install it in a dedicated venv (never in "
            "rag-service runtime deps):\n"
            "  pip install ragas langchain-anthropic\n"
            "and export the judge-LLM credentials ragas expects (e.g. "
            "ANTHROPIC_API_KEY). Then rerun."
        ) from exc
    try:
        result = evaluate(
            Dataset.from_list(rows),
            metrics=[faithfulness, context_precision, context_recall, answer_relevancy],
        )
    except Exception as exc:
        raise RagasUnavailable(
            f"ragas evaluation failed: {exc}\n"
            "Check the judge-LLM credentials/endpoint configured for ragas."
        ) from exc
    return {name: float(result[name]) for name in RAGAS_METRICS}


# --------------------------------------------------------------------------
# Report + threshold gate.
# --------------------------------------------------------------------------

@dataclass
class RunStats:
    """Aggregated row results, keyed by metric."""

    rows: list[dict[str, Any]] = field(default_factory=list)
    api_errors: int = 0
    per_domain: dict[str, list[dict[str, Any]]] = field(default_factory=lambda: {"overall": []})

    def add(self, row: dict[str, Any]) -> None:
        self.rows.append(row)
        self.per_domain.setdefault(row["domain"], []).append(row)
        self.per_domain["overall"].append(row)


def _mean(values: list[float]) -> float | None:
    return statistics.fmean(values) if values else None


def _fmt(value: float | None) -> str:
    return f"{value:.3f}" if value is not None else "  n/a"


def _gate_rows(rows: list[dict[str, Any]], min_faithfulness: float, min_context_recall: float) -> list[dict[str, Any]]:
    """Rows that miss a user-set threshold (0 = gate disabled)."""
    failures = []
    for row in rows:
        score = row.get("scores") or {}
        faithfulness = score.get("faithfulness")
        context_recall = score.get("context_recall")
        if min_faithfulness > 0 and (faithfulness is None or faithfulness < min_faithfulness):
            failures.append(row)
        elif min_context_recall > 0 and (context_recall is None or context_recall < min_context_recall):
            failures.append(row)
    return failures


def print_report(stats: RunStats, min_faithfulness: float = 0.0, min_context_recall: float = 0.0) -> int:
    """Print the console report; returns exit code 0 or 2 (below threshold)."""
    metrics = RAGAS_METRICS
    header = f"{'group':<10}" + "".join(f"{m:>20}" for m in metrics) + f"{'count':>8}"
    lines = [header, "-" * len(header)]

    per_domain = stats.per_domain
    for domain in sorted(k for k in per_domain if k != "overall"):
        lines.append(_group_line(domain, per_domain[domain], metrics))
    lines.append(_group_line("overall", per_domain.get("overall", []), metrics))
    lines.append("")

    gates = []
    for flag, metric in (("--min-faithfulness", "faithfulness"), ("--min-context-recall", "context_recall")):
        minimum = min_faithfulness if metric == "faithfulness" else min_context_recall
        if minimum > 0:
            gates.append(f"{flag} {minimum:.3f} on {metric}")
    if gates:
        lines.append(f"thresholds: {'; '.join(gates)}")
    lines.append("")
    lines.append(f"api errors (rows without contexts): {stats.api_errors}")
    print("\n".join(lines))

    if min_faithfulness <= 0 and min_context_recall <= 0:
        return 0
    failures = _gate_rows(stats.rows, min_faithfulness, min_context_recall)
    if failures:
        logger.warning("%d/%d rows below the configured thresholds", len(failures), len(stats.rows))
        return 2
    return 0


def _group_line(name: str, rows: list[dict[str, Any]], metrics: tuple[str, ...]) -> str:
    scores = {
        metric: [row["scores"][metric] for row in rows if row["scores"].get(metric) is not None]
        for metric in metrics
    }
    return f"{name:<10}" + "".join(f"{_fmt(_mean(scores[m])):>20}" for m in metrics) + f"{len(rows):>8}"


# --------------------------------------------------------------------------
# Orchestration.
# --------------------------------------------------------------------------

def run_eval(dataset: EvalDataset, args: argparse.Namespace, make_answer: Any) -> tuple[RunStats, list[str], list[str]]:
    """Run every question through the service + answer composer.

    make_answer(question, contexts) -> (answer, model_id) — injectable so the
    anthropic-based composer can be unit-testable without a key. Returns
    (stats, warnings, errors).
    """
    logger.info("evaluating %d questions against %s", len(dataset.items), args.base_url)
    stats = RunStats()
    warnings: list[str] = []
    errors: list[str] = []

    to_run = list(dataset.items)
    if args.limit > 0:
        to_run = to_run[: args.limit]
    for item in to_run:
        row: dict[str, Any] = {
            "question": item.query,
            "ground_truth": item.expected_answer,
            "domain": item.domain,
            "tenant_id": item.tenant_id,
            "tags": list(item.tags),
            "retrieved_contexts": [],
            "answer": "",
            "scores": {},
            "run_id": None,
            "retrieved_count": None,
            "latency_ms": None,
        }
        try:
            response = query_service(
                args.base_url, args.auth_secret, item.query, top_k=args.top_k,
                tenant_id=item.tenant_id,
            )
            row["run_id"] = response.get("run_id")
            row["retrieved_count"] = response.get("retrieved_count")
            row["latency_ms"] = response.get("latency_ms")
            contexts = [h["content"] for h in response.get("hits", []) if isinstance(h, dict) and h.get("content")]
            row["retrieved_contexts"] = contexts
        except RagApiError as exc:
            errors.append(f"  {item.domain}: {item.query[:60]}... -> {exc}")
            row["error"] = str(exc)
            stats.api_errors += 1
            stats.add(row)
            if args.verbose:
                logger.warning("query failed for %r: %s", item.query[:60], exc)
            continue
        if not row["retrieved_contexts"]:
            warnings.append(f"  {item.domain}: no contexts retrieved for {item.query[:60]}... (0 hits -> all context metrics 0)")
            if args.verbose:
                logger.warning("no contexts retrieved for %r", item.query[:60])
        try:
            answer, model = make_answer(item.query, row["retrieved_contexts"])
        except Exception as exc:
            raise RagasUnavailable(f"answer composition via the judge LLM failed: {exc}") from exc
        row["answer"] = answer
        row["model"] = model
        stats.add(row)
        if args.verbose:
            logger.info("answered %r with %d contexts (model=%s)", item.query[:60], len(row["retrieved_contexts"]), model)
    return stats, warnings, errors


def _main(argv: list[str]) -> int:
    try:  # console may default to cp1252 — make Vietnamese output lossless
        sys.stdout.reconfigure(encoding="utf-8")
        sys.stderr.reconfigure(encoding="utf-8")
    except (AttributeError, OSError):
        pass
    parser = argparse.ArgumentParser(
        description="Offline RAGAS eval over ai_rag_eval-shaped questions against a deployed rag-service.",
    )
    parser.add_argument("--base-url", default=os.environ.get("RAG_SERVICE_URL", "http://localhost:8000"),
                        help="deployed rag-service base URL (env RAG_SERVICE_URL)")
    parser.add_argument("--auth-secret", default=os.environ.get("RAG_AUTH_SECRET", ""),
                        help="shared service-auth secret (env RAG_AUTH_SECRET)")
    parser.add_argument("--dataset", default=str(SCRIPT_DIR / "eval_set.json"),
                        help="eval dataset JSON (default scripts/eval_set.json)")
    parser.add_argument("--limit", type=int, default=0,
                        help="max questions to run, 0 = all (default 0)")
    parser.add_argument("--min-faithfulness", type=float, default=0.0,
                        help="exit 2 below this faithfulness; 0 = disabled (default 0)")
    parser.add_argument("--min-context-recall", type=float, default=0.0,
                        help="exit 2 below this context_recall; 0 = disabled (default 0)")
    parser.add_argument("--top-k", type=int, default=3, help="top_k sent to /api/rag/query (default 3)")
    parser.add_argument("--model", default=os.environ.get("RAG_EVAL_MODEL", ""),
                        help="LLM judge + answer-composer model id (default: haiku-class from the reranker setting)")
    parser.add_argument("--verbose", action="store_true")
    args = parser.parse_args(argv)

    logging.basicConfig(level=logging.DEBUG if args.verbose else logging.WARNING,
                        format="%(levelname)s %(name)s: %(message)s")

    if not args.auth_secret:
        print("error: --auth-secret (or env RAG_AUTH_SECRET) is required — x-service-auth signing secret", file=sys.stderr)
        return 1
    if len(args.auth_secret) < 32:
        print("error: auth secret must be at least 32 characters (Go identity contract)", file=sys.stderr)
        return 1
    if not 0.0 <= args.min_faithfulness <= 1.0 or not 0.0 <= args.min_context_recall <= 1.0:
        print("error: threshold flags must be within 0.0..1.0", file=sys.stderr)
        return 1
    if args.limit < 0:
        print("error: --limit must be >= 0", file=sys.stderr)
        return 1

    try:
        dataset = parse_dataset(Path(args.dataset))
    except DatasetError as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    # Answer composition: ask the same LLM used for judging to write the
    # answer from the retrieved contexts (the deployed rag-service never
    # composes answers in Phase 1). Fail the run when no judge LLM is
    # configured — the report would otherwise be meaningless.
    api_key = os.environ.get("ANTHROPIC_API_KEY")
    if not api_key:
        print("error: ANTHROPIC_API_KEY is not set — no LLM available to compose answers and judge scores.", file=sys.stderr)
        print("  The eval set is valid and parsing works without it, but an eval run needs an LLM.", file=sys.stderr)
        return 1
    import anthropic

    model = args.model or "claude-haiku-4-5-20251001"
    client = anthropic.Anthropic(api_key=api_key)

    def make_answer(question: str, contexts: list[str]) -> tuple[str, str]:
        answer = _compose_answer(client, model, question, contexts)
        return answer, model

    try:
        stats, warnings, errors = run_eval(dataset, args, make_answer)
    except RagasUnavailable as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1

    # Metrics stage — only when ragas is importable; exit 1 otherwise.
    for row in stats.rows:
        if row["scores"] or row.get("error"):
            continue
        try:
            row["scores"] = compute_ragas_metrics([row]) or {}
        except RagasUnavailable as exc:
            print(f"error: {exc}", file=sys.stderr)
            print(f"  {len([r for r in stats.rows if not r.get('error')])} rows retrieved; re-run with ragas installed to score.", file=sys.stderr)
            return 1

    if warnings:
        print("\nwarnings:")
        print("\n".join(warnings))
    if errors:
        print("\nerrors:")
        print("\n".join(errors))
    print()
    if stats.api_errors == len(stats.rows):
        print("error: no question reached the service — check --base-url and connectivity.", file=sys.stderr)
        return 1
    return print_report(stats, args.min_faithfulness, args.min_context_recall)


def _compose_answer(client: Any, model: str, question: str, contexts: list[str]) -> str:
    """Ask the judge LLM to answer `question` from `contexts` only."""
    listing = "\n\n".join(f"[{i}] {c}" for i, c in enumerate(contexts, start=1))
    system = (
        "Bạn là trợ lý RAG của nền tảng Arda. Trả lời câu hỏi của người dùng "
        "CHỈ dựa trên các đoạn ngữ cảnh được cung cấp. Nếu ngữ cảnh không đủ "
        "thông tin để trả lời, hãy nói rõ điều đó. Không thêm kiến thức ngoài "
        "ngữ cảnh. Trả lời bằng tiếng Việt, gọn gàng, chính xác."
    )
    user = f"Ngữ cảnh:\n{listing}\n\nCâu hỏi: {question}"
    message = client.messages.create(
        model=model,
        max_tokens=1024,
        system=system,
        messages=[{"role": "user", "content": user}],
    )
    return "".join(block.text for block in message.content if getattr(block, "type", "") == "text").strip()


if __name__ == "__main__":
    sys.exit(_main(sys.argv[1:]))
