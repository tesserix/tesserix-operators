# ADR 0005: Readable, unique Helivanta organization names

Status: implementation candidate; not deployed.

Helivanta customer organizations currently put the full tenant UUID before the
business name. New first-owner signups instead use `Helivanta-Yes-Hospital`.
The business name remains unchanged in the private claim and the product's
canonical hospital/pharmacy record. This rule applies to fresh Helivanta owners;
other products, existing-user creation and explicit adoption are unchanged.

Zitadel owns organization names, their instance-wide uniqueness and derived
default domains. Tenant UUIDs, deterministic native organization IDs, first-owner
subjects and `tenant_id` metadata remain the identity boundary. A readable name,
matching email, domain search or collision never grants access or adopts another
organization or human. The operator still checks the exact owner, home org,
email verification, native ownership, metadata and product grant before activation.
The trusted onboarding API owns immutable claim input; customers cannot write
claims or choose another tenant's native ID.

Normalize business-name separators to hyphens, retain letters, marks and ASCII
digits, and use `Workspace` if none remain. Keep the readable stem and its
lowercase domain representation within 55 UTF-8 bytes without splitting a rune.
This leaves room before Zitadel's 63-byte domain-label truncation for numeric
suffixes. Preserve the full submitted business name in its existing data owner;
the abbreviated identity label is not a replacement master.

First try the unsuffixed name. Zitadel's atomic name/domain constraints settle
concurrent claims. On HTTP 409, reread **only our deterministic organization ID**
first, recovering concurrent creation without duplicating a human. If still
absent, perform an exact, two-result-bounded default-domain lookup. Only a
confirmed different organization using that domain permits another candidate:
`Helivanta-Yes-Hospital-2`, then `-3`, through `-25`. Never use the collision
organization as the result. An unrelated user-ID conflict, absent/ambiguous
lookup, denied/unavailable dependency or exhausted candidates leaves the claim
not ready, with no product entitlement. The normal 30-second reconcile deadline
and workqueue retry/backoff still apply. No public name-availability API or
customer directory disclosure is added.

After a successful create or lost response, retries resolve the same immutable
ID and accept its assigned candidate. Existing exact raw business names and
UUID-prefixed legacy names remain accepted for that same tenant only. This
change never renames existing organizations, changes their domains, resends
verification, replaces an owner or resets credentials. The two already-created
customer organizations need a separately reviewed rename plan: native renames
also change the default domain and may affect domain-suffixed login names.

Reuse ADR 0004's planning envelope: up to 1 request/s burst, under 8 KiB claims,
100/1,000 organizations at months 12/36. Most creates add no round trips; each
confirmed collision adds one create, one ID read and one bounded domain lookup.
At most 25 candidates are tried within the existing attempt deadline. More than
25 identical normalized names requires a more specific workspace name; there is
no claim of automatic success for unlimited duplicates. No new service, table,
migration or infrastructure cost is introduced.

Deploy through the normal operator CI/release and Kargo-generated GitOps PR after
approval. Verify exact image readiness and fresh native creation separately;
provider test doubles do not prove production identity integration. Roll back by
reverting **new-name allocation while retaining candidate recognition**, then
releasing forward. An older binary that rejects readable names would block
reconciliation of organizations already created with them. Retain customer
claims and native users; deletion/recreation is not a rollback.

Tests cover readable names, same-name concurrent creates with distinct owners,
lost responses, Unicode/domain length, unrelated conflicts, unavailable lookup,
legacy replay and existing authorization regressions. Live creation and any
existing-org rename remain explicit acceptance gates.
