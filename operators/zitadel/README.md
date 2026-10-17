# Zitadel operator

This operator reconciles GitOps-declared product resources and private runtime
organization claims submitted by the authorized onboarding API. It preserves
existing user subjects and clinical tenant IDs during explicit adoption.

`ZitadelProject` resolves the declared organization name to its immutable
Zitadel ID, adopts a matching project or creates one, and records the immutable
project ID in status. `ZitadelApplication` references that project and creates
or adopts an OIDC public client. It permits native, user-agent and public Web
clients, the authorization-code flow, and no client authentication; this enforces PKCE
at the authorization server and does not generate a client secret. Deleting a
claim does not delete its remote resource.

The controller runs in the dedicated `identity-operator` namespace, never the
existing `zitadel` namespace. It exchanges its mounted Zitadel machine-key for
short-lived management tokens. The live management key still comes from the
transitional GCP source through External Secrets. Use OpenBao for new keys and
retain independent recovery during cutover. Claims and status must never contain credentials,
authorization codes, passkeys, MFA factors, or user identity data.

Expected control-plane scale is low (under ten reconciliations per minute, one
small JSON request/response per operation). Reconciliation targets p99 under
30 seconds; failed attempts use the controller workqueue backoff. Terminal
configuration errors remain not-ready until corrected. If Zitadel is
unavailable, existing applications continue authenticating; new claims remain
not-ready and are retried.

See [the Helivanta onboarding and live review](docs/helivanta-onboarding.md)
and [ADR 0002](docs/adr/0002-hospital-tenancy-and-login.md) for hospital organizations,
project grants, custom Login V2 UIs, tested changes and the pending rollout.

`loginBaseUri` selects a product-hosted HTTPS Login V2 UI. New clients explicitly
use V2. Omission preserves an adopted client's login configuration. Omission of
`refreshToken` preserves an existing refresh grant while other declared OIDC
settings still reconcile. Web clients remain public/PKCE; never adopt a
confidential client as a public claim. `accessTokenType` defaults to `jwt` and
`idTokenUserinfoAssertion` defaults to true for existing BFF consumers. Declare
`bearer` and false explicitly when preserving Helivanta's token contract; opaque
API bearer tokens need introspection. See the audited
[Helivanta adoption preview](config/samples/identity_v1alpha1_helivanta_adoption.yaml).

`access.mode: organization` requires the user's organization to hold a grant to
the product project and leaves user grants under tenant administration. Omitted
access preserves adopted policy. `restricted` is an exact platform-maintained
user list, unsuitable for hospital self-service. Claims are trusted platform
configuration and must not be writable by customer tenants.

The controller uses leader election, a 30-second attempt deadline and five-minute
drift checks. Install the namespaced lease RBAC before upgrading. The first
upgrade from the old controller must avoid overlapping writers (Recreate).

Confidential OIDC applications are intentionally not supported yet: Zitadel
returns a client secret only at creation, so a retry after a failed handoff
cannot safely reconstruct it.

`ZitadelMachineUser` creates or adopts a service account with JWT access
tokens and gives it one credential, a personal access token or a client secret.
It never adopts a human user that shares the name and requires the machine
account name to match the reviewed claim name. The credential goes only to
OpenBao, at `<product>/<environment>/app/<claim-name>`, and only for claims listed
in `--openbao-machine-users` (`product/prod/claim`, `product/dev/claim` or
`product/uat/claim`). Existing two-part `product/claim` mappings keep their legacy
`<product>/app/<claim-name>` paths until writers and readers migrate together.
With that flag empty the machine-user controller does not start; project and
application reconciliation still runs. OpenBao is the recovery record for the
one-time secret: while the path holds a value nothing is issued. A failed first
publication is recoverable on retry by revoking orphan
credentials before issuing again. If a previously published credential is
missing, reconciliation stops for explicit recovery; deleting an OpenBao value
is not an approved rotation mechanism. A failed OpenBao read never counts as
empty. Credential rotation must be separately approved and update readers and
writers together.

The operator grants machine users no roles. A role such as user deletion is a
separately reviewed grant, because whoever may create claims must not be able to
mint privileged credentials. This control plane must not be used for user
migration; it is a separate, auditable workflow.

`ZitadelOrganization` is opt-in through reviewed `--organization-products`, a
private held GCS archive bucket and an authenticated onboarding API. Customer
claims are runtime data and must never be committed to public GitOps. See
[ADR 0003](docs/adr/0003-private-hospital-onboarding.md) for hospital groups,
branches, independent pharmacies/labs, shared users, safe adoption and retries.
See [security assurance](docs/security-assurance.md) for SOC 2/HIPAA control
coverage and the organizational evidence that code cannot establish.
