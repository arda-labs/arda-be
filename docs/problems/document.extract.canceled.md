---
code: document.extract.canceled
status: 408
title: Request canceled
summary: |
  The client closed the request before extraction finished.
client_action: |
  None; retry if the user still wants the result.
operator_action: |
  None unless frequent; check client timeouts.
related_routes:
  - POST /api/documents/extract
---
