package evalonboarding_test

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	evalsv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/evals/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/evals/internal/evalonboarding"
	"github.com/tesserix/devai-sandbox-operator/operators/evals/internal/evalstore"
	"github.com/tesserix/devai-sandbox-operator/operators/evals/internal/langfuseapi"
)

type fakeLangfuse struct {
	project    langfuseapi.Project
	recordedID string
	listed     []langfuseapi.APIKey
	created    int
	err        error
}

func (f *fakeLangfuse) EnsureProject(_ context.Context, recordedID, name string, _ map[string]string) (langfuseapi.Project, error) {
	f.recordedID = recordedID
	if f.err != nil {
		return langfuseapi.Project{}, f.err
	}
	if f.project.ID == "" {
		f.project = langfuseapi.Project{ID: "proj-" + name, Name: name}
	}
	return f.project, nil
}

func (f *fakeLangfuse) ListAPIKeys(context.Context, string) ([]langfuseapi.APIKey, error) {
	return f.listed, nil
}

func (f *fakeLangfuse) CreateAPIKey(context.Context, string, string) (langfuseapi.APIKey, error) {
	f.created++
	return langfuseapi.APIKey{ID: "key", PublicKey: "pk-lf-new", SecretKey: "sk-lf-new"}, nil
}

type fakeSecrets struct {
	values map[string]string
	writes []string
}

func (f *fakeSecrets) Latest(_ context.Context, name string) (string, bool, error) {
	value, ok := f.values[name]
	return value, ok, nil
}

func (f *fakeSecrets) Ensure(_ context.Context, name, value string) error {
	if f.values == nil {
		f.values = map[string]string{}
	}
	f.values[name] = value
	f.writes = append(f.writes, name)
	return nil
}

type fakeDatasets struct {
	product  string
	datasets []evalstore.Dataset
	calls    int
}

func (f *fakeDatasets) Upsert(_ context.Context, product string, datasets []evalstore.Dataset) error {
	f.calls++
	f.product = product
	f.datasets = datasets
	return nil
}

func newClaim(name string, datasets ...evalsv1alpha1.Dataset) *evalsv1alpha1.EvalOnboarding {
	return &evalsv1alpha1.EvalOnboarding{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "evals-operator"},
		Spec:       evalsv1alpha1.EvalOnboardingSpec{DisplayName: "Kora", Datasets: datasets},
	}
}

func build(t *testing.T, claim *evalsv1alpha1.EvalOnboarding) (*evalonboarding.Reconciler, *fakeLangfuse, *fakeSecrets, *fakeDatasets, func() *evalsv1alpha1.EvalOnboarding) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := evalsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(claim).WithObjects(claim).Build()
	langfuse, secrets, datasets := &fakeLangfuse{}, &fakeSecrets{}, &fakeDatasets{}
	reconciler := evalonboarding.NewReconciler(kube, langfuse, secrets, datasets, "prod-")
	stored := func() *evalsv1alpha1.EvalOnboarding {
		out := &evalsv1alpha1.EvalOnboarding{}
		if err := kube.Get(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}, out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	return reconciler, langfuse, secrets, datasets, stored
}

func TestReconcileCreatesKeyMirrorsSecretsAndRegistersDatasets(t *testing.T) {
	t.Parallel()
	claim := newClaim("kora", evalsv1alpha1.Dataset{Name: "coaching-golden", Modality: "agent", Description: " coaching "})
	reconciler, langfuse, secrets, datasets, stored := build(t, claim)

	if err := reconciler.Reconcile(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}); err != nil {
		t.Fatal(err)
	}
	if langfuse.created != 1 || len(secrets.writes) != 2 || secrets.writes[0] != "prod-kora-langfuse-secret-key" || secrets.writes[1] != "prod-kora-langfuse-public-key" {
		t.Fatalf("created = %d, writes = %v", langfuse.created, secrets.writes)
	}
	if secrets.values["prod-kora-langfuse-public-key"] != "pk-lf-new" || secrets.values["prod-kora-langfuse-secret-key"] != "sk-lf-new" {
		t.Fatalf("values = %v", secrets.values)
	}
	if datasets.product != "kora" || len(datasets.datasets) != 1 || datasets.datasets[0].Description != "coaching" {
		t.Fatalf("datasets = %#v for %q", datasets.datasets, datasets.product)
	}
	status := stored().Status
	if status.ProjectID != "proj-Kora" || status.PublicKeySecret != "prod-kora-langfuse-public-key" || status.DatasetsRegistered != 1 {
		t.Fatalf("status = %#v", status)
	}
	if len(status.Conditions) != 1 || status.Conditions[0].Status != metav1.ConditionTrue {
		t.Fatalf("conditions = %#v", status.Conditions)
	}
}

