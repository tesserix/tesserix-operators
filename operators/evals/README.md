# Evals onboarding operator

`EvalOnboarding` claims give a product everything ADR-0003 (australis) needs
before it can be graded: a Langfuse project, a project API key pair mirrored
into Google Secret Manager, and its golden datasets registered in
`eval.datasets` on `evals_db`.

Secret Manager targets are derived as `<prefix><claim>-langfuse-public-key`
and `<prefix><claim>-langfuse-secret-key`, so the existing `prod-devai-*` keys
are adopted rather than replaced. A new key pair is minted only when Langfuse
no longer lists the mirrored public key or either half is missing. Datasets
are upserted on `(product, name)` with the claim name as the product.

```yaml
apiVersion: evals.tesserix.app/v1alpha1
kind: EvalOnboarding
metadata:
  name: kora
  namespace: evals-operator
spec:
  displayName: Kora
  datasets:
    - {name: coaching-golden, modality: agent}
    - {name: food-retrieval-golden, modality: retrieval}
```

The operator authenticates to Langfuse with an organization-scoped key pair
mounted from External Secrets and reread on every request, and to `evals_db`
as role `grader` with a mounted password. It never deletes Langfuse projects,
API keys, GCP secrets or dataset rows; removal is a separately approved action.
