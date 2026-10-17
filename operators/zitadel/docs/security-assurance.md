# Identity control assurance: SOC 2 and HIPAA

This is an implementation/evidence checklist, not a certification or a legal
compliance assertion. SOC 2 attests operating controls over a defined system
and period. HIPAA requires covered-entity/business-associate analysis, risk
assessment, administrative/physical safeguards and appropriate BAAs in addition
to technical safeguards. Scope includes identity, Helivanta, infrastructure,
vendors, personnel and operations. The deployment must not be described as
"SOC 2 certified" or "HIPAA compliant" on the basis of these changes.

| Control | Implementation and evidence to retain | Remaining organizational evidence |
| --- | --- | --- |
| Unique identity and minimum access (HIPAA 164.312(a), SOC 2 CC6) | Stable Zitadel subject; distinct org/tenant bindings; verified first human; explicit memberships; per-resource permission checks; live ORG_OWNER check | Periodic workforce access review, termination SLA, approved emergency-access process |
| Group/branch and independent-entity isolation (CC6) | One org per administration boundary; group resources have separate hospital/pharmacy/lab permissions; tenant RLS; explicit sharing; foreign-product and tenant denial tests | Representative production-like clinical isolation and abuse testing; independent penetration test |
| MFA and sessions (164.312(a), CC6) | Strict new-org policy forces MFA, disallows public registration/discovery and requires domain verification, removes MFA skip period, caps reauthentication lifetime; product sessions preserve original auth time and idle deadlines | Inherited/adopted orgs and users' home orgs need a verified MFA cutover; federated MFA assurance; break-glass enrollment/recovery drills |
| Provisioning integrity (CC6, CC8) | Authorized product or platform request; existing UUID adoption is platform-only; deterministic create/retry; atomic append-only membership marker blocks re-grant after status loss; adoption never bootstraps missing membership; no silent user transfer/admin replacement; no remote API calls in DB transactions | Approver/change-management evidence and monitored onboarding failure response |
| Service identity and secrets (CC6) | Audience-bound short-lived Kubernetes tokens, exact account check, private claims RBAC; credential values go only to reviewed OpenBao paths; image/CRD validation | Existing shared management credential remains a privileged transitional source; separate operational-key migration/cold-start/recovery evidence before retirement |
| Audit controls (164.312(b), CC7) | Mandatory request attribution; projection and entitlement audit transaction; completion audit; immutable held GCS intent/receipt; no tokens/PHI in YAML | Audit export/access reviews, monitoring and retention verification; emergency-access audit |
| Integrity/transmission (164.312(c),(e), CC6) | Exact issuer/audience/signature validation; PKCE; no credential-forwarding redirects; TLS to public services; ambient mTLS on control-plane path; strict service tokens | Vendor and complete internal-network encryption review, certificate operations, BAA applicability for Google/Cloudflare/other processors |
| Availability and recovery (164.308(a)(7), CC7, A1) | Forward retry, fail closed/pending, existing login independent of operator, retained configuration and no automatic destructive cleanup | Signed RPO/RTO, restore and disaster-recovery exercises covering database, OpenBao, Zitadel and audit stores |
| Secure delivery (CC8) | Go 1.27.2, pinned base images, race tests, vet/build/lint/vulnerability checks, immutable deployment images and reviewed GitOps | CI evidence links, change approver, vulnerability response process and ongoing scanning |
| Privacy/retention (164.316(b), C1) | Config records omit credentials/PHI, preserve versions and holds; no expiration lifecycle, writer lacks delete/hold release | Legal retention schedule and data inventory. HIPAA's six-year documentation requirement is not a universal medical-record retention rule. |

The intended trust boundary is explicit: a product caller may request only its
own configured product; a customer login cannot claim platform or org-owner
privileges. A clinician shared across tenants receives separate product and
resource memberships. The hospital selected in the UI never establishes access.
An org grant is an authentication entitlement and is not clinical authorization.

The [2026-10-17 verification record](verification-2026-10-17.md) lists the observed
production adoption, new onboarding, credential isolation and archive checks.

GCS holds protect against the routine writer and accidental deletion. Platform
administrators can ultimately change IAM and release holds; an irreversible
retention lock and organizational retention policy are separate decisions.
Do not use real patient data in onboarding or restore verification.
