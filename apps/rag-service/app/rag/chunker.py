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
    words = _words(body)
    if len(words) <= chunk_size:
        return [body]

    step = max(1, chunk_size - chunk_overlap)
    units: list[list[str]] = []
    for para in (p.strip() for p in body.split("\n\n") if p.strip()):
        pw = _words(para)
        if len(pw) > chunk_size:
            units.extend(pw[i:i + step] for i in range(0, len(pw), step))
        else:
            units.append(pw)

    parts: list[str] = []
    buffer: list[str] = []
    for unit in units:
        if not buffer:
            buffer = unit
        elif len(buffer) + len(unit) <= chunk_size:
            buffer.extend(unit)
        else:
            parts.append(" ".join(buffer))
            overlap = buffer[-chunk_overlap:] if chunk_overlap else []
            buffer = overlap + unit
    if buffer:
        parts.append(" ".join(buffer))
    return [p for p in parts if p]
