import hashlib

import pytest

from app.rag.chunker import chunk_markdown


def _count_words(text: str) -> int:
    return len(text.split())


def test_heading_chunks_and_drops_title():
    md = "# Chính sách nhân sự\n\n## Nghỉ phép\n\nMỗi năm 12 ngày.\n\n### Tang lễ\n\n3 ngày.\n"
    chunks = chunk_markdown(md)
    assert [c.heading for c in chunks] == ["Nghỉ phép", "Tang lễ"]
    assert all(c.content_hash for c in chunks)
    assert "Chính sách nhân sự" not in chunks[0].heading


def test_oversized_body_is_split_with_overlap():
    md = "# T\n\n## A\n\n" + "\n\n".join(f"Đoạn {i}: " + "nội dung dài. " * 60 for i in range(10)) + "\n"
    chunks = chunk_markdown(md, chunk_size=120, chunk_overlap=20)
    assert len(chunks) > 1
    assert "nội dung dài." in chunks[-1].content


def test_content_hash_is_sha256():
    chunks = chunk_markdown("## Hello\n\nworld")
    assert chunks[0].content_hash == hashlib.sha256(chunks[0].content.encode("utf-8")).hexdigest()


def test_empty_body_after_heading_is_skipped():
    chunks = chunk_markdown("# Title\n\n## Empty\n\n## Real\n\ncontent here\n")
    assert len(chunks) == 1
    assert chunks[0].heading == "Real"


def test_no_chunk_exceeds_chunk_size_words():
    md = "# T\n\n## A\n\n" + "\n\n".join(f"Đoạn {i}: " + "nội dung dài. " * 60 for i in range(10)) + "\n"
    chunks = chunk_markdown(md, chunk_size=120, chunk_overlap=20)
    for c in chunks:
        assert len(c.content.split()) <= 120
    # consecutive parts share the previous part's last 20 words
    for prev, cur in zip(chunks, chunks[1:]):
        prev_words = prev.content.split()
        assert cur.content.startswith(" ".join(prev_words[-20:]))


def test_single_oversized_paragraph_without_blank_lines_is_split():
    md = "# T\n\n## A\n\n" + "một từ. " * 300 + "\n"
    chunks = chunk_markdown(md, chunk_size=100, chunk_overlap=10)
    assert len(chunks) > 1
    assert all(len(c.content.split()) <= 100 for c in chunks)
    for prev, cur in zip(chunks, chunks[1:]):
        prev_words = prev.content.split()
        assert cur.content.startswith(" ".join(prev_words[-10:]))


def test_paragraph_in_range_after_flush_does_not_exceed_chunk_size():
    """Paragraph in (chunk_size - chunk_overlap, chunk_size] after a flush
    must not produce a part exceeding chunk_size (review issue 1)."""
    # 400-word para + blank + 500-word para, defaults 512/64
    md = "# T\n\n## A\n\n" + "word " * 400 + "\n\n" + "word " * 500 + "\n"
    chunks = chunk_markdown(md, chunk_size=512, chunk_overlap=64)
    assert all(_count_words(c.content) <= 512 for c in chunks)


def test_chunk_overlap_gte_chunk_size_raises():
    """Degenerate config: overlap >= chunk_size → ValueError."""
    with pytest.raises(ValueError, match="chunk_overlap"):
        chunk_markdown("## A\n\ncontent", chunk_size=10, chunk_overlap=10)
    with pytest.raises(ValueError, match="chunk_overlap"):
        chunk_markdown("## A\n\ncontent", chunk_size=10, chunk_overlap=15)


def test_chunk_overlap_negative_raises():
    with pytest.raises(ValueError, match="chunk_overlap"):
        chunk_markdown("## A\n\ncontent", chunk_size=10, chunk_overlap=-1)


def test_chunk_size_zero_raises():
    with pytest.raises(ValueError, match="chunk_size"):
        chunk_markdown("## A\n\ncontent", chunk_size=0)


def test_second_title_line_is_not_skipped():
    """Only the first # line is the title; subsequent # lines land in body."""
    md = "# Title\n\n## A\n\n# Not a title\n\n### Eh\n\nbody\n"
    chunks = chunk_markdown(md)
    assert chunks[0].heading == "A"
    # "# Not a title" is in the body of the A section because it's not a ##/### heading
    assert "# Not a title" in chunks[0].content
    assert chunks[1].heading == "Eh"
