# Integrating operators, OpenBao and the hospital API

Use this with [the hospital model and adoption runbook](helivanta-onboarding.md)
and [ADR 0003](adr/0003-private-hospital-onboarding.md). These examples are
reference inputs; replace placeholders privately. Never commit populated customer
claims, JWTs, client secrets, patient data or downloaded archive records.

## Trust and credential flow

| Caller | Authentication and allowed operation |
| --- | --- |
| Zitadel operator | `identity-operator/zitadel-operator`, OpenBao Kubernetes role `zitadel-machine-user-writer`; create/read/update only explicitly reviewed credential paths |
| Helivanta backend | `helivanta/helivanta-api`, OpenBao role `helivanta-onboarding-client-reader`; read only its exact onboarding-client path |
| Product machine calling onboarding API | Short-lived Zitadel client-credentials token for the numeric onboarding project audience; registry maps the verified subject to Helivanta |
| Operator activating identity | Projected KSA JWT, audience `onboarding-provisioning`, exact operator subject checked with TokenReview |
| Onboarding API activating Helivanta | A different projected KSA JWT, audience `helivanta-provisioning`, exact onboarding API subject checked with TokenReview |
| Customer browser | Product login/session and explicit tenant membership; never a machine credential, operator JWT or Kubernetes permission |

The machine credential's production KV v2 path is
`kv/data/helivanta/prod/app/helivanta-onboarding-client`; its data field is `value`.
Its OAuth client ID is `helivanta-onboarding-client` (not a secret). The private
value is the client secret. The reader cannot update, rotate, list other paths,
delete, destroy versions or read another product/environment. It uses its own
Kubernetes service-account JWT, never the operator's identity or OpenBao root token.
The shared operator writer also has separately reviewed legacy Kora paths; it
is a platform provisioner, not a customer identity.

Inside this repository, operators reuse `internal/openbaostore`: `Latest` validates
a usable KV version, `Ensure` writes with check-and-set, and an explicit claim/path
map prevents choosing arbitrary paths. For new credentials, use no GCP fallback.
The deployed CLI map uses `product/environment/claim`, producing
`product/environment/app/claim`. Omission disables management of that machine claim.
Existing credentials are reused; loss of a published value is a recovery problem,
not permission to silently rotate a working identity.

A product backend can use the OpenBao SDK or the following HTTP contract:

1. Read its mounted, short-lived KSA JWT file into memory.
2. `POST <OpenBao>/v1/auth/kubernetes/login` with JSON
   `{"role":"helivanta-onboarding-client-reader","jwt":"<mounted JWT>"}`.
3. Read `auth.client_token` from that response without logging it.
4. `GET <OpenBao>/v1/kv/data/helivanta/prod/app/helivanta-onboarding-client`
   with `X-Vault-Token: <short-lived OpenBao token>`; read `data.data.value`.
5. Use the OAuth SDK's client-credentials flow against
   `https://auth.tesserix.app/oauth/v2/token`, with the client ID above and that
   secret. Request `openid` and
   `urn:zitadel:iam:org:project:id:<numeric onboarding project ID>:aud`.
6. Call the onboarding API with the returned access token. Cache only until its
   expiry and obtain new tokens when needed; do not persist bearer tokens in YAML.

The numeric audience comes from the `onboarding-api` project's current operator
status and the owning GitOps `zitadel.audience` configuration. It is not the
literal product key or project name. Do not copy a production audience into a
different instance. Preserve TLS verification, reject redirects carrying credentials,
set request deadlines, and keep HTTP debug/body logging disabled. Never pass
secrets as CLI arguments, put them in URLs or expose them to the browser.
OpenBao's in-cluster HTTP service relies on the reviewed cluster/mesh transport;
use the approved TLS address when crossing that boundary.

## New customer request

Use [helivanta-new-group.json](examples/helivanta-new-group.json) for a hospital
group, or [helivanta-new-independent.json](examples/helivanta-new-independent.json)
for an independently administered hospital, pharmacy or lab. The input shape is
the same because it creates an identity boundary, not physical facilities.

```http
POST /v1/hospital-onboardings HTTP/1.1
Host: onboard.tesserix.app
Authorization: Bearer <short-lived approved product access token>
Idempotency-Key: <stable UUID for this approved customer request>
Content-Type: application/json

{"product":"helivanta","displayName":"Example Hospital Group","initialAdminUserId":"REPLACE_WITH_ACTIVE_VERIFIED_HUMAN_ID"}
```

Approve the initial human explicitly. They receive identity `ORG_OWNER` in a new
org and the first clinical `tenant_admin` in the new tenant. This is a group-wide
administrator role; it is not appropriate for branch-only staff. The person must
already exist, be active and have a verified email. The API generates the stable
tenant UUID; do not let a product machine choose an existing customer binding.

A 202 response means accepted/pending, not activated. Save the returned tenant UUID
and check:

