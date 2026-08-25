# Arda CopilotKit Runtime

This internal Node.js service adapts CopilotKit's single-route runtime to Arda's
AG-UI-compatible Go AI service.

Requests must arrive through `auth-gateway` with a verified Arda identity. The
runtime is intentionally not exposed through an ingress and does not own
application permissions, tool authorization, or tenant isolation.
