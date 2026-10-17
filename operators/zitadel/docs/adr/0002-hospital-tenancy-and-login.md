# ADR 0002: Hospital identity boundaries and custom login

Status: accepted; runtime customer-claim decision superseded by ADR 0003 for operator capabilities; runtime hospital provisioning and
production policy rollout remain pending.

## Context and threat model

Identity credentials, hospital membership and patient data are the assets.
Attackers include unauthenticated users, another hospital's users, a compromised
login client and an overprivileged tenant administrator. Boundaries separate
the public browser, Helivanta session service, Zitadel organization/project,
OpenFGA and tenant database. Every crossing must authenticate and authorize the
specific tenant/object; an organization hint or email is never sufficient.

The existing operator planning envelope is fewer than ten reconciliations per
minute with small JSON payloads, a 30-second attempt deadline and a five-minute
drift interval. No production hospital signup traffic, user count, 12/36-month
data volume or auth latency/availability SLO was measured. These remain input
requirements for the runtime onboarding service, not invented sizing claims.
This change adds no service, database or dedicated per-hospital deployment;
control-plane compute cost remains the existing single small operator.

## Decision

Use one customer organization per independent hospital identity administration
boundary; a hospital group may share one only deliberately. Keep one shared
Helivanta product project and a stable set of application clients. Use project
grants for coarse product entitlement and Helivanta/OpenFGA/RLS for membership
and clinical authorization. Cross-hospital memberships are explicit application
permissions, not inferred from home organization or email.

The operator manages product resources and reviewed service identities. Runtime
hospital signup belongs to a durable product workflow, never public GitOps
claims containing customer directories. See [the onboarding contract](../helivanta-onboarding.md).

Add `access.mode: organization` without using the exact-user-list `restricted`
mode. Preserve existing organization gates when reconciling other controls and
preserve adopted project policies when access is omitted. New restricted/org
projects must be gated on creation. Organization ownership and application
project references are immutable claim bindings.

Support Web/None PKCE clients and per-application Login V2 base URIs. Preserve
unmanaged login/logout/branding settings while healing declared OIDC drift.
Token type and ID-token userinfo assertion are configurable, preserving the
existing Helivanta contract. New clients explicitly use Login V2; adopting one does not globally change the
instance login policy. Existing refresh grants survive an omitted setting.
Only trusted platform configuration may select a hosted UI or adopt a client.

Machine-user claims must match their reviewed account names and exact OpenBao
destinations. New mappings separate prod/dev/uat. Legacy paths remain until
readers, writers and recovery migrate together. Missing published credentials
fail closed rather than rotate consumers out of service.

## Alternatives

A project per hospital was rejected: it fragments product configuration without
providing separate directories, SSO administration and policies. A single org
for all unrelated hospitals was rejected: tenant identity administration and
recovery policies would share a boundary. Kubernetes hospital CRDs were rejected
because signup is customer state and must not depend on a public infrastructure
PR or expose customer identities there.

## Failure, migration and rollback

Zitadel/OpenBao/DB/OpenFGA outages leave onboarding pending and authorization
denied; the product login path never depends on the Kubernetes operator.
Successful reconciliation rechecks drift periodically without status loops;
attempts are bounded. Leader election and first-upgrade Recreate prevent
concurrent credential writers. Exact-list restricted projects must remain a
platform-only tool, since they deliberately remove grants outside the list.

Introduce CRDs and lease RBAC before the image. Adopt Helivanta with unchanged
settings first; establish existing customer grants before enabling the org gate.
No default-instance security change or existing-user migration is included.
Rollback the operator image independently of additive CRDs; configuration
changes need a captured-state restoration plan. Deletion/rotation/production
rollout require specific approval. Restore RPO/RTO, runtime onboarding SLOs,
full product isolation tests and production image verification are outstanding.

[ADR 0003](0003-private-hospital-onboarding.md) supersedes the rejection of private runtime organization claims and defines hospital groups and shared users. Customer records remain excluded from public GitOps.
