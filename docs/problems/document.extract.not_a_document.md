---
code: document.extract.not_a_document
status: 422
title: Not a citizen ID card
summary: |
  The model judged the image is not a Vietnamese CCCD/Căn cước card.
client_action: |
  Retake the photo of the card itself; enter data manually if it persists.
operator_action: |
  Trace `request_id`; check `document_extract_requests_total{outcome="not_document"}` for a spike.
related_routes:
  - POST /api/documents/extract
---