func TestReconcileKeepsExistingKeyWhenLangfuseStillListsIt(t *testing.T) {
	t.Parallel()
	claim := newClaim("devai")
	claim.Status.ProjectID = "devai"
	reconciler, langfuse, secrets, datasets, _ := build(t, claim)
	langfuse.project = langfuseapi.Project{ID: "devai", Name: "DevAI"}
	langfuse.listed = []langfuseapi.APIKey{{ID: "k", PublicKey: "pk-lf-existing"}}
	secrets.values = map[string]string{"prod-devai-langfuse-public-key": "pk-lf-existing", "prod-devai-langfuse-secret-key": "sk-lf-existing"}

	if err := reconciler.Reconcile(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}); err != nil {
		t.Fatal(err)
	}
	if langfuse.recordedID != "devai" || langfuse.created != 0 || len(secrets.writes) != 0 {
		t.Fatalf("recorded = %q, created = %d, writes = %v", langfuse.recordedID, langfuse.created, secrets.writes)
	}
	if datasets.calls != 1 || len(datasets.datasets) != 0 {
		t.Fatalf("datasets = %#v", datasets)
	}
}

func TestReconcileRotatesWhenLangfuseNoLongerListsMirroredKey(t *testing.T) {
	t.Parallel()
	claim := newClaim("kora")
	reconciler, langfuse, secrets, _, _ := build(t, claim)
	langfuse.listed = []langfuseapi.APIKey{{ID: "k", PublicKey: "pk-lf-other"}}
	secrets.values = map[string]string{"prod-kora-langfuse-public-key": "pk-lf-revoked", "prod-kora-langfuse-secret-key": "sk-lf-revoked"}

	if err := reconciler.Reconcile(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}); err != nil {
		t.Fatal(err)
	}
	if langfuse.created != 1 || secrets.values["prod-kora-langfuse-public-key"] != "pk-lf-new" {
		t.Fatalf("created = %d, values = %v", langfuse.created, secrets.values)
	}
}

func TestReconcileRejectsInvalidDatasetsWithoutCallingExternalSystems(t *testing.T) {
	t.Parallel()
	claim := newClaim("kora", evalsv1alpha1.Dataset{Name: "Bad Name", Modality: "agent"})
	reconciler, langfuse, _, datasets, stored := build(t, claim)

	if err := reconciler.Reconcile(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}); err != nil {
		t.Fatalf("invalid claims should be terminal, got %v", err)
	}
	if langfuse.recordedID != "" || langfuse.created != 0 || datasets.calls != 0 {
		t.Fatal("external systems were called for an invalid claim")
	}
	conditions := stored().Status.Conditions
	if len(conditions) != 1 || conditions[0].Status != metav1.ConditionFalse || conditions[0].Reason != "ReconcileFailed" {
		t.Fatalf("conditions = %#v", conditions)
	}
}

func TestReconcileRetriesTransientLangfuseFailures(t *testing.T) {
	t.Parallel()
	claim := newClaim("kora")
	reconciler, langfuse, _, _, stored := build(t, claim)
	langfuse.err = &langfuseapi.StatusError{Code: 503}

	err := reconciler.Reconcile(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name})
	if err == nil || !errors.Is(err, langfuse.err) {
		t.Fatalf("expected the transient error to be returned, got %v", err)
	}
	if stored().Status.Conditions[0].Status != metav1.ConditionFalse {
		t.Fatal("expected a not-ready condition")
	}
}
