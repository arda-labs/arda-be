# Arda Authentication Gateway

The gateway is the only public entry point for the CopilotKit runtime. It
validates the Arda session and `ai.assistant.use` permission, then forwards the
request to the internal `ai-runtime` service with a short-lived workload
identity token.
