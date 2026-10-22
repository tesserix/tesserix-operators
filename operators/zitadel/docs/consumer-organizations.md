# Consumer organizations and Login V2

Every operator-managed native, web, and user-agent application uses Login V2.
Reconciliation upgrades missing/legacy login-version settings, preserves an
existing custom V2 `baseUri` when the claim omits `loginBaseUri`, and applies an
explicitly declared URI when present. App IDs, client IDs, redirect URIs, and
unmanaged OIDC settings are preserved. Unclaimed applications are not changed.

A public `ZitadelProject` can opt into `spec.consumerOrganization`. This creates
one isolated organization with ID `consumer-<claim-name>`, using the project's
`organization` as its display name. TESSERIX and ZITADEL are reserved names.
Hospital/tenant provisioning continues to use `ZitadelOrganization` separately.

The consumer claim declares an active, verified human administrator, theme colors,
and Google/Apple/Facebook provider references. The administrator retains org-owner
access through their existing account. The operator rejects missing credentials
before organization creation; it never changes instance or shared TESSERIX policy.
An ownership marker binds the org to `<namespace>/<claim-name>`. A retry after an
interrupted creation can recover the deterministic org only when its exact name,
state, and initial org owner match. Conflicting ownership stops reconciliation.

The org policy disables username/password login, manual signup, passkeys,
password reset, and domain discovery. Only the declared social providers are
attached. Provider-based first-login account creation is enabled; automatic
account linking and profile overwrites are disabled. Social accounts remain
separate unless an explicit, verified linking feature is added.

Provider options, public settings, policy, and branding are reconciled for drift.
Credential digests in status avoid rewriting unchanged credentials. Changing an
OpenBao value triggers the provider update; errors never include secret content.
Status contains organization/provider IDs for consuming products. Clients request
`urn:zitadel:iam:org:id:<orgId>` and `urn:zitadel:iam:user:resourceowner`; product
APIs must verify the resource-owner claim, issuer, client audience, and nonce.

## OpenBao bindings

Set `CONSUMER_PROVIDER_SECRETS` to reviewed, comma-separated
`product/environment/product-prefixed-secret` bindings. Environments are `dev`,
`uat`, or `prod`; legacy paths without an environment are rejected. The mapping
resolves to `kv/<product>/<environment>/app/<secret>`.

`CONSUMER_PROVIDER_OPENBAO_ROLE` selects a read-only, namespace-bound Kubernetes
role. `CONSUMER_PROVIDER_WORKLOAD_JWT_FILE` should use a projected, short-lived token with
an audience matching the OpenBao role. It is independent of the existing
machine-user writer token, whose audience and readers remain unchanged. A new product needs explicit reviewed
bindings and exact read-only paths, rather than a wildcard permission.

Each referenced OpenBao KV v2 value has the existing store's string field `value`,
containing JSON with the following keys (never commit credential values):

- Google: `clientId`, `clientSecret`.
- Apple: `clientId` (Services ID), `teamId`, `keyId`, `privateKey` (base64 PKCS#8 PEM).
- Facebook: `clientId`, `clientSecret`. The operator fixes the HTTPS Graph v26.0
  authorization/token/profile endpoints and requests `public_profile,email`.

All provider callback URLs are the broker's registered callback, not the native
app callback. Native clients use authorization code with PKCE and no client secret.

## Meta on V2

Upstream v4.19.5 Generic OAuth maps the external subject but leaves profile fields
empty. The minimal broker patch in `tesserix/zitadel`, branch
`feat/civiclens-social-v2`, maps typed standard/Meta profile attributes while
keeping email verification false. This is a V2-compatible backend mapping;
legacy V1 actions are not used. A Meta account must grant email access and may
need broker email verification. Missing profile data fails through the hosted V2
flow; the product must not advertise a working provider until live verification.

Release the patched v4.19.5 broker and the operator before reconciling consumer
claims. Populate the product-scoped credentials, then verify Ready receipts,
V2 authorization redirects, first and returning social logins, no password/signup
controls, and exact native callbacks. A successful claim is not proof that
provider-side app approval or Apple Services ID configuration is complete.