```http
GET /v1/hospital-onboardings/<returned tenant UUID>?product=helivanta HTTP/1.1
Host: onboard.tesserix.app
Authorization: Bearer <short-lived approved product access token>
```

Only `state: complete` with a current Ready condition and retained archive receipt
is complete. Retry transient failures with the same key and unchanged input;
changed input with that key conflicts. Bound retries and use backoff/jitter.
Persist the request key and returned UUID in the backend's own durable job state.
Do not start another request after an uncertain response or treat a timeout as
permission to create a second org. The console form retains its key while retrying
and provides a resume link after acceptance; a backend needs durable persistence.

## Existing hospital adoption

Use the platform console's adoption mode with
[helivanta-adopt-existing.json](examples/helivanta-adopt-existing.json) as a field
reference. This route requires a platform administrator's authenticated console
session (including its CSRF protection). A product machine receives 403 when it
tries to supply adoption IDs. Never forward a browser's org/tenant header as
management authority.

Verify the exact existing `tenant_id`, org ID and existing clinical tenant
administrator. Preserve login policy for a staged MFA cutover. Adoption retains
users, org owners, memberships, UUIDs and clinical rows. The controller refuses
an incompatible mapping, inactive grant or a different existing administrator.
[helivanta-private-claim.yaml](examples/helivanta-private-claim.yaml) illustrates
the resulting private claim structure; it is not a public GitOps customer manifest
or a replacement for the authenticated API. Kubernetes access to these claims
belongs to the platform API, not hospital users.

## Groups, independent facilities and shared people

| Scenario | What the caller does |
| --- | --- |
| Shared-governance group | One new request/key; after identity readiness, create hospitals/branches/pharmacies/labs through Helivanta's authenticated resource workflows |
| Independently administered hospital/pharmacy/lab | Separate request/key, organization and tenant; affiliation does not create cross-tenant rights |
| Autonomous group subsidiary | Separate boundary and explicit product relationships; never infer access from group branding |
| Same person at two independent customers | Keep the existing Zitadel subject, grant explicit membership in each tenant, then appropriate hospital/resource permissions |
| Branch-only clinician | Narrow role/resource grants; do not give `tenant_admin` to limit them to one branch |

Onboarding a second customer does not move a shared user out of their home org.
Their home-org MFA/SSO assurance remains relevant. Clinical invitations, resource
creation, SSO domain verification, customer branding and paid entitlement approval
are separate product workflows. A project grant permits authentication, not PHI access.

## Adding another operator or product integration

1. Review ownership, environment, namespace, service account, product scope and
   token audience before writing configuration.
2. Add its product-prefixed exact path to the operator's reviewed map and writer
   policy; add a separate exact-path read policy bound to the intended consumer.
   Do not reuse the platform writer role in a product or customer namespace.
3. Keep credentials in OpenBao and configure the consumer's read method together.
   Use a namespace-local SecretStore/readonly mount only when that consumer needs
   a Kubernetes Secret; never invent a GCP Secret Manager dependency or a wildcard
   tenant policy to make an integration convenient.
4. Register only reviewed project/machine/client identifiers. Product registration
   does not create remote objects or prove deployment. The wizard's review record
   must not be applied as guessed identity/mesh configuration.
5. Prove own-path read, other-path/environment denial, update/delete denial, token
   audience rejection, retry/replay and a real consumer call. Keep evidence free
   of credential values and patient data.
6. Record owner, rotation procedure, recovery source and access-review schedule.
   Rotation is a separately approved action; cut readers/writers over together.

## Failure and recovery checklist

- **OpenBao unavailable or denied:** leave provisioning pending. Check exact path,
  role, SA/namespace, usable KV version and expiry. Do not bypass it with a newly
  created GCP secret or a root token.
- **Wrong audience/subject:** expect 401/403 with no identity mutation. A product
  machine has no Zitadel instance-wide IAM role and cannot call internal activation.
- **Archive failure:** no Ready status. Intent must be held before identity mutation;
  the receipt must be held before completion. The workload cannot delete objects,
  release holds or overwrite a generation.
- **Membership revoked after completion:** replay must not bootstrap it again.
  Reconcile/audit the revocation; never silently restore clinical privileges.
- **Cluster or OpenBao disaster:** recover their independent backups/bootstrap
  material first. Read a pinned GCS generation privately, verify its canonical
  spec/binding and use an authorized recovery workflow. A normal retry test is
  not evidence of a full disaster-recovery drill.

GCS archives contain identifiers/configuration, not credentials or patient data.
Versioning, indefinite event holds, no expiry lifecycle, a six-year retention
minimum and restricted workload IAM protect history; privileged administrators
can ultimately alter IAM/holds. Existing operator bootstrap credentials still
using transitional storage must not be removed until reader, cold-start,
independent recovery and deletion checks pass. Do not claim all legacy secret
migrations, a proven RTO/RPO, SOC 2 certification or HIPAA compliance from these
integration tests alone.
