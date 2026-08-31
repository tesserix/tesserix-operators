# DevAI sandbox data operator guide

## What is deployed

The `devai-sandbox-operator` runs in the `devai` namespace. A `SandboxDataSync` claim causes it to create a same-namespace ConfigMap and CronJob. The CronJob runs the sync worker at `spec.schedule`, forbids overlapping runs, retains three job histories, and has a one-hour deadline.

Claims are stored in GitOps at `operators/claims/db-anonymise/<product>.yaml`. The Kora template is `operators/claims/db-anonymise/kora.yaml`.

## Required target before enabling a claim

The target must be a dedicated sandbox CNPG cluster or logical database, never the product's production database. Create it with a separate GitOps change and then provide a namespace-local Secret named `kora-sandbox-sync` containing `source-url`, `target-url`, and `anonymization-salt`. The source database role must be `SELECT`-only. The target role must be limited to the sandbox database.

The Kora production `kora-postgres` cluster is not a valid target. As of 2026-08-31, no Kora sandbox CNPG cluster or `kora-sandbox-sync` Secret exists, so the Kora claim is intentionally not applied.

## Anonymization policy

Every copied column needs an explicit transform. `email`, `name`, `hash`, and `redact` remove raw personal data. `preserve` is only for fields classified non-sensitive. The salt makes transformed values deterministic within one sandbox so evaluation joins work without retaining the source values.

For Kora users, `firebase_uid`, `email`, `display_name`, and `apple_refresh_token` must not be preserved. Review every additional table and all foreign-key dependencies before adding it to the claim.

## Verification

After the target Secret exists and the claim has been added to the operator Kustomization, verify the generated CronJob:

```sh
kubectl -n kora get cronjob sandbox-sync-kora-evals
kubectl -n kora get cronjob sandbox-sync-kora-evals -o jsonpath='{.spec.schedule}{"\n"}'
```

The expected schedule is `0 2 * * *` (02:00 in the Kubernetes controller time zone). Test the worker without waiting a day by creating a one-off Job from the CronJob, then inspect only job status and sanitized metrics/logs:

```sh
kubectl -n kora create job --from=cronjob/sandbox-sync-kora-evals kora-evals-manual
kubectl -n kora wait --for=condition=complete job/kora-evals-manual --timeout=1h
```

Do not print database URLs, Secret values, or copied rows. A failed job leaves the previous sandbox dataset in place; investigate the job event and re-run only after correcting the GitOps configuration.
