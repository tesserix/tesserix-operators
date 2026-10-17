# Helivanta identity and hospital onboarding

## Decision

Use one Zitadel organization per independently administered hospital customer,
and one shared `helivanta` project owned by TESSERIX. A hospital group can share
an organization when it intentionally shares its directory, SSO administration
and identity policies; individual hospitals still need explicit application
permissions. Do not make a project, client ID, client secret or deployment for
every hospital.

An organization is an identity administration boundary. It is not a substitute
for Helivanta's tenant authorization. A user has a home identity organization
and may have explicitly authorized membership in several Helivanta tenants.
An email suffix, organization selection, invitation URL or browser-supplied
`tenant_id` does not grant membership.

| Concern | Authority |
| --- | --- |
| Authentication, password/passkeys, MFA, external SSO | Zitadel |
| Which customers bought Helivanta | Tenant project grants plus Helivanta's active subscription/tenant state |
| Who belongs to a hospital and may access a clinical object | Helivanta membership and OpenFGA |
| Row isolation | Tenant-scoped transactions and forced PostgreSQL RLS |
| Product project and OIDC client configuration | GitOps Zitadel operator |
| Customer organization lifecycle | A durable, authenticated Helivanta onboarding workflow |
| Credentials | Environment-scoped OpenBao paths with namespace-bound identities |

The operator does not implement the customer lifecycle workflow. It does
not create hospitals, organization owners, external IdPs, branding or customer
project grants. No Kubernetes hospital CRD is introduced: customer directories,
IdP credentials and signup state do not belong in a public GitOps repository.

## What was actually verified on 2026-10-17

The browser console was authenticated as `samyak.rout@gmail.com` at
`https://auth.tesserix.app`. The public OIDC issuer matched that address and
advertised S256 PKCE. Read-only GKE inspection used account
`unidevidp@gmail.com`, project `tesseracthub-480811`, context
`gke_tesseracthub-480811_asia-south1_tesseract-prod-in-gke`.

Zitadel v4.15.3 had 3/3 replicas, its Login V2 deployment 2/2, and the identity
operator 1/1. Four project claims, eleven application claims and three machine
user claims reported Ready at their current generations. Helivanta was not
among those claims. Readiness is an observation, not proof of tenant isolation.

The live Helivanta project ID was `386782452506428319`. Its existing
`helivanta-web` client ID was `386782591925092979`, application ID
`386782591824429683`, application type Web, authentication None/PKCE,
authorization-code grant, no refresh grant, and a Login V2 base URI of
`https://helivanta.app`. Its access tokens were opaque, userinfo assertion was
off, and two exact callbacks were registered: `/api/auth/callback` and
`/api/auth/silent-renew`. A memory-only read-only API audit verified the new
resource-owner checks against the real Helivanta project and all three Kora
machine users. The [adoption preview](../config/samples/identity_v1alpha1_helivanta_adoption.yaml)
matched all managed live OIDC settings without an update. Starting sign-in at `https://helivanta.app/login`
returned to the custom Helivanta credential screen with a Zitadel V2 auth
request. No password was entered and a complete authenticated product session
was not tested.

The following remain live findings, not settings changed by this review:

- Helivanta's user-role and organization-project-grant login gates were both off.
- TESSERIX and instance login policies allowed registration and did not force
  MFA. Public organization registration was off. Product self-registration may
  be intentional; hospitals should have separate, stricter org policies.
- TESSERIX inherited domain settings with organization-domain verification off,
  while domain discovery was on. Do not enable hospital domain routing until
  ownership has been verified and the effective policy is checked.
- The instance Login V2 forcing feature was off. Per-client V2 settings matter;
  changing the instance forcing switch would affect unrelated products.
- The operator management key was synchronized from the transitional GCP
  Secret Manager source. Its existing key was used only in memory for authorized
  read-only API checks. No credential values were printed, saved, committed or
  copied to another secret store. Migration is pending verified OpenBao storage,
  consumer cutover and independent recovery.

Local Helivanta source review found a custom login client that reads effective
organization policies and a product session/membership model backed by OpenFGA.
The source inspected did not contain a production organization-provisioning
workflow. This review did not run Helivanta's database/RLS integration suite or
a hospital-to-hospital penetration test, so it does not certify that product.

## Automatic onboarding contract

The following is the required implementation contract for Helivanta's runtime
onboarding service. It is not provided by the product-resource operator.

1. **Authorize the request.** An authenticated platform onboarding principal or
   approved customer-signup flow supplies a stable idempotency key. Validate the
   commercial approval, hospital identity and authorized first administrator.
   A public email-domain match is insufficient.
