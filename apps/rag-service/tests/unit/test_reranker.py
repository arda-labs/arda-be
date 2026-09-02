"""P3.2 unit tests — reranker protocol, none + anthropic with fallback."""

import json
import re

import pytest

from app.config import RerankerSettings
from app.rag.retriever import Candidate
from app.rag.reranker import (
    AnthropicReranker,
    NoneReranker,
    build_reranker,
)

DEFAULT = RerankerSettings()  # provider=none, model=claude-haiku-4-5-20251001, timeout_ms=1500


def _candidates(n: int) -> list[Candidate]:
    return [
        Candidate(
            chunk_id=f"c{i}",
            source_id=1,
            source_version_id=1,
            version="v1",
            title=f"title {i}",
            heading="",
            content=f"content {i}",
            score=float(n - i),  # already RRF-ordered best-first
            source="both",
        )
        for i in range(n)
    ]


class _FakeAnthropicClient:
    """Records the messages payload; returns a configured score JSON."""

    def __init__(self, scores: list[float]) -> None:
        self.scores = scores
        self.payloads: list[dict] = []

    def __call__(self, messages: dict) -> str:
        self.payloads.append(messages)
        return json.dumps(self.scores)


class _RaisingClient:
    def __call__(self, messages: dict) -> str:
        raise RuntimeError("upstream is on fire")


def _anthropic(scores: list[float]) -> tuple[AnthropicReranker, _FakeAnthropicClient]:
    client = _FakeAnthropicClient(scores)
    return AnthropicReranker(DEFAULT, client=client), client


def test_none_reranker_slices_and_preserves_order():
    c = _candidates(5)
    top = NoneReranker().rerank("query", c, top_n=3)
    assert top == c[:3]


def test_none_reranker_top_n_larger_than_input():
    c = _candidates(2)
    assert NoneReranker().rerank("q", c, top_n=10) == c


def test_anthropic_rerank_reorders_by_score():
    c = _candidates(3)  # order c0, c1, c2
    reranker, client = _anthropic([1.0, 9.0, 5.0])
    out = reranker.rerank("q", c, top_n=3)
    assert out == [c[1], c[2], c[0]]
    assert reranker.rerank("q", c, top_n=2) == [c[1], c[2]]


def test_anthropic_rerank_does_not_mutate_input():
    c = _candidates(3)
    original = list(c)
    reranker, _ = _anthropic([1.0, 9.0, 5.0])
    reranker.rerank("q", c, top_n=3)
    assert c == original


def test_anthropic_prompt_contains_query_and_model():
    c = _candidates(2)
    reranker, client = _anthropic([1.0, 1.0])
    reranker.rerank("ngày nghỉ phép", c, top_n=2)
    payload = client.payloads[0]
    content = payload["messages"][0]["content"]
    assert payload["model"] == "claude-haiku-4-5-20251001"
    assert "ngày nghỉ phép" in content
    assert "title 0" in content and "title 1" in content


def test_anthropic_fallback_on_client_exception():
    c = _candidates(4)
    reranker = AnthropicReranker(DEFAULT, client=_RaisingClient())
    assert reranker.rerank("q", c, top_n=3) == c[:3]


def test_anthropic_fallback_on_unparseable_scores():
    c = _candidates(3)
    for bad in ["not json", "42", "[1, 2]", "[1.0, 2.0, 3.0, 4.0]", "[true, 2.0, 3.0]"]:
        client = _FakeAnthropicClient.__new__(_FakeAnthropicClient)
        client.scores = []
        client.payloads = []
        client.__call__ = lambda messages, s=bad: s
        reranker = AnthropicReranker(DEFAULT, client=client)
        assert reranker.rerank("q", c, top_n=3) == c[:3], f"expected fallback for {bad!r}"


def test_build_reranker_provider_none():
    rr = build_reranker(RerankerSettings(provider="none"))
    assert isinstance(rr, NoneReranker)


def test_build_reranker_provider_anthropic(monkeypatch):
    monkeypatch.setenv("ANTHROPIC_API_KEY", "k")
    rr = build_reranker(RerankerSettings(provider="anthropic"))
    assert isinstance(rr, AnthropicReranker)


def test_build_reranker_unknown_provider_returns_none():
    assert build_reranker(RerankerSettings(provider="cohere")) is None


def test_real_constructor_sets_timeout(monkeypatch):
    """Real path builds anthropic.Anthropic with timeout from settings."""
    import anthropic as anthropic_mod

    captured = {}
    monkeypatch.setenv("ANTHROPIC_API_KEY", "k")

    def fake_init(self, **kwargs):
        captured.update(kwargs)

    monkeypatch.setattr(anthropic_mod.Anthropic, "__init__", fake_init)
    AnthropicReranker(RerankerSettings(timeout_ms=2500, provider="anthropic"))
    assert captured["timeout"] == 2.5
