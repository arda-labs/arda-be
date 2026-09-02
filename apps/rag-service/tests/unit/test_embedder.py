import os

import pytest

from app.rag.embedder import Embedder, EmbeddingError, build_embedder


class _FakeClient:
    """Deterministic fake: returns N vectors of dim `dim`."""

    def __init__(self, dim: int) -> None:
        self.dim = dim

    def get_text_embedding_batch(self, texts: list[str]) -> list[list[float]]:
        return [[0.1] * self.dim for _ in texts]


def _settings(dim: int = 1024, base_url: str = "http://localhost:9999/v1", key: str = "k"):
    from app.config import EmbeddingSettings

    os.environ["CF_WORKERS_AI_API_TOKEN"] = key
    return EmbeddingSettings(dimensions=dim, base_url=base_url)


def test_dimension_mismatch_raises(monkeypatch):
    emb = Embedder(_settings(dim=1024), _client=_FakeClient(1023))
    with pytest.raises(EmbeddingError, match="1024"):
        emb.embed(["a", "b"])


def test_correct_dimension_passes():
    emb = Embedder(_settings(dim=1024), _client=_FakeClient(1024))
    vecs = emb.embed(["a", "b", "c"])
    assert len(vecs) == 3
    assert all(len(v) == 1024 for v in vecs)


def test_empty_texts_returns_empty():
    emb = Embedder(_settings(), _client=_FakeClient(1024))
    assert emb.embed([]) == []


def test_model_and_dimensions_properties():
    emb = Embedder(_settings(dim=768), _client=_FakeClient(768))
    assert emb.dimensions == 768
    assert emb.model


def test_build_embedder_none_without_base_url():
    from app.config import EmbeddingSettings

    assert build_embedder(EmbeddingSettings(base_url="")) is None


def test_build_embedder_none_without_api_key():
    from app.config import EmbeddingSettings

    os.environ.pop("CF_WORKERS_AI_API_TOKEN", None)
    assert build_embedder(EmbeddingSettings(base_url="http://x")) is None


def test_build_embedder_returns_instance():
    from app.config import EmbeddingSettings

    os.environ["CF_WORKERS_AI_API_TOKEN"] = "k"
    emb = build_embedder(EmbeddingSettings(base_url="http://x"))
    assert emb is not None
    assert emb.dimensions == 1024