2. **Persist intent.** In one local transaction create a pending tenant and an
   onboarding job/outbox record. Use a unique customer key and unique Zitadel
   organization mapping. Store the workflow ID, step and immutable remote IDs.
   Never hold a database transaction open across a Zitadel API call.
3. **Create the organization.** A backend-only, least-privilege provisioning
   identity calls `POST /v2/organizations`. Save the returned org ID before
   proceeding. Use a deterministic requested ID where supported by the API;
   after an ambiguous timeout recover by that recorded/deterministic identity,
   never by a mutable display name or email alone.
4. **Set hospital policy.** Keep local signup off by default. Require the agreed
   MFA/passkey policy, lockout, recovery and session lifetimes. Read back the
   effective org policy; fail closed if it cannot be determined. SSO-based MFA
   assurance needs an explicit trusted-IdP contract, not an assumption that
   every upstream login used MFA.
5. **Grant the product.** From the project-owner context create an explicit
   project grant to the new hospital organization for the shared Helivanta
   project (`POST /management/v1/projects/{projectId}/grants`). Use a small
   entitlement role if the grant requires roles. Doctor/nurse/ward permissions
   remain in OpenFGA. Do not use the operator's `restricted` member list for
   hospitals: that mode deliberately prunes user grants to an exact list.
6. **Invite the first administrator.** Bind the accepted verified subject to
   this pending tenant through a single-use, expiring invitation. Grant only
   the hospital's application administrator permissions. Zitadel management
   permissions are a separate decision: use an org-scoped user-manager role
   by default, and grant ORG_OWNER only when delegation of SSO and security
   policy management is explicitly intended. Never grant IAM_OWNER.
7. **Configure customer SSO and appearance.** The authenticated tenant admin
   verifies its domain via DNS/HTTP, adds an organization-scoped IdP and binds
   it to that org's login policy. Keep account linking and automatic signup
   disabled until ownership, upstream issuer/tenant and allowed identities are
   checked. Persist upstream client credentials only in that hospital's exact
   environment-scoped OpenBao path. DNS ownership and customer IdP setup may
   require human action; show a pending state rather than pretending signup
   is already complete. Tenant-supplied issuer/metadata URLs must be validated;
   block metadata-service, loopback/private and link-local destinations unless
   an explicitly reviewed assisted connector requires them. LDAP needs a
   separately approved network path, not unrestricted self-service egress.
8. **Provision application authorization.** Add the tenant's initial membership
   in Helivanta, publish through its transactional outbox, and await successful
   OpenFGA reconciliation. Mint the Helivanta session only after verified
   subject membership and active tenant checks. Tenant switching rechecks
   membership on the server; it never trusts the submitted tenant ID alone.
9. **Activate last.** Verify invitation acceptance, effective MFA, product grant,
   tenant mapping, authorization synchronization and cross-tenant denial. Then
   transition `pending` to `active`. Persist and audit actor, immutable IDs and
   outcome, without credentials or patient data.

This crosses more than two systems and waits for people/DNS. Use a durable
workflow (the house default is Temporal), not a synchronous signup handler
with a sequence of HTTP calls. Workflow ID `helivanta-onboard-{tenantID}` is the
idempotency key. Activities need timeouts, bounded transient retries and a
manual-recovery state. Persist step results; retry after a crash without
creating duplicate organizations, grants, invitations or membership tuples.

If Zitadel, OpenBao, the database or OpenFGA is unavailable, new onboarding
stays pending/not-ready. An authorization error never becomes an allow.
Existing authentication does not depend on the Kubernetes operator. If a later
step fails, leave the new tenant inactive and retain its IDs for recovery;
do not automatically delete an organization or revoke an existing customer's
access. Destructive compensation and credential rotation need specific approval.

## Custom login UI using the shared identity platform

For a product, register one client per web/mobile/API trust boundary. The new
`loginBaseUri` claim field selects an HTTPS Login V2 service. Web clients can
use `applicationType: web` with authorization code and None/PKCE, matching
Helivanta's existing client. Confidential web clients are not supported by
this operator; do not adopt one and silently convert it to a public client.

Two supported experiences:

- **Organization branding:** logos, colors, texts and org-specific policies are
  configured on the hospital organization. Use the project private-labeling
  setting that permits the login user's organization policy when hosted Zitadel
  branding is required. The operator preserves that setting during updates.
