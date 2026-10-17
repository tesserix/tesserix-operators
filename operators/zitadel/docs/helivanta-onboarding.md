# Helivanta identity and hospital onboarding

## Organization and project model

Use one Zitadel organization per independent identity administration boundary,
with one stable Helivanta tenant UUID. Use the shared platform-owned Helivanta
project and product clients. Do not create a project, client secret or deployment
for each physical hospital. [ADR 0003](adr/0003-private-hospital-onboarding.md)
is the implementation decision and supersedes the earlier review-only design.

| Customer arrangement | Identity and authorization model |
| --- | --- |
| One independent hospital | One org and tenant; hospital and branch resources inside the tenant |
| Hospital group sharing identity governance | One org and tenant; multiple hospitals and branches, each with explicit resource permissions |
| Group pharmacy or lab | Resource in the group's tenant; pharmacy/lab permissions remain explicit |
| Independent pharmacy, lab or autonomous subsidiary | Separate org and tenant; a commercial affiliation does not grant data access |
| Clinician shared across hospitals | One home Zitadel subject; explicit membership and appropriate resource grants in each tenant |

Zitadel authenticates the person. Helivanta memberships and OpenFGA authorize
product access, and tenant-scoped transactions/forced PostgreSQL RLS isolate rows.
`tenant_admin` is deliberately a group-wide role. Give branch-only staff narrower
roles and resource permissions. Identity `ORG_OWNER` is separate from clinical
`tenant_admin`. Neither an org hint, email suffix, project grant nor browser-supplied
tenant UUID creates clinical membership. Tenant switching rechecks membership
and preserves the original authentication and idle-session deadlines.

Shared users retain their home-org authentication/MFA policy. A strict destination
hospital policy alone does not strengthen authentication performed by a weaker
home org. Enforce trusted-IdP/home-org assurance and a reviewed step-up policy
before treating shared-user access as satisfying a hospital's MFA requirements.

See the [OpenBao/API integration guide and examples](openbao-and-onboarding-integration.md)
for writer/reader bindings, HTTP contracts and recovery checks.

## Provisioning interfaces

A platform administrator uses the authenticated onboarding console at
`https://onboard.tesserix.app/orgs/hospital`. Approved product backends may call
`POST /v1/hospital-onboardings` with a verified, product-scoped machine token and
stable `Idempotency-Key`. Only platform administrators may supply existing
organization and tenant IDs for adoption. Generic organization creation, grant
and enrolment routes reject Helivanta so callers cannot bypass retained records.

New requests specify the display name and an existing active, verified human
administrator subject. The API derives the new tenant UUID from the verified
caller, product and request key. The controller creates a deterministic org,
assigns that requested human as initial `ORG_OWNER`, requires MFA and domain
verification, disables public signup/discovery, and establishes the shared
Helivanta project grant. This is intentional identity delegation; approve the
initial administrator before submitting. No IAM-wide role is granted.

The API alone creates/reads immutable private `ZitadelOrganization` claims in
`identity-operator`. Customer identifiers and claims never enter public GitOps.
The claim's product must map to an explicitly reviewed platform-owned project.
Before changing identity, the controller writes held intent YAML. It then calls
the onboarding API with the exact operator service-account token and audience
`onboarding-provisioning`. The API checks the stored claim and fresh Zitadel
proof, commits the projection/entitlement/audit and calls Helivanta using a
separate `helivanta-provisioning` token. Helivanta accepts only the onboarding
API service account and reconciles membership plus OpenFGA permissions after
its local transaction commits. The exact HTTPS workload route bypasses browser
session middleware; the backend still authenticates every request.
The public Helivanta hop uses `X-Onboarding-Token` to keep the Kubernetes JWT
separate from ingress OAuth validation. TokenReview still requires the exact
audience and onboarding API service account; dual credential headers are denied.

Only a new pending tenant may bootstrap its first clinical administrator. An
existing tenant requires the requested human's existing `tenant_admin` membership.
A persistent activation marker prevents a completed request from recreating a
revoked final membership. Dependency failures remain pending and retry forward;
no rollback deletes an identity or removes clinical data. The controller writes
a held receipt before reporting Ready at the current claim generation. The console
reports complete only when the API confirms that activation and archive receipt.

This provisions the identity/tenant foundation. Hospital, branch, pharmacy and
lab records, commercial approval, workforce invitations, customer IdPs, custom
branding, and their resource-level access are separate authenticated product
configuration. Do not describe identity readiness as completion of all clinical
or commercial setup.

## Adopt an existing hospital safely

1. Verify its existing Zitadel `tenant_id` metadata, UUID, org ID and exact
   existing clinical administrator. Capture current memberships and managed
   project/client settings privately before adoption.
