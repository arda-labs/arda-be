"""P3.2: reranker abstraction — none + anthropic (spec §5.2).

`AnthropicReranker` NEVER fails the query: any LLM-client exception
(network, timeout, parsing, bad status) falls back to returning the
candidates in input order, truncated to top_n. That fallback is the
contract, not an afterthought.
"""

import json
import os
from typing import Callable, Protocol, TypeAlias

from app.config import RerankerSettings
from app.rag.retriever import Candidate

# A Messages-API callable (tests inject a fake). Bound to the constructor
# when the real client is built: `lambda msgs: client.messages.create(**msgs)`.
ClientCallable: TypeAlias = Callable[[dict], str]


class Reranker(Protocol):
    """Structural protocol — implementers satisfy it implicitly."""

    def rerank(self, query: str, candidates: list[Candidate], top_n: int) -> list[Candidate]: ...


class NoneReranker:
    """provider=none — no reranking, preserve input order."""

    def rerank(self, query: str, candidates: list[Candidate], top_n: int) -> list[Candidate]:
        return candidates[:top_n]


_PROMPT = (
    "You are a relevance scorer for a document retrieval system. "
    "Query: {query}\n\n"
    "Score each candidate 0-10 for relevance to the query, as a number. "
    "Higher = more relevant. Only output the scores as a JSON array of "
    "numbers, one per candidate in the order given. No prose.\n\n"
    "Candidates:\n{candidates}"
)


class AnthropicReranker:
    """provider=anthropic — Haiku scores candidates; sort desc, take top_n.

    The messages callable is injectable for tests; when it is, the caller
    owns the client (and its timeout). Otherwise a real
    `anthropic.Anthropic(timeout=settings.timeout_ms / 1000)` client is
    built from the API key in `settings.api_key_env`.
    """

    def __init__(self, settings: RerankerSettings, client: ClientCallable | None = None) -> None:
        self._settings = settings
        if client is None:
            import anthropic

            api_key = os.environ.get(settings.api_key_env, "")
            anthropic_client = anthropic.Anthropic(
                api_key=api_key, timeout=settings.timeout_ms / 1000
            )
            client = lambda msgs: anthropic_client.messages.create(**msgs).content[0].text
        self._client = client

    def rerank(self, query: str, candidates: list[Candidate], top_n: int) -> list[Candidate]:
        try:
            text = self._client(self._prompt_messages(query, candidates))
            scores = json.loads(text)
        except Exception:
            return candidates[:top_n]

        if not isinstance(scores, list) or len(scores) != len(candidates):
            return candidates[:top_n]
        scored = [
            (c, float(s))
            for c, s in zip(candidates, scores)
            if isinstance(s, (int, float)) and not isinstance(s, bool)
        ]
        if len(scored) != len(candidates):  # any non-numeric entry → trust nothing
            return candidates[:top_n]
        scored.sort(key=lambda cs: cs[1], reverse=True)
        return [c for c, _ in scored][:top_n]

    def _prompt_messages(self, query: str, candidates: list[Candidate]) -> dict:
        listing = "\n".join(
            f"{i}. {c.title} — {c.heading or '(no heading)'}\n   {c.content[:500]}"
            for i, c in enumerate(candidates, start=1)
        )
        return {
            "model": self._settings.model,
            "max_tokens": 256,
            "messages": [{"role": "user", "content": _PROMPT.format(query=query, candidates=listing)}],
        }


def build_reranker(settings: RerankerSettings) -> Reranker | None:
    if settings.provider == "none":
        return NoneReranker()
    if settings.provider == "anthropic":
        return AnthropicReranker(settings)
    return None