- **Product-hosted Login V2:** Helivanta renders the branded UI and uses a
  backend login-client identity to talk to Zitadel's Session/OIDC APIs. That
  credential stays in OpenBao and the backend; the browser never receives it.
  Styling must never bypass org MFA/password-expiry checks, account recovery,
  factor enrollment, WebAuthn origin/RP-ID checks or auth-request binding.
  The present Helivanta login UI receives credentials; its backend checks them
  through Zitadel, so the UI/backend are part of the credential trust boundary.

Map trusted product hosts or invitations to immutable org IDs server-side.
An org scope/hint selects the intended login experience; it is not proof of
membership. Never accept a browser-supplied management org header. Do not put
hospital identity credentials, arbitrary JavaScript or unsanitized HTML in
branding. Register exact HTTPS callbacks, validate state/nonce and PKCE S256,
and allowlist post-login return paths. Reject wildcard callbacks and open
redirects. Secure/HttpOnly/SameSite cookies and CSRF protection belong to the
product session layer. Do not use a Zitadel ID token as a generic API bearer
token; validate the intended token type, issuer, audience, algorithm and expiry.

## Operator changes in this review

- Go 1.27.2 for every binary in the shared module, CI and Docker builder.
- Public Web clients and explicit Login V2 base URI support. Token type and
  userinfo assertion are configurable so adopting Helivanta preserves its
  opaque/no-userinfo behavior. Existing products retain JWT/userinfo defaults.
  Omitted zero-valued protobuf enum fields are normalized before comparing.
- OIDC drift reconciliation independent of optional refresh-token management;
  omission preserves existing refresh grants and custom login configuration.
- Preservation of logout URLs, additional origins, clock skew and role flags
  when replacing an OIDC config. Unsafe development mode is removed.
- `access.mode: organization` turns on the tenant-project-grant gate without
  pruning hospital users. Existing project branding and org gates survive
  other updates; absent access preserves adopted project controls.
- Restricted/org gates are set at project creation, avoiding a public interval.
- Verified-human and resource-owner checks before email grants; exact machine
  identity and organization checks before adoption.
- Unsafe callback URI rejection, current readiness, complete paginated lists,
  no unchanged-status write loops, bounded reconciliation and five-minute
  drift checks. Leader election prevents concurrent credential writers.
- Reviewed credential destinations are checked before remote mutation. A
  missing previously published credential requires recovery, not silent
  rotation. Initial failed handoffs remain recoverable by retry.
- New credential mappings support `product/prod/claim`, `product/dev/claim`
  and `product/uat/claim`; two-part legacy mappings remain compatible. OpenBao
  policies and consumer readers must change together before any live cutover.

Claims are privileged platform configuration. Product tenants must never get
Kubernetes claim-write permissions or the operator management credential.
Names alone are not cryptographic ownership: only trusted platform maintainers
may approve adoption and the reviewed machine-user list.

## Safe rollout and outstanding work

No production setting, grant, credential, deployment or customer was changed.
No commits, pushes or pull requests were created.

The owning Argo application points to `tesserix-k8s/k8s/operators/zitadel`.
Local operator sample manifests do not deploy themselves. The owning checkout
also had unrelated user changes and did not match the complete live claim
inventory; those files were preserved. A prerequisite patch was instead prepared
against the current GitHub manifest and verified in an isolated temporary copy:
[GitOps prerequisites](../config/rollout/README.md). It retains the current
machine-user opt-ins and credential source and does not change the image digest.

Before rollout, bring the updated CRDs and namespaced lease RBAC into the owning
repository, build/publish an immutable image from this reviewed code, and update
its image digest through the existing delivery path. For the first upgrade
from the old controller, use Recreate to avoid an overlap with a version that
does not participate in leader election. Operator downtime delays provisioning;
it does not stop existing Zitadel authentication. The sample manager manifest
includes this strategy, non-root readable key mounts and lease permissions.
The Deployment uses a per-resource `ServerSideApply=false` Argo option with
`rollingUpdate: null`: testing caught SSA retaining the old rolling-update
fields when changing strategy. The Deployment was accepted using client-side
apply with server dry run; the other prerequisites were accepted with SSA
server dry run. Neither validation persisted changes.

Adopt the existing Helivanta project with **access omitted first**, after
confirming bootstrap and operator cannot both write its configuration. Preserve
all remote IDs and exact live callback/logout settings. Audit the adoption
preview before enabling a claim. Establish and test grants for every existing
hospital/customer and support identity before switching Helivanta to
`access.mode: organization`. Turning on that gate prematurely can lock out
existing users. Existing sessions may remain valid until their expiry, so the
product must enforce membership and tenant status independently.

