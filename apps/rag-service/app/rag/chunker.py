import hashlib
import re
from dataclasses import dataclass


@dataclass(frozen=True)
class Chunk:
    heading: str
    content: str
    content_hash: str


def _sha256(text: str) -> str:
    return hashlib.sha256(text.encode("utf-8")).hexdigest()


def _words(text: str) -> list[str]:
    return re.findall(r"\S+", text, flags=re.UNICODE)


def chunk_markdown(markdown: str, *, chunk_size: int = 512,
                   chunk_overlap: int = 64, chunker_version: str = "1") -> list[Chunk]:
    """Heading-scoped chunker (Go indexer behavior) with overlap for oversized bodies."""
    chunks: list[Chunk] = []
    for heading, body in _split_by_headings(markdown):
        body = body.strip()
        if not body:
            continue
        for part in _split_with_overlap(body, chunk_size, chunk_overlap):
            chunks.append(Chunk(heading=heading.strip(), content=part, content_hash=_sha256(part)))
    return chunks


def _split_by_headings(markdown: str):
    """Yield (heading, body) pairs. '# ' title lines are skipped."""
    lines = markdown.replace("\r\n", "\n").split("\n")
    current_heading, current_body, title_seen = "", [], False
    for line in lines:
        if re.match(r"^# ", line) and not title_seen:
            title_seen = True
            continue
        if re.match(r"^#{2,3} ", line):
            if current_body:
                yield current_heading, "\n".join(current_body)
            current_heading = re.sub(r"^#+\s*", "", line)
            current_body = []
        else:
            current_body.append(line)
    if current_body:
        yield current_heading, "\n".join(current_body)


def _split_with_overlap(body: str, chunk_size: int, chunk_overlap: int) -> list[str]:
    """Split body into parts of at most chunk_size words.

    Parts prefer paragraph boundaries; consecutive parts share the last
    chunk_overlap words of the previous part. A paragraph that alone
    exceeds chunk_size is pre-sliced into step-sized units so the
    overlap tail always fits when units are merged.
    """
    if chunk_size < 1:
        raise ValueError("chunk_size must be >= 1")
    if not 0 <= chunk_overlap < chunk_size:
        raise ValueError("chunk_overlap must satisfy 0 <= chunk_overlap < chunk_size")

    words = _words(body)
    if len(words) <= chunk_size:
        return [body]

    step = chunk_size - chunk_overlap  # >= 1 after validation
    units: list[list[str]] = []
    for para in (p.strip() for p in body.split("\n\n") if p.strip()):
        pw = _words(para)
        if len(pw) > chunk_size:
            units.extend(pw[i:i + step] for i in range(0, len(pw), step))
        else:
            units.append(pw)

    parts: list[str] = []
    buffer: list[str] = []
    i = 0
    while i < len(units) or buffer:
        if not buffer:
            buffer = units[i]
            i += 1
            continue
        if i < len(units) and len(buffer) + len(units[i]) <= chunk_size:
            buffer.extend(units[i])
            i += 1
            continue
        # cannot extend further -> flush; next part starts with the overlap tail
        parts.append(" ".join(buffer))
        overlap = buffer[-chunk_overlap:] if chunk_overlap else []
        if i >= len(units):
            buffer = []
        elif len(overlap) + len(units[i]) <= chunk_size:
            buffer = overlap + units[i]
            i += 1
        else:
            fit = chunk_size - len(overlap)
            buffer = overlap + units[i][:fit]
            units[i] = units[i][fit:]  # remainder stays queued
            if not units[i]:
                i += 1
    return [p for p in parts if p]
