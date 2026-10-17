package zitadelorganization_test

import (
	"context"
	"errors"
	"testing"

	identity "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelorganization"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type identityAPI struct{ calls int }

func (f *identityAPI) EnsureOrganization(context.Context, identity.ZitadelOrganizationSpec, string, string) (string, error) {
	f.calls++
	return "existing-org", nil
}

type archive struct {
	calls int
	fail  bool
}

func (f *archive) Put(context.Context, string, []byte) (string, error) {
	f.calls++
	if f.fail {
		return "", errors.New("archive unavailable")
	}
	return "123", nil
}
func TestReconcileRequiresDurableArchiveBeforeIdentityAndReady(t *testing.T) {
	s := runtime.NewScheme()
	if e := identity.AddToScheme(s); e != nil {
		t.Fatal(e)
	}
	p := &identity.ZitadelProject{ObjectMeta: metav1.ObjectMeta{Name: "helivanta", Namespace: "identity", Generation: 1}, Spec: identity.ZitadelProjectSpec{Organization: "TESSERIX", DisplayName: "helivanta"}, Status: identity.ZitadelProjectStatus{ProjectID: "project", ObservedGeneration: 1, Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Reconciled", Message: "ok", ObservedGeneration: 1}}}}
	c := &identity.ZitadelOrganization{ObjectMeta: metav1.ObjectMeta{Name: "helivanta-11111111-1111-4111-8111-111111111111", Namespace: "identity", Generation: 1}, Spec: identity.ZitadelOrganizationSpec{InitialAdminUserID: "admin", Product: "helivanta", TenantID: "11111111-1111-4111-8111-111111111111", DisplayName: "TEST Hospital", OrganizationID: "existing-org"}}
	k := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(c).WithObjects(p, c).Build()
	a := &archive{fail: true}
	remote := &identityAPI{}
	r := zitadelorganization.NewReconciler(k, remote, a, map[string]string{"helivanta": "helivanta"}, &activation{})
	key := types.NamespacedName{Namespace: c.Namespace, Name: c.Name}
	if e := r.Reconcile(t.Context(), key); e == nil {
		t.Fatal("archive outage accepted")
	}
	if remote.calls != 0 {
		t.Fatal("mutated identity before intent archive")
	}
	a.fail = false
	if e := r.Reconcile(t.Context(), key); e != nil {
		t.Fatal(e)
	}
	if e := k.Get(t.Context(), key, c); e != nil {
		t.Fatal(e)
	}
	if !meta.IsStatusConditionTrue(c.Status.Conditions, "Ready") || c.Status.OrganizationID != "existing-org" || c.Status.ArchiveGeneration != "123" {
		t.Fatalf("incomplete result: %+v", c.Status)
	}
	if remote.calls != 1 || a.calls != 3 {
		t.Fatalf("calls remote=%d archive=%d", remote.calls, a.calls)
	}
}

type activation struct {
	fail  bool
	calls int
}

func (f *activation) Complete(context.Context, identity.ZitadelOrganizationSpec, string, string) error {
	f.calls++
	if f.fail {
		return errors.New("product API unavailable")
	}
	return nil
}
func TestActivationFailureDoesNotReportIdentityAsComplete(t *testing.T) {
	s := runtime.NewScheme()
	if e := identity.AddToScheme(s); e != nil {
		t.Fatal(e)
	}
	p := &identity.ZitadelProject{ObjectMeta: metav1.ObjectMeta{Name: "helivanta", Namespace: "identity"}, Status: identity.ZitadelProjectStatus{ProjectID: "p", Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "ok", Message: "ok"}}}}
	c := &identity.ZitadelOrganization{ObjectMeta: metav1.ObjectMeta{Name: "helivanta-11111111-1111-4111-8111-111111111111", Namespace: "identity"}, Spec: identity.ZitadelOrganizationSpec{InitialAdminUserID: "admin", Product: "helivanta", TenantID: "11111111-1111-4111-8111-111111111111", DisplayName: "Hospital"}}
	k := fake.NewClientBuilder().WithScheme(s).WithStatusSubresource(c).WithObjects(c, p).Build()
	a := &activation{fail: true}
	archive := &archive{}
	r := zitadelorganization.NewReconciler(k, &identityAPI{}, archive, map[string]string{"helivanta": "helivanta"}, a)
	key := types.NamespacedName{Namespace: c.Namespace, Name: c.Name}
	if e := r.Reconcile(t.Context(), key); e == nil {
		t.Fatal("product failure accepted")
	}
	if e := k.Get(t.Context(), key, c); e != nil {
		t.Fatal(e)
	}
	if meta.IsStatusConditionTrue(c.Status.Conditions, "Ready") || archive.calls != 1 {
		t.Fatal("reported or archived incomplete onboarding as complete")
	}
}
