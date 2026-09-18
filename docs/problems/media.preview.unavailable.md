---
code: media.preview.unavailable
status: 503
title: Document conversion unavailable
summary: |
  Office preview conversion is not configured on this media-service instance
  (`GOTENBERG_URL` is empty) or the Gotenberg dependency is unreachable.
client_action: |
  Do not retry in a loop. Show the file card with the download button; retry
  only after the operator confirms conversion is enabled again.
operator_action: |
  Check that `GOTENBERG_URL` is set for the media-service deployment, that the
  `gotenberg` pod in the `platform` namespace is Ready, and correlate
  `request_id`/`trace_id` with media-service and Gotenberg logs.
---

Returned by `GET /api/media/{public_id}/preview` for Office documents when the
conversion dependency is disabled or failing. PDF pass-through and existing
downloads keep working; only inline Office previews are affected.
