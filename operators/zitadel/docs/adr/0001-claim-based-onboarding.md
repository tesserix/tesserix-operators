# ADR 0001: Claim-based Zitadel product onboarding

## Context

Products need repeatable onboarding into the Tesserix Zitadel organization
without exposing management credentials or coupling it to existing-user
migration. Expected control-plane load is fewer than ten reconciliations per
minute, with a p99 completion target of 30 seconds excluding Zitadel outages.

## Decision

Use namespaced Kubernetes claims in the dedicated `identity-operator` namespace.
`ZitadelProject` creates or adopts one product project. `ZitadelApplication`
references the project and creates or adopts a public OIDC application. The
controller records immutable remote IDs in status and never deletes remote
objects when a claim is removed.

The initial application API supports only native and user-agent clients with
authorization code, PKCE, and no client authentication. User-agent callbacks
must be HTTPS; native clients may use an app scheme. The controller exchanges a
machine key from GCP Secret Manager for short-lived management tokens. It reads
only its own namespace and never reads product workload secrets.

## Consequences

The system is idempotent across controller retries: it adopts a matching name
before creating and uses the stored immutable ID afterwards. If Zitadel is
unavailable, claims remain unreconciled and existing logins continue because
they do not call the operator. There is no rollback by deletion; correcting a
bad claim requires an explicit, audited remote operation.

Confidential applications and existing-user migration are excluded. A client
secret is returned only once by Zitadel, so confidential support requires an
approved Secret Manager destination and a recoverable handoff protocol. User
migration must preserve product user IDs, be resumable and audited, and must
not use email-only linking.
