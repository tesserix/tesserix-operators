package zitadelproject_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	identityv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelproject"
)

func TestReconcile_creates_missing_project_and_records_immutable_id(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := identityv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	claim := &identityv1alpha1.ZitadelProject{
		ObjectMeta: metav1.ObjectMeta{Name: "homechef", Namespace: "identity"},
		Spec: identityv1alpha1.ZitadelProjectSpec{
			DisplayName:  "HomeChef",
			Organization: "TESSERIX",
		},
	}
	remote := &fakeProjects{created: zitadelproject.Project{ID: "project-123", Name: "HomeChef"}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(claim).WithObjects(claim).Build()
	reconciler := zitadelproject.NewReconciler(client, scheme, remote)

	if err := reconciler.Reconcile(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}); err != nil {
		t.Fatal(err)
	}

	if remote.createCalls != 1 {
		t.Fatalf("create calls = %d, want 1", remote.createCalls)
	}

	stored := &identityv1alpha1.ZitadelProject{}
	if err := reconciler.Client().Get(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}, stored); err != nil {
		t.Fatal(err)
	}
	if stored.Status.ProjectID != "project-123" {
		t.Fatalf("status project ID = %q, want project-123", stored.Status.ProjectID)
	}
	if len(stored.Status.Conditions) != 1 || stored.Status.Conditions[0].Type != "Ready" || stored.Status.Conditions[0].Status != metav1.ConditionTrue {
		t.Fatalf("ready condition = %#v", stored.Status.Conditions)
	}
}

type fakeProjects struct {
	created     zitadelproject.Project
	createCalls int
}

func (f *fakeProjects) FindByID(context.Context, string, string) (zitadelproject.Project, bool, error) {
	return zitadelproject.Project{}, false, nil
}

func (f *fakeProjects) FindByName(context.Context, string, string) (zitadelproject.Project, bool, error) {
	return zitadelproject.Project{}, false, nil
}

func (f *fakeProjects) Create(context.Context, string, zitadelproject.ProjectInput) (zitadelproject.Project, error) {
	f.createCalls++
	return f.created, nil
}
