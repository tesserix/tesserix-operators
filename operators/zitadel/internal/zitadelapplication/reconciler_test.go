package zitadelapplication_test

import (
	"context"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	identityv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapi"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapplication"
)

func TestReconcile_creates_public_native_application_and_never_persists_secret(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := identityv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	project := &identityv1alpha1.ZitadelProject{
		ObjectMeta: metav1.ObjectMeta{Name: "homechef", Namespace: "identity"},
		Status:     identityv1alpha1.ZitadelProjectStatus{ProjectID: "project-123"},
	}
	claim := &identityv1alpha1.ZitadelApplication{
		ObjectMeta: metav1.ObjectMeta{Name: "homechef-ios", Namespace: "identity"},
		Spec: identityv1alpha1.ZitadelApplicationSpec{
			ProjectRef:             identityv1alpha1.ZitadelProjectReference{Name: "homechef"},
			DisplayName:            "HomeChef iOS",
			ApplicationType:        "native",
			RedirectURIs:           []string{"com.homechef.app:/oauth/callback"},
			PostLogoutRedirectURIs: []string{"com.homechef.app:/oauth/logout"},
		},
	}
	remote := &fakeApplications{created: zitadelapplication.Application{ID: "app-123", ClientID: "client-123", Name: "HomeChef iOS"}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project, claim).WithObjects(project, claim).Build()
	reconciler := zitadelapplication.NewReconciler(client, remote)

	if err := reconciler.Reconcile(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}); err != nil {
		t.Fatal(err)
	}
	if remote.createCalls != 1 {
		t.Fatalf("create calls = %d, want 1", remote.createCalls)
	}
	if remote.input.AppType != "native" || remote.input.AuthMethod != "none" {
		t.Fatalf("application input = %#v", remote.input)
	}
	if remote.input.ResponseType != "code" || remote.input.GrantType != "authorization_code" {
		t.Fatalf("application input = %#v", remote.input)
	}

	stored := &identityv1alpha1.ZitadelApplication{}
	if err := client.Get(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}, stored); err != nil {
		t.Fatal(err)
	}
	if stored.Status.AppID != "app-123" || stored.Status.ClientID != "client-123" {
		t.Fatalf("status = %#v", stored.Status)
	}
	if len(stored.Status.Conditions) != 1 || stored.Status.Conditions[0].Status != metav1.ConditionTrue {
		t.Fatalf("conditions = %#v", stored.Status.Conditions)
	}
}

func TestReconcile_rejects_duplicate_redirect_uris_before_remote_call(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := identityv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	project := &identityv1alpha1.ZitadelProject{ObjectMeta: metav1.ObjectMeta{Name: "homechef", Namespace: "identity"}, Status: identityv1alpha1.ZitadelProjectStatus{ProjectID: "project-123"}}
	claim := &identityv1alpha1.ZitadelApplication{ObjectMeta: metav1.ObjectMeta{Name: "invalid", Namespace: "identity"}, Spec: identityv1alpha1.ZitadelApplicationSpec{ProjectRef: identityv1alpha1.ZitadelProjectReference{Name: "homechef"}, DisplayName: "Invalid", ApplicationType: "native", RedirectURIs: []string{"com.homechef.app:/callback", "com.homechef.app:/callback"}}}
	remote := &fakeApplications{}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project, claim).WithObjects(project, claim).Build()
	reconciler := zitadelapplication.NewReconciler(client, remote)

	if err := reconciler.Reconcile(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}); err == nil {
		t.Fatal("Reconcile() error = nil, want validation error")
	}
	if remote.createCalls != 0 {
		t.Fatalf("create calls = %d, want 0", remote.createCalls)
	}
}

func TestReconcile_rejects_insecure_user_agent_redirect_before_remote_call(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := identityv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	project := &identityv1alpha1.ZitadelProject{ObjectMeta: metav1.ObjectMeta{Name: "homechef", Namespace: "identity"}, Status: identityv1alpha1.ZitadelProjectStatus{ProjectID: "project-123"}}
	claim := &identityv1alpha1.ZitadelApplication{ObjectMeta: metav1.ObjectMeta{Name: "invalid-web", Namespace: "identity"}, Spec: identityv1alpha1.ZitadelApplicationSpec{ProjectRef: identityv1alpha1.ZitadelProjectReference{Name: "homechef"}, DisplayName: "Invalid Web", ApplicationType: "user_agent", RedirectURIs: []string{"http://homechef.app/oauth/callback"}}}
	remote := &fakeApplications{}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project, claim).WithObjects(project, claim).Build()
	reconciler := zitadelapplication.NewReconciler(client, remote)

	if err := reconciler.Reconcile(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}); err == nil {
		t.Fatal("Reconcile() error = nil, want validation error")
	}
	if remote.createCalls != 0 {
		t.Fatalf("create calls = %d, want 0", remote.createCalls)
	}
}

type fakeApplications struct {
	created     zitadelapplication.Application
	input       zitadelapplication.ApplicationInput
	createCalls int
}

func (f *fakeApplications) FindApplicationByID(context.Context, string, string, string) (zitadelapplication.Application, bool, error) {
	return zitadelapplication.Application{}, false, nil
}

func (f *fakeApplications) FindApplicationByName(context.Context, string, string, string) (zitadelapplication.Application, bool, error) {
	return zitadelapplication.Application{}, false, nil
}

func (f *fakeApplications) CreateApplication(_ context.Context, _ string, _ string, input zitadelapplication.ApplicationInput) (zitadelapplication.Application, error) {
	f.createCalls++
	f.input = input
	return f.created, nil
}

var _ zitadelapplication.Applications = (*fakeApplications)(nil)
var _ = zitadelapi.Application{}
