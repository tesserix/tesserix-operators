# Secret storage

Use in-cluster OpenBao for new and existing application and operational secrets.
Use product-prefixed identifiers, separate environment paths, namespace-bound
identities and exact-path least-privilege policies. Update writers, rotation jobs
and readers together. Never introduce new GCP Secret Manager dependencies.

Existing GCP fallbacks are transitional and must be removed only after the
corresponding consumer, recovery and deletion checks pass. OpenBao bootstrap and
recovery material must remain independently recoverable outside OpenBao.