2. Select adoption in the console and supply those existing IDs. Keep
   `preserveLoginPolicy` selected for a staged MFA/domain-policy cutover.
3. The controller rejects a mismatched mapping or inactive identity/grant. It
   does not move users, replace IDs, change org owners or create a second tenant.
4. Follow the resume link and check status until Ready/current with a held receipt.
   Compare existing memberships and client settings with the captured baseline,
   verify the product session, and check a safe replay returns the same identity.
5. Separately plan MFA enrollment/recovery, verified domains and any project-wide
   org gate. Existing inherited policy preservation is a compatibility choice,
   not evidence of MFA enforcement. Never enable a shared project gate before
   every intended existing customer has its correct grant.

YES HOSPITAL must use this adoption path with its current private IDs. Never put
those customer bindings or user subjects in public fixtures or sample manifests.
No clinical database migration is required by the identity-adoption feature.

## Retained YAML and recovery

The archive bucket is `tesseracthub-480811-onboarding-records-prod`. Each customer
has immutable content-addressed revisions under
`helivanta/prod/tenants/<UUID>/revisions/<specSHA>/`, containing held `intent.yaml`
and `identity.yaml`. Each write requires `ifGenerationMatch=0`; an existing record
must match exactly and remain held. Records contain configuration identifiers,
never credentials, patient data or clinical payloads. Credentials stay in OpenBao.

The private bucket uses uniform IAM, public-access prevention, versioning,
default event-based holds, six-year minimum retention and no expiration lifecycle.
Terraform prevents bucket destruction. The actual operator Google service account
has only create/get on that product/environment prefix; its Workload Identity
binding is restricted to `identity-operator/zitadel-operator`. It cannot delete,
overwrite or release holds. Administrators controlling IAM can ultimately change
these protections, so this is not a promise of absolute undeletability. An
irreversible retention lock requires a separately reviewed legal/operational decision.

For recovery, read a pinned GCS generation, verify the hash and identity binding,
and restore through an authorized private-claim workflow. Reconciliation reuses
existing identities and held archive generations. A non-destructive replay tests
idempotency; it is not a full disaster-recovery drill. Verify independent cluster,
OpenBao and database recovery before claiming an established RTO/RPO.

Offboarding must stop the desired-state claim before revoking project grants,
otherwise reconciliation may restore them. Preserve archive history. Clinical
membership revocation and commercial offboarding use their audited product flows;
the identity controller does not delete customers or identities.

## Custom login UI using Zitadel

Register a product client per web/mobile/API trust boundary. Public web clients
use authorization code with None/PKCE S256; exact HTTPS callbacks are mandatory.
The operator's HTTPS `loginBaseUri` chooses a trusted Login V2 implementation.
Confidential web client adoption is not supported; never silently convert one.

Two supported approaches:

- **Org branding:** configure hospital logos, colors, text and policies in its
  organization. Use project private labeling that permits the login user's org
  policy when hosted Zitadel branding is required. The operator preserves that
  existing project setting. Domains must be verified before tenant routing.
- **Product-hosted Login V2:** Helivanta renders the interface and its backend
  calls Zitadel Session/OIDC APIs using a backend-only login-client credential
  in OpenBao. That credential never belongs in the browser or customer YAML.
  Credentials are checked by Zitadel; the custom UI/backend remains part of
  the credential trust boundary. Customer themes should be validated design
  tokens and approved images/text, never arbitrary scripts or HTML.

Map approved hosts/invitations to immutable org IDs on the server. An org hint
selects login experience, not authorization. Preserve MFA, recovery, password
expiry, WebAuthn origin/RP-ID and auth-request binding. Validate state/nonce,
issuer/audience/token type/algorithm/expiry; allowlist post-login return paths.
Use Secure/HttpOnly/SameSite cookies and CSRF protection. A Zitadel ID token is
not a general clinical API bearer token. Customer IdP linking and upstream URL
fetches require ownership checks, exact secrets and SSRF controls; this flow
does not expose them as unrestricted self-service configuration.

## Security assurance and release evidence

See [security-assurance.md](security-assurance.md) for the control/evidence matrix
and outstanding operational requirements. Code changes and successful smoke
tests do not establish SOC 2 certification or HIPAA compliance. BAA coverage,
workforce policies, access reviews, risk assessments, audit retention, recovery
drills and incident response require organizational evidence.

The initial read-only review on 2026-10-17 authenticated the browser to
`https://auth.tesserix.app` as the requested account and verified Helivanta's
existing public Web/PKCE client, opaque access tokens and Login V2 base URI.
The implementation preserves those contracts. Production mutations are delivered
through reviewed GitOps with before/after membership and claim checks. Release,
adoption and archive observations must be recorded only after they actually pass.
