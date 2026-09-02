from app.rag.chunker import chunk_markdown


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
    from app.rag.chunker import chunk_markdown
    import hashlib
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