Do not flip instance-wide MFA, registration, domain or Login V2 settings as a
shortcut. Configure new hospital orgs strictly, then separately plan changes
for existing orgs with enrollment/recovery and customer-impact checks. Do not
move existing users between orgs or link accounts by email during this work.

Required staging evidence before clinical production onboarding:

- Two hospital orgs, distinct domains and distinct tenant mappings.
- Inventory of instance/org/project administrators and every service identity's
  effective IAM roles, with no cross-product user-management authority granted
  to a product workload by accident. Existing PAT lifetimes and privileged
  credential rotation/revocation require a separately reviewed operational plan;
  this operator intentionally grants no IAM roles.
- Password/passkey MFA, upstream SSO, domain challenge and invitation replay
  rejection, cross-org login selection, password reset and recovery flows.
- Unauthorized organization rejected by the product grant gate; authorized
  hospital A cannot read, update, export or subscribe to hospital B's objects.
- JWT issuer/audience/algorithm/expiry failures; session revocation, logout,
  CSRF, tenant switching and denied direct Zitadel-token access to clinical APIs.
- Crash/retry at each onboarding step; no duplicate orgs or owner invitations.
- OpenBao/Zitadel/OpenFGA outages, denied secret paths, credential recovery and
  an independently accessible, tested identity backup restore.

Production rollout, grant changes, credential rotation and any migration need
specific approval under the repository AGENTS.md. Rollback is the previous
immutable operator image plus compatible CRDs. Remote configuration changes
are not undone by rolling back the binary; restore only captured non-secret
settings with separately reviewed actions. Never delete claims expecting that
to delete or recreate remote resources. Restore/recovery RPO and RTO remain
unverified by this review.

## Verification record

Regression tests were written and observed failing before their fixes, including
callback drift, erased login settings, unsafe adoption, unsafe callback schemes,
unverified/foreign identities, missing credential recovery, stale readiness,
truncated grant lists and management redirects.

Commands run with Go 1.27.2 (a temporary GOCACHE was used for sandbox writes):

| Check | Result |
| --- | --- |
| `go test -race ./...` | Passed after fixing the synthetic credential-shaped test fixture |
| `go vet ./...` | Passed |
| `go build ./...` | Passed for every operator |
| `golangci-lint run ./operators/zitadel/... ./internal/openbaostore/...` | Passed, zero issues |
| `govulncheck ./...` | Passed, no vulnerabilities after upgrading OpenTelemetry to 1.44.0 |
| `gofmt -l operators/zitadel internal/openbaostore` | Clean |
| `git diff --check` | Clean |
| `kubectl kustomize` for CRDs and manager | Rendered successfully |
| `kubectl apply --server-side --dry-run=server --force-conflicts` for the three CRDs | All accepted; no changes persisted |
| `golangci-lint run ./...` | 13 pre-existing findings outside changed packages: unchecked cleanup errors and capitalized Secret Manager errors |

The dry run initially reported Argo field-ownership conflicts. The subsequent
`--force-conflicts` call still used **server dry run** and did not take ownership
or alter live schemas. Kubernetes rejected the first uniqueness constraint;
the final manifests use supported set-list semantics and were accepted.

Container base images are pinned to the digests resolved from the existing
Tesserix weekly images. `.dockerignore` excludes VCS metadata, local agent/cloud
configuration, environment files and private-key files from the build context.
No image was pushed and no production rollout was attempted.
The source changes also reject confidential/unknown-client adoption and refuse
to forward management authorization through redirects.

A final read-only cluster check still showed the original operator image at
1/1, all 4 project / 11 application / 3 machine-user claims Ready at their
current generation, Zitadel at 3/3 and its login service at 2/2.

Final API preview: all four managed projects passed organization ownership and
access checks; both restricted Langfuse members passed verified-human checks,
with two actual grants for two declared members; every one of the eleven managed
OIDC applications matched the new reconciler's desired configuration. No updates
were performed. The separate Helivanta adoption preview also matched exactly.
The temporary audit program was removed after verification.

Final container verification: `docker build --target zitadel-operator --tag
tesserix-zitadel-review:go1.27.2 .` passed. Running `--help` with `--read-only`,
`--network=none`, all capabilities dropped and `no-new-privileges` passed.
The image user is `65532:65532` and the extracted ELF reports `go1.27.2`.
This local arm64 image has ID
`sha256:f85575398a970d33babaa81210d6360f2a5d27667a52090626cde119be84a90c`;
production still requires its CI-produced image for the deployment platform.
`go mod verify` and repository-wide formatting passed. The exact
`govulncheck@v1.1.4` pinned by the existing shared CI workflow also passed on
Go 1.27.2, as did the current local vulnerability scanner.
