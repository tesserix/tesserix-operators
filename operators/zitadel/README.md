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

Confidential OIDC applications are intentionally not supported yet: Zitadel
returns a client secret only at creation, so a retry after a failed handoff
cannot safely reconstruct it.

`ZitadelMachineUser` creates or adopts a service account with JWT access
tokens and gives it one credential, a personal access token or a client secret.
It never adopts a human user that shares the name. The credential goes only to
OpenBao, at `<product>/app/<claim-name>`, and only for claims listed in
`--openbao-machine-users` (`product/claim` pairs). With that flag empty the
controller does not start. OpenBao is the recovery record for the one-time
secret: while the path holds a value nothing is issued; when it is empty every
existing credential is revoked and one new one is issued and published, so a
failed handoff leaves no credential behind that anyone holds. A failed OpenBao
read never counts as empty. Rotating means deleting the OpenBao value.

The operator grants machine users no roles. A role such as user deletion is a
separately reviewed grant, because whoever may create claims must not be able to
mint privileged credentials. This control plane must not be used for user
migration; it is a separate, auditable workflow.
