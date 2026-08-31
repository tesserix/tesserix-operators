package sandboxsync_test

import (
	"context"
	"testing"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	devai "github.com/tesserix/devai-sandbox-operator/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/internal/sandboxsync"
)

func TestReconcile_creates_a_suspended_safe_cronjob_with_secret_refs(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	if err := devai.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := batchv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	claim := &devai.SandboxDataSync{
		ObjectMeta: metav1.ObjectMeta{Name: "billing-evals", Namespace: "devai"},
		Spec: devai.SandboxDataSyncSpec{
			Schedule:                   "0 2 * * *",
			Source:                     devai.DatabaseReference{SecretRef: devai.SecretKeyReference{Name: "billing-readonly", Key: "url"}},
			Target:                     devai.DatabaseReference{SecretRef: devai.SecretKeyReference{Name: "billing-sandbox", Key: "url"}},
			AnonymizationSaltSecretRef: devai.SecretKeyReference{Name: "billing-salt", Key: "value"},
			Tables:                     []devai.TableRule{{Source: "public.customers", Target: "public.customers", Columns: []devai.ColumnRule{{Name: "email", Transform: "email"}}}},
		},
	}
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(claim).Build()
	reconciler := sandboxsync.NewReconciler(client, scheme, "ghcr.io/tesserix/devai-sandbox-sync:v0.1.0")

	if err := reconciler.Reconcile(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}); err != nil {
		t.Fatal(err)
	}

	job := &batchv1.CronJob{}
	if err := client.Get(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: "sandbox-sync-billing-evals"}, job); err != nil {
		t.Fatal(err)
	}
	if job.Spec.Schedule != "0 2 * * *" || job.Spec.ConcurrencyPolicy != batchv1.ForbidConcurrent {
		t.Fatalf("unexpected schedule policy: %#v", job.Spec)
	}
	container := job.Spec.JobTemplate.Spec.Template.Spec.Containers[0]
	if container.Image != "ghcr.io/tesserix/devai-sandbox-sync:v0.1.0" {
		t.Fatalf("unexpected image: %s", container.Image)
	}
	if container.SecurityContext == nil || container.SecurityContext.ReadOnlyRootFilesystem == nil || !*container.SecurityContext.ReadOnlyRootFilesystem {
		t.Fatal("sync worker must have a read-only root filesystem")
	}
	if len(container.Env) != 3 || container.Env[0].ValueFrom.SecretKeyRef.Name != "billing-readonly" || container.Env[1].ValueFrom.SecretKeyRef.Name != "billing-sandbox" || container.Env[2].ValueFrom.SecretKeyRef.Name != "billing-salt" {
		t.Fatalf("secret environment references were not wired: %#v", container.Env)
	}
}
