# Tesserix operators

This repository contains independently deployable control-plane operators.

- [DevAI Sandbox Data Operator](docs/adr/0001-sandbox-data-sync.md) creates a
  daily anonymised sandbox sync job from a `SandboxDataSync` claim.
- [Zitadel operator](operators/zitadel/README.md) creates product projects and
  PKCE-only public OIDC applications from GitOps claims. It does not migrate
  users or delete remote identity resources.

## DevAI Sandbox Data Operator

This Kubernetes operator creates a daily sandbox sync job from a `SandboxDataSync` claim. The worker snapshots selected PostgreSQL tables, applies deterministic transforms, and writes only to a separate sandbox database.

Both binaries build from Tesserix-maintained `base-go-builder` and `base-distroless-static` images. CI delegates Go quality and container supply-chain checks to the reusable `tesserix-workflows` v2.1.0 workflows; the Docker targets are `operator` and `sync`.

Install the CRD and manager through the owning GitOps repository, then apply a claim based on [the example](config/samples/devai_v1alpha1_sandboxdatasync.yaml). Create the three referenced Secrets independently; their values must never be committed.

The source database role must be `SELECT`-only. The target role must have rights only to the named sandbox database. The worker currently replaces each selected target table in one target transaction and therefore requires compatible schemas and an FK-safe ordering of `spec.tables`.

Supported transforms are `email`, `name`, `hash`, `redact`, and `preserve`. Use `preserve` only for fields that have been classified non-sensitive. The anonymization salt makes transformed values stable within a sandbox, preserving evaluation joins without exposing original values.

See [ADR 0001](docs/adr/0001-sandbox-data-sync.md) for scaling, failure, rollback, security, and cost decisions.

## Evals onboarding operator

`operators/evals` reconciles `EvalOnboarding` claims into a Langfuse project, a
mirrored project key pair in Secret Manager and dataset rows in `evals_db`. See
[its README](operators/evals/README.md).
