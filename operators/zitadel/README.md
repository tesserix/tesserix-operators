# Zitadel operator

This operator reconciles GitOps-declared Zitadel product resources. It is not an
existing-user migration engine.

`ZitadelProject` resolves the declared organization name to its immutable
Zitadel ID, adopts a matching project or creates one, and records the immutable
project ID in status. `ZitadelApplication` references that project and creates
or adopts an OIDC public client. It permits only native or user-agent clients,
the authorization-code flow, and no client authentication; this enforces PKCE
at the authorization server and does not generate a client secret. Deleting a
claim does not delete its remote resource.

The controller runs in the dedicated `identity-operator` namespace, never the
existing `zitadel` namespace. It exchanges its mounted Zitadel machine-key for
short-lived management tokens. The machine key comes from GCP Secret Manager
through External Secrets; claims and status must never contain credentials,
authorization codes, passkeys, MFA factors, or user identity data.

Expected control-plane scale is low (under ten reconciliations per minute, one
small JSON request/response per operation). Reconciliation targets p99 under
30 seconds and retries only transient management API failures. If Zitadel is
unavailable, existing applications continue authenticating; new claims remain
not-ready and are retried.

Confidential clients are intentionally not supported yet. They require an
explicit GCP Secret Manager destination and one-time recovery design: Zitadel
returns a client secret only at creation, so a retry after a failed handoff
cannot safely reconstruct it. This control plane must not be used for user
migration; it is a separate, auditable workflow.
