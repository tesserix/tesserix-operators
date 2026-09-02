package analyticsonboarding_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	analyticsv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/openpanel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/openpanel/internal/analyticsonboarding"
	"github.com/tesserix/devai-sandbox-operator/operators/openpanel/internal/openpanelapi"
)

func TestReconcileEnsuresProjectAndMirrorsClientIDToDerivedSecret(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := analyticsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	claim := &analyticsv1alpha1.AnalyticsOnboarding{
		ObjectMeta: metav1.ObjectMeta{Name: "langfuse", Namespace: "analytics-operator"},
		Spec: analyticsv1alpha1.AnalyticsOnboardingSpec{
			DisplayName: "Langfuse",
			Domain:      "https://langfuse.tesserix.app",
			CORS:        []string{"https://langfuse.tesserix.app"},
			Types:       []string{"website"},
		},
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(claim).WithObjects(claim).Build()
	projects := &fakeProjects{result: openpanelapi.Result{ProjectID: "project-langfuse", ClientID: "client-123"}}
	secrets := &fakeSecrets{}
	reconciler := analyticsonboarding.NewReconciler(kube, projects, secrets, "prod-openpanel-")

	if err := reconciler.Reconcile(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}); err != nil {
		t.Fatal(err)
	}
	if projects.recordedID != "" || projects.input.Name != "Langfuse" || projects.input.Domain != claim.Spec.Domain {
		t.Fatalf("project input = %#v, recorded id = %q", projects.input, projects.recordedID)
	}
	if secrets.name != "prod-openpanel-langfuse-client-id" || secrets.value != "client-123" {
		t.Fatalf("secret = %q, value = %q", secrets.name, secrets.value)
	}

	stored := &analyticsv1alpha1.AnalyticsOnboarding{}
	if err := kube.Get(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}, stored); err != nil {
		t.Fatal(err)
	}
	if stored.Status.ProjectID != "project-langfuse" || stored.Status.ClientIDSecret != "prod-openpanel-langfuse-client-id" {
		t.Fatalf("status = %#v", stored.Status)
	}
	if len(stored.Status.Conditions) != 1 || stored.Status.Conditions[0].Status != metav1.ConditionTrue {
		t.Fatalf("conditions = %#v", stored.Status.Conditions)
	}
}

func TestReconcileRejectsOriginsWithPathsWithoutCallingExternalSystems(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := analyticsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	claim := &analyticsv1alpha1.AnalyticsOnboarding{
		ObjectMeta: metav1.ObjectMeta{Name: "devai", Namespace: "analytics-operator"},
		Spec: analyticsv1alpha1.AnalyticsOnboardingSpec{
			DisplayName: "DevAI",
			Domain:      "https://devai.tesserix.app/dashboard",
			CORS:        []string{"https://devai.tesserix.app"},
		},
	}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(claim).WithObjects(claim).Build()
	projects := &fakeProjects{}
	reconciler := analyticsonboarding.NewReconciler(kube, projects, &fakeSecrets{}, "prod-openpanel-")

	if err := reconciler.Reconcile(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}); err != nil {
		t.Fatalf("invalid claims should be terminal, got %v", err)
	}
	if projects.calls != 0 {
		t.Fatalf("external project calls = %d, want 0", projects.calls)
	}
	stored := &analyticsv1alpha1.AnalyticsOnboarding{}
	if err := kube.Get(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}, stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Status.Conditions) != 1 || stored.Status.Conditions[0].Status != metav1.ConditionFalse {
		t.Fatalf("conditions = %#v", stored.Status.Conditions)
	}
}

type fakeProjects struct {
	result     openpanelapi.Result
	recordedID string
	input      openpanelapi.ProjectInput
	calls      int
}

func (f *fakeProjects) EnsureProject(_ context.Context, recordedID string, input openpanelapi.ProjectInput) (openpanelapi.Result, error) {
	f.calls++
	f.recordedID = recordedID
	f.input = input
	return f.result, nil
}

type fakeSecrets struct {
	name  string
	value string
}

func (f *fakeSecrets) Ensure(_ context.Context, name, value string) error {
	f.name = name
	f.value = value
	return nil
}
