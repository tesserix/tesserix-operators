package analyticsonboarding_test

import (
	"context"
	"errors"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	analyticsv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/openpanel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/openpanel/internal/analyticsonboarding"
	"github.com/tesserix/devai-sandbox-operator/operators/openpanel/internal/openpanelapi"
)

func TestReconcilePublishesClientIDAtProductOpenBaoPath(t *testing.T) {
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
	reconciler := analyticsonboarding.NewReconciler(kube, projects, secrets, map[string]string{"langfuse": "langfuse/app/langfuse-openpanel-client-id", "devai": "devai/app/devai-openpanel-client-id"})

	if err := reconciler.Reconcile(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}); err != nil {
		t.Fatal(err)
	}
	if projects.recordedID != "" || projects.input.Name != "Langfuse" || projects.input.Domain != claim.Spec.Domain {
		t.Fatalf("project input = %#v, recorded id = %q", projects.input, projects.recordedID)
	}
	if secrets.name != "langfuse/app/langfuse-openpanel-client-id" || secrets.value != "client-123" {
		t.Fatalf("secret = %q, value = %q", secrets.name, secrets.value)
	}

	stored := &analyticsv1alpha1.AnalyticsOnboarding{}
	if err := kube.Get(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}, stored); err != nil {
		t.Fatal(err)
	}
	if stored.Status.ProjectID != "project-langfuse" || stored.Status.ClientIDSecret != "langfuse/app/langfuse-openpanel-client-id" {
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
	reconciler := analyticsonboarding.NewReconciler(kube, projects, &fakeSecrets{}, map[string]string{"langfuse": "langfuse/app/langfuse-openpanel-client-id", "devai": "devai/app/devai-openpanel-client-id"})

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

func TestUnreviewedClaimCannotCallExternalSystems(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	if err := analyticsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	claim := &analyticsv1alpha1.AnalyticsOnboarding{ObjectMeta: metav1.ObjectMeta{Name: "unreviewed", Namespace: "analytics-operator"}, Spec: analyticsv1alpha1.AnalyticsOnboardingSpec{DisplayName: "Unknown", Domain: "https://example.com", CORS: []string{"https://example.com"}}}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(claim).WithObjects(claim).Build()
	// Nil dependencies make any call across the boundary fail the test.
	r := analyticsonboarding.NewReconciler(kube, nil, nil, map[string]string{"devai": "devai/app/devai-openpanel-client-id"})
	key := types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}
	if err := r.Reconcile(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	stored := &analyticsv1alpha1.AnalyticsOnboarding{}
	if err := kube.Get(t.Context(), key, stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Status.Conditions) != 1 || stored.Status.Conditions[0].Status != metav1.ConditionFalse || stored.Status.ClientIDSecret != "" {
		t.Fatalf("unexpected status: %#v", stored.Status)
	}
}

func TestOpenBaoFailureRetriesWithoutReportingReady(t *testing.T) {
	t.Parallel()
	scheme := runtime.NewScheme()
	if err := analyticsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	claim := &analyticsv1alpha1.AnalyticsOnboarding{ObjectMeta: metav1.ObjectMeta{Name: "devai", Namespace: "analytics-operator"}, Spec: analyticsv1alpha1.AnalyticsOnboardingSpec{DisplayName: "DevAI", Domain: "https://devai.tesserix.app", CORS: []string{"https://devai.tesserix.app"}}}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(claim).WithObjects(claim).Build()
	projects := &fakeProjects{result: openpanelapi.Result{ProjectID: "existing-project", ClientID: "existing-client"}}
	secrets := &retrySecrets{}
	r := analyticsonboarding.NewReconciler(kube, projects, secrets, map[string]string{"devai": "devai/app/devai-openpanel-client-id"})
	key := types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}
	if err := r.Reconcile(t.Context(), key); err == nil {
		t.Fatal("storage outage must request retry")
	}
	stored := &analyticsv1alpha1.AnalyticsOnboarding{}
	if err := kube.Get(t.Context(), key, stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Status.Conditions) != 1 || stored.Status.Conditions[0].Status != metav1.ConditionFalse {
		t.Fatalf("unexpected status: %#v", stored.Status)
	}
	if err := r.Reconcile(t.Context(), key); err != nil {
		t.Fatal(err)
	}
	if err := kube.Get(t.Context(), key, stored); err != nil {
		t.Fatal(err)
	}
	if stored.Status.Conditions[0].Status != metav1.ConditionTrue || stored.Status.ProjectID != "existing-project" || stored.Status.ClientIDSecret != "devai/app/devai-openpanel-client-id" || secrets.calls != 2 {
		t.Fatalf("retry did not converge: %#v", stored.Status)
	}
}

type retrySecrets struct{ calls int }

func (s *retrySecrets) Ensure(_ context.Context, name, value string) error {
	s.calls++
	if s.calls == 1 {
		return errors.New("OpenBao returned HTTP 503")
	}
	if name != "devai/app/devai-openpanel-client-id" || value != "existing-client" {
		return errors.New("retry changed destination or payload")
	}
	return nil
}
