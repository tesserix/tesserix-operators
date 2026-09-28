# ADR 0002: Analytics onboarding writes client IDs to OpenBao

## Context

Two production claims (DevAI and Langfuse) currently reconcile public analytics
client IDs. Payloads are below 1 KiB each; writes occur only when an ID changes.
Assume at most two concurrent reconciliations and fewer than one steady-state
write per minute. Growth requires adding reviewed product mappings and policies,
not widening a wildcard grant. No measured onboarding latency SLO exists; the
migration acceptance is both claims Ready and existing IDs unchanged.

An active GCP writer recreated a source after its reader had moved to OpenBao.
The trust boundary is the namespace-bound operator identity accessing product
secret paths. A compromised claim or operator must not read unrelated secrets
or write outside its two reviewed destinations. Root OpenPanel credentials are
mounted by ESO through a separate, read-only identity.

## Decision

Use the existing OpenBao KV-v2 adapter shared with evals. Analytics supplies no
GCP fallback. Validate claim names against a reviewed product map before making
OpenPanel calls. Store IDs under `<product>/app/<product>-openpanel-client-id`.
The production role permits create/read/update only for those two paths, with
five-minute tokens and self-revocation. It has no delete capability.

Reads compare existing values before writing. Writes use check-and-set, so
concurrent updates fail and retry instead of silently overwriting a new version.
Identical values cause no write. Each outbound HTTP request has a ten-second
timeout; authentication tokens are revoked with a separate five-second deadline.
An OpenBao outage marks the claim unready and controller-runtime retries. A
retry finds the OpenPanel project by ID or name and reuses its write client;
there is no fallback to GCP and no compensation that deletes the project.

## Migration and rollback

Deploy exact-path policies and network egress first. Archive pinned GCP sources,
verify their equality with existing OpenBao values and the OpenPanel API, then
roll out the operator and switch root-credential readers through GitOps. The
binary accepts old GCP flags but ignores them during this rollout. Verify both
claims, fresh readers and isolated restore before deleting originals and GCP
grants. A rollback to the old binary requires restored GCP sources and grants;
after retirement use a forward fix or an explicitly reviewed recovery.

No new service or storage system is introduced. Storage is two tiny records plus
bounded KV history; no material compute increase is expected. Leaving the GCP
writer active was rejected because it recreates retired dependencies. A new
workflow engine is unnecessary for this existing idempotent reconciliation loop.
