---
code: media.preview.unsupported
status: 415
title: Preview not supported
summary: |
  The document type cannot be rendered as a PDF preview. Only PDFs and Office
  documents (Word, Excel, PowerPoint, OpenDocument) are convertible.
client_action: |
  Do not retry unchanged. Offer the original file for download instead of an
  inline preview.
operator_action: |
  Trace `request_id` and check `media_files.content_type`/extension for the
  public id; extend the supported type list deliberately if a new format must
  preview.
---

Returned by `GET /api/media/{public_id}/preview` when the stored file is not a
PDF and not an Office/OpenDocument format that Gotenberg can convert.

The SPA treats this as "no inline preview available" and falls back to the file
card with its download button.
