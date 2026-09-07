# statistical-service

Arda Go microservice for statistical reporting and analytics.

Unified container ports: HTTP `8080` / gRPC `9090` (see `configs/config.yaml`).
Standard build/test/run commands are in the `Makefile`. Migrations are embedded
goose files under `migrations/` and auto-apply on startup.
