# ADR 0001: Onboard eval products through Langfuse's organization API

## Context

Each graded product needs a Langfuse project, a project key pair in Secret
Manager for External Secrets, and dataset rows in `evals_db`. DevAI was wired
by hand through `LANGFUSE_INIT_*` variables, which can seed exactly one
project and cannot rotate keys. Langfuse's Instance Management API, which
creates organizations, is Enterprise-only; the project and API-key routes are
open source and accept an organization-scoped key.

Load is under one reconciliation per minute, about five products now and fifty
within three years. Convergence within five minutes is enough.

## Decision

A namespaced `EvalOnboarding` controller holds one organization-scoped key
pair created once in the Langfuse UI. It adopts a project by recorded id, then
by display name, and creates it otherwise. It mints a project key only when the
mirrored public key is absent from Langfuse or from Secret Manager, writing the
secret half before the public half so a crash between the two forces a clean
re-mint rather than a mismatched pair. Datasets are upserted through the
`grader` role using per-table grants owned by `tesserix-k8s`.

Langfuse, Secret Manager and Postgres calls are separate idempotent steps.
Transport failures, 429 and 5xx retry through controller-runtime; other 4xx
and invalid claims are terminal until the claim changes.

## Consequences

Organization key creation stays a one-time human step per Langfuse instance.
Renaming `spec.displayName` after the first reconcile is a Langfuse rename
only because the project id is recorded in status. Nothing is deleted by the
controller.
