# ADR 0001: Reconcile analytics onboarding through supported APIs

## Context

The existing Helm hook inserts OpenPanel projects and clients directly into
PostgreSQL and writes Kubernetes Secrets across application namespaces. That
couples onboarding to OpenPanel's private schema and grants broad Kubernetes
write access.

Onboarding is expected to stay below one reconciliation per minute with claim
payloads below 4 KB, about 10 products after 12 months and 50 after 36 months.
The target is 99.9% availability and convergence within five minutes, with a
10-second p99 budget for each external request.

## Decision

Use a namespaced `AnalyticsOnboarding` controller and OpenPanel's supported
Manage API. The controller creates or updates projects, ensures a write client,
and uses Google Secret Manager's REST API with workload identity to create or
update a derived client-ID secret. Root API credentials are mounted from GCP
through External Secrets and reread for every request.

OpenPanel and Secret Manager calls are separate idempotent steps. A crash after
either step converges on replay. HTTP 429, 5xx, transport failures, and status
write conflicts are retried by controller-runtime; invalid 4xx and invalid
claims are terminal until desired state changes.

## Consequences

OpenPanel or Secret Manager downtime makes only onboarding not-ready; existing
event ingestion is unaffected. A compromised controller can manage OpenPanel
projects and secrets under the fixed analytics prefix, so it receives a
dedicated workload identity and no Kubernetes Secret write permission.

Deleting a claim does not delete the project or secret. Rollback removes the
claims and deployment while retaining provisioned state. The steady-state cost
is one 50m CPU/128 MiB pod plus negligible Secret Manager version storage.

Direct database reconciliation and a generic cross-namespace Kubernetes Secret
writer were rejected because both expand blast radius and depend on internal
schemas or broad RBAC. Temporal was rejected because this is a replay-safe
two-step reconciliation with no wall-clock waits or compensation.
