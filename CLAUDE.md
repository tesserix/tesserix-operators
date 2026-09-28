# Repository guidance

Follow [AGENTS.md](AGENTS.md) for secret storage. OpenBao is the default for new
and existing products and operational credentials. Existing GCP Secret Manager
fallbacks are migration-only; never add a new one.
