import os
from dataclasses import dataclass

from app.config import EmbeddingSettings


class EmbeddingError(Exception):
    pass


def _env(name: str) -> str:
    """Read env var; return "" when unset so missing keys read as not-configured."""
    return os.environ.get(name, "")


@dataclass
class Embedder:
    settings: EmbeddingSettings
    _client: object | None = None   # OpenAIEmbedding instance (lazy)

    @property
    def model(self) -> str:
        return self.settings.model

    @property
    def dimensions(self) -> int:
        return self.settings.dimensions

    def _client_impl(self):
        if self._client is None:
            from llama_index.embeddings.openai import OpenAIEmbedding

            api_key = _env(self.settings.api_key_env)
            # llama-index OpenAIEmbedding validates model against its own enum
            # (only OpenAI models). We pass a placeholder that passes the enum
            # check, then override model_name with the real provider model
            # (e.g. @cf/qwen/qwen3-embedding-0.6b) so the actual API payload
            # uses the correct model name.
            self._client = OpenAIEmbedding(
                model="text-embedding-ada-002",
                model_name=self.settings.model,
                api_key=api_key,
                api_base=self.settings.base_url or None,
                embed_batch_size=self.settings.batch_size,
            )
        return self._client

    def embed(self, texts: list[str]) -> list[list[float]]:
        if not texts:
            return []
        vectors = self._client_impl().get_text_embedding_batch(texts)
        for vector in vectors:
            if len(vector) != self.settings.dimensions:
                raise EmbeddingError(
                    f"Dimension mismatch: expected {self.settings.dimensions}, got {len(vector)}"
                )
        return vectors


def build_embedder(settings: EmbeddingSettings) -> Embedder | None:
    if not settings.base_url or not _env(settings.api_key_env):
        return None
    return Embedder(settings)
