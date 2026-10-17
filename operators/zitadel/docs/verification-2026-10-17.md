# Identity onboarding verification — 2026-10-17

Production verification completed at 07:50 UTC through the authenticated browser,
registered product client, real operator and workload-authenticated activation API.
This record deliberately omits customer names, bindings, credentials and patient data.

## Observed results

- All 18 original project/application/machine claims kept their remote identifiers;
  all 22 configured platform identity claims were current and Ready.
- One existing hospital was adopted with its organization/tenant identifiers and
  two clinical memberships preserved. A genuinely new synthetic hospital reached
  Ready/Activated with its requested existing human as initial administrator.
- Both used the shared Helivanta project. The new organization required MFA and
  domain verification, disabled public registration/domain discovery and retained
  TOTP/passkey factors. Adoption preserved its existing login policy.
- Four authenticated activation replays retained exactly one append-only
  membership-verification marker per tenant. They changed neither memberships
  nor the held receipt generations.
- Each onboarding had a held intent and receipt: four objects/versions in total.
  Bucket versioning, default/event holds, six-year minimum retention, uniform
  access, public-access prevention and absence of expiry lifecycle were verified.
  The writer had scoped create/get access and no project-wide storage role.
- The OpenBao consumer read only its exact production credential at version 1.
  Other product/environment paths and the wrong namespace were denied; write,
  delete and list capabilities were absent. The OAuth principal matched Helivanta.
- Same-key replay retained the tenant and receipt generation; changed input
  returned 409. Product-machine adoption and foreign-product access returned 404.
  A product OAuth token could not authenticate as the internal workload caller.
- The approved projected workload token reached input validation (400 for an
  empty binding). Wrong audience, wrong account/namespace and repeated credential
  headers returned 401 without mutation.
- Operator, ingress policies, onboarding service and Helivanta API were Synced
  and Healthy. The final API revision was
  `44ae5ca24e9be51373a4ac5ff2bf54d85a6b4d78`, with both replicas updated/ready.

## Release and test evidence

- [Go 1.27.2/private onboarding operator](https://github.com/tesserix/tesserix-operators/pull/19),
  [controller diagnostics](https://github.com/tesserix/tesserix-operators/pull/22).
- [Onboarding workload transport](https://github.com/tesserix/onboarding-service/pull/17),
  [adoption bootstrap safeguard](https://github.com/tesserix/onboarding-service/pull/18).
- [Helivanta workload transport](https://github.com/tesserix/helivanta/pull/1195),
  [atomic durable membership safeguard](https://github.com/tesserix/helivanta/pull/1196).
- [Ingress admission](https://github.com/tesserix/tesserix-k8s/pull/1439),
  [operator release](https://github.com/tesserix/tesserix-k8s/pull/1440),
  [leader-elected rolling updates](https://github.com/tesserix/tesserix-k8s/pull/1441).

Helivanta's required PR checks and onboarding's release CI passed, including Go,
web, browser E2E, image and secret checks. API race tests
ran against PostgreSQL; provisioning/adoption race regressions used PostgreSQL
and NATS. Vet, build, scoped lint, chart rendering and hospital contract checks
passed. Local Colima full-IAM runs hit existing cache-stress deadlines; one
isolated retry passed, and an extra cold Linux compilation was interrupted.
These local attempts are not represented as passing full local suites. The
unchanged tests passed in required CI for the production source tree.

## Limits

See [security-assurance.md](security-assurance.md). This is implementation and
smoke-test evidence, not SOC 2 attestation, HIPAA certification, a penetration test
or a disaster restore. Existing/home-organization MFA cutover, BAAs, access reviews,
incident response, recovery drills and retirement of transitional privileged
bootstrap sources still require their separate operational evidence. Privileged
administrators can alter IAM/holds; absolute administrator-proof undeletability
is not claimed.
