import io
import pytest

from app.rag.parser import parse_document, DocumentParsingError


def test_parse_markdown_text():
    content = "# Tiêu đề\n\nNội dung chính sách nghỉ phép."
    result = parse_document(content.encode("utf-8"), "policy.md")
    assert "# Tiêu đề" in result
    assert "Nội dung chính sách nghỉ phép." in result


def test_parse_plain_text():
    content = "Dòng 1\nDòng 2\nDòng 3"
    result = parse_document(content.encode("utf-8"), "notes.txt")
    assert "Dòng 1" in result
    assert "Dòng 3" in result


def test_parse_unknown_extension_fallback_to_text():
    content = "Một đoạn văn bản thông thường."
    result = parse_document(content.encode("utf-8"), "document.custom")
    assert "Một đoạn văn bản thông thường." in result


def test_preview_chunks_service():
    from app.domain.models import ChunkPreviewRequest, ChunkerConfig
    from app.domain.security import SecurityContext
    from app.service.sources import preview_chunks

    ctx = SecurityContext(
        user_id="usr-123",
        tenant_id="t-1",
        permissions=("ai.knowledge.manage",),
        source_service="auth-gateway",
    )
    req = ChunkPreviewRequest(
        content="# Tiêu đề\n\n## Mục A\n\nNội dung mục A dài hơn bình thường một chút để kiểm tra chunking.",
        chunker_config=ChunkerConfig(chunk_size=100, chunk_overlap=10),
    )
    resp = preview_chunks(ctx, req)
    assert resp.total_chunks == 1
    assert resp.chunks[0].heading == "Mục A"
    assert "Nội dung mục A" in resp.chunks[0].content
    assert resp.chunks[0].word_count > 0


def test_parse_and_preview_file_service():
    from app.domain.security import SecurityContext
    from app.service.sources import parse_and_preview_file

    ctx = SecurityContext(
        user_id="usr-123",
        tenant_id="t-1",
        permissions=("ai.knowledge.manage",),
        source_service="auth-gateway",
    )
    file_bytes = b"# Quy che\n\n## Cong tac phi\n\nTieu chuan 500k/ngay."
    resp = parse_and_preview_file(ctx, file_bytes, "rule.md", chunk_size=50, chunk_overlap=5)
    assert resp.total_chunks == 1
    assert resp.chunks[0].heading == "Cong tac phi"
    assert resp.extracted_text is not None
    assert "Tieu chuan 500k/ngay." in resp.extracted_text


