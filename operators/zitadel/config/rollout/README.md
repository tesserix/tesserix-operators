# Owning GitOps prerequisites

`k8s-prerequisites.patch` was prepared against the current GitHub copy of
`tesserix/tesserix-k8s`, path `k8s/operators/zitadel/resources.yaml`, on
2026-10-17. The baseline Git blob hash was
`ffb213cc838f0a82f4c84f5c88c5ceb82af0f367`.

It updates the three CRDs from this repository, adds namespaced leader-election
lease permissions and uses Recreate for the first upgrade from the controller
that does not participate in leader election. The Deployment opts out of SSA
per resource and explicitly clears `rollingUpdate`, because server dry-run
verification showed SSA retaining the old rolling-update fields. The actual
Deployment strategy change passed client-side apply with server dry run; CRDs
and the remaining prerequisites passed SSA with server dry run. Existing machine-user opt-ins,
credential sources, workload settings and claims are retained. It does not
change an image digest or activate Helivanta claims or hospital security policy.

The patch was checked and applied only in an isolated temporary copy. The user
checkout was not modified. Review it against a refreshed owning checkout:

```sh
git apply --check /absolute/path/to/k8s-prerequisites.patch
git apply /absolute/path/to/k8s-prerequisites.patch
```

These commands change local files only. Review the result before any requested
commit/release. Install compatible CRDs and lease RBAC before the new controller
image. Publish and pin the CI-produced image for the production node platform
through the existing release/Kargo path; the local review image is not a
production rollout artifact. Do not apply `resources.yaml` directly: Argo's
kustomization owns the deployed image mapping and claims.

Production deployment still requires specific approval. See
[the hospital onboarding and rollout guide](../../docs/helivanta-onboarding.md).
