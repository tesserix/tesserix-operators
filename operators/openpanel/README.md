# Analytics onboarding operator

`AnalyticsOnboarding` claims create or adopt an OpenPanel project, reconcile its
name/domain/CORS through OpenPanel's Manage API, ensure a write client, and
mirror that client ID into Google Secret Manager.

The Secret Manager target is derived as
`<configured-prefix><claim-name>-client-id`. Claims cannot select an arbitrary
secret, and neither management credentials nor client values are written to
the claim. The operator rereads mounted management credentials on every API
request so External Secrets rotation does not require a restart.

```yaml
apiVersion: analytics.tesserix.app/v1alpha1
kind: AnalyticsOnboarding
metadata:
  name: langfuse
  namespace: analytics-operator
spec:
  displayName: Langfuse
  domain: https://langfuse.tesserix.app
  cors: [https://langfuse.tesserix.app]
  types: [website]
```

The operator does not delete OpenPanel projects or GCP secrets when a claim is
deleted. Removal is intentionally a separately approved destructive action.
