# ADR 0004: A first human owner for a new customer organization

Status: implementation candidate; not deployed. Extends ADR 0003.

The existing workflow requires a previously verified human subject. A customer
without a Zitadel account cannot satisfy that prerequisite. Keep the existing activation callback. The customer journey is automated after
verified signup; platform review is not an activation prerequisite. Add an optional `initialAdmin` profile to
the private immutable organization claim, alongside `initialAdminUserId`.
For this mode the subject must be `owner-<tenantId>` and the organization must be
new, never an adoption. The operator uses Zitadel v2's atomic organization plus
inline human creation, requesting native email verification with no password,
returned code or fabricated verification. Native Login V2 owns password setup,
recovery and MFA; Helivanta never handles a password. Activation waits for an
active human with the expected subject, home organization and verified email.
An invitation is not proof that a person accepted it.

The profile contains the owner's supplied given name, family name and email.
The private claim is platform-only customer data, never GitOps. Names are not
invented; support for customers without a family name needs the identity owner's
supported name model before accepting that case. Profiles are bounded to the
identity API's 200-character limits, contain no control characters and use a
plain email address. They are excluded from held archive payloads; an intent
fingerprint binds the exact immutable original spec without retaining cleartext
contact/name data in the six-year configuration archive. Subject IDs remain
personal identifiers and retain ADR 0003's access/retention review requirement.

Assets are owner identity, tenant isolation, contact data and administrator
privileges. Unauthenticated callers and customer tenants cannot write claims;
the onboarding API binds the first verified registrant as owner and the operator verifies the
native account before product activation. Neither a caller-selected subject nor
matching display name substitutes for matching the home organization and email.
Existing-owner creation and adoption retain their current behavior.

A lost create response retries the same organization/subject IDs. Reconciliation
never searches by email to adopt a person, never sends a new code on every poll,
and never deletes or replaces an owner. Pending verification is a visible
not-ready condition and the normal five-minute drift check revisits it. Identity
or archive outages leave activation blocked. A conflicting account or tenant
binding fails closed; correction needs a reviewed migration, not overwriting.

Use ADR 0003's 1 request/s burst, <8 KiB requests, 100/1,000 organizations at
12/36 months and 30-second reconciliation deadline. Noninteractive identity
completion targets p99 two minutes; human verification has no automatic success
or expiry claim. Pending discovery can take five minutes after email validation.
No new runtime or datastore is introduced; storage/API calls are within the
existing control-plane budget. The simpler existing-user-only option is rejected
because it leaves a brand-new customer unable to onboard.

Roll out additive claim schema first, then operator, then onboarding API and
owner administration/public journey. Older callers omit the profile and continue unchanged.
Rollback stops new-profile submission before reverting the controller; keep new
pending claims and remote organizations for forward recovery. No database
migration or deletion is part of this change. Public intake, automatic hospital/pharmacy setup, owner security administration,
profile collection and live email/login acceptance are separate integration gates;
this operator change alone does not complete customer onboarding.

Archive recovery of a new-profile claim requires resubmitting the owner profile
through its authorized data owner and matching the retained fingerprint; a
redacted archive alone cannot reconstruct the full customer profile.
