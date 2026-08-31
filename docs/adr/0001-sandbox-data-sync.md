# ADR 0001: DevAI sandbox data sync

## Context

DevAI evaluations need representative product data without exposing customer data. Initial planning assumes 50 products, 10–50 GB per product daily, five concurrent syncs, and a one-hour p99 completion target. The control plane target is 99.9% monthly availability; a missed sync is degradable because the prior successful sandbox dataset remains available.

## Decision

`SandboxDataSync` is a namespaced claim. The operator materialises a ConfigMap holding only transform policy and a daily `CronJob`. The job reads a source URL from a read-only production credential, writes only to a distinct sandbox target credential, and receives a secret salt for deterministic transforms. It does not copy Kubernetes Secret values into ConfigMaps, status, logs, or CRs.

Every copied column has an explicit transform. `email`, `name`, `hash`, and `redact` remove raw values; `preserve` is allowed only for product owners to mark non-sensitive fields. The policy should be reviewed alongside the product schema before enablement. Generated email addresses use `sandbox.invalid`; hashes are HMAC-SHA-256 with the per-product salt. CronJob forbids overlap, has one retry, and times out after one hour.

The source account must have `SELECT` only on claimed tables. The target account must be limited to the sandbox database. The worker does not mount a Kubernetes service-account token and the namespace must apply egress NetworkPolicy permitting only the source and sandbox database paths.

## Failure, rollback, and cost

If a source/target connection or transform fails, the job fails and alerts; the prior sandbox remains the evaluation baseline. Jobs use a repeatable-read source snapshot and a target transaction so a failed run does not publish partial data. Retry is safe because target tables are replaced in the transaction. Rollback is a Git revert, which stops future reconciled changes; restoring a prior sandbox snapshot follows the product database backup runbook. Approximate monthly compute is 50 × 1 hour/day × 2 GiB, plus database read and network egress; measure after a pilot.

## Alternatives

We rejected database-wide blind dumps because they cannot prove PII is removed. We rejected a queue/Temporal workflow because one scheduled, idempotent job per product is currently sufficient.
