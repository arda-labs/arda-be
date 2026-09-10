---
code: rag.invalid_job_id
status: 400
title: Invalid job id
summary: |
  The job status URL `GET /api/rag/jobs/{id}` was called with an empty job id.
client_action: |
  Do not retry unchanged. Poll the exact job id returned when the ingestion
  job was created.
operator_action: |
  Trace `request_id` and check whether the client lost the job id from a
  publish/ingest response or truncated the URL path.
---

Emitted only for an empty trailing path segment. A non-empty id that matches
no job returns `rag.job_not_found`.
