package zitadelproject_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	identityv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelproject"
)

func newTestScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := identityv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return scheme
}

func reconcile(t *testing.T, claim *identityv1alpha1.ZitadelProject, remote zitadelproject.Projects) (*zitadelproject.Reconciler, error) {
	t.Helper()
	scheme := newTestScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(claim).WithObjects(claim).Build()
	reconciler := zitadelproject.NewReconciler(client, scheme, remote)
	err := reconciler.Reconcile(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name})
	return reconciler, err
}

func storedClaim(t *testing.T, reconciler *zitadelproject.Reconciler, claim *identityv1alpha1.ZitadelProject) *identityv1alpha1.ZitadelProject {
	t.Helper()
	stored := &identityv1alpha1.ZitadelProject{}
	if err := reconciler.Client().Get(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}, stored); err != nil {
		t.Fatal(err)
	}
	return stored
}

func TestReconcile_creates_missing_project_and_records_immutable_id(t *testing.T) {
	t.Parallel()

	claim := &identityv1alpha1.ZitadelProject{
		ObjectMeta: metav1.ObjectMeta{Name: "homechef", Namespace: "identity"},
		Spec: identityv1alpha1.ZitadelProjectSpec{
			DisplayName:  "HomeChef",
			Organization: "TESSERIX",
		},
	}
	remote := &fakeProjects{created: zitadelproject.Project{ID: "project-123", Name: "HomeChef"}}
	reconciler, err := reconcile(t, claim, remote)
	if err != nil {
		t.Fatal(err)
	}

	if remote.createCalls != 1 {
		t.Fatalf("create calls = %d, want 1", remote.createCalls)
	}
	stored := storedClaim(t, reconciler, claim)
	if stored.Status.ProjectID != "project-123" {
		t.Fatalf("status project ID = %q, want project-123", stored.Status.ProjectID)
	}
	if len(stored.Status.Conditions) != 1 || stored.Status.Conditions[0].Type != "Ready" || stored.Status.Conditions[0].Status != metav1.ConditionTrue {
		t.Fatalf("ready condition = %#v", stored.Status.Conditions)
	}
}

func TestReconcile_records_failure_condition_when_remote_lookup_fails(t *testing.T) {
	t.Parallel()

	claim := &identityv1alpha1.ZitadelProject{
		ObjectMeta: metav1.ObjectMeta{Name: "homechef", Namespace: "identity"},
		Spec:       identityv1alpha1.ZitadelProjectSpec{DisplayName: "HomeChef", Organization: "TESSERIX"},
	}
	reconciler, err := reconcile(t, claim, &failingProjects{})
	if err == nil {
		t.Fatal("Reconcile() error = nil, want remote error")
	}

	stored := storedClaim(t, reconciler, claim)
	if len(stored.Status.Conditions) != 1 || stored.Status.Conditions[0].Status != metav1.ConditionFalse || stored.Status.Conditions[0].Reason != "ReconcileFailed" {
		t.Fatalf("failure condition = %#v", stored.Status.Conditions)
	}
}

func TestReconcile_restricted_enables_role_check_and_grants_members(t *testing.T) {
	t.Parallel()

	claim := &identityv1alpha1.ZitadelProject{
		ObjectMeta: metav1.ObjectMeta{Name: "homechef", Namespace: "identity"},
		Spec: identityv1alpha1.ZitadelProjectSpec{
			DisplayName:  "HomeChef",
			Organization: "TESSERIX",
			Access: &identityv1alpha1.ZitadelProjectAccess{
				Mode: "restricted",
				Members: []identityv1alpha1.ZitadelProjectMember{
					{Email: "samyak.rout@gmail.com"},
					{Email: "mahesh.sangawar@gmail.com", Roles: []string{"admin"}},
				},
			},
		},
	}
	remote := &fakeProjects{
		byName:  true,
		project: zitadelproject.Project{ID: "p1", Name: "HomeChef"},
		users:   map[string]string{"samyak.rout@gmail.com": "u1", "mahesh.sangawar@gmail.com": "u2"},
	}
	reconciler, err := reconcile(t, claim, remote)
	if err != nil {
		t.Fatal(err)
	}

	if len(remote.updateProjectCalls) != 1 || remote.updateProjectCalls[0] != true {
		t.Fatalf("update project calls = %#v, want one enabling role check", remote.updateProjectCalls)
	}
	if len(remote.addedRoles) != 2 {
		t.Fatalf("added roles = %#v, want member and admin", remote.addedRoles)
	}
	if got := remote.addedGrants["u1"]; len(got) != 1 || got[0] != "member" {
		t.Fatalf("grant for u1 = %#v, want [member]", got)
	}
	if got := remote.addedGrants["u2"]; len(got) != 1 || got[0] != "admin" {
		t.Fatalf("grant for u2 = %#v, want [admin]", got)
	}
	stored := storedClaim(t, reconciler, claim)
	if stored.Status.Conditions[0].Status != metav1.ConditionTrue {
		t.Fatalf("ready condition = %#v", stored.Status.Conditions)
	}
}

func TestReconcile_restricted_updates_and_prunes_grants(t *testing.T) {
	t.Parallel()

	claim := &identityv1alpha1.ZitadelProject{
		ObjectMeta: metav1.ObjectMeta{Name: "homechef", Namespace: "identity"},
		Spec: identityv1alpha1.ZitadelProjectSpec{
			DisplayName:  "HomeChef",
			Organization: "TESSERIX",
			Access: &identityv1alpha1.ZitadelProjectAccess{
				Mode:    "restricted",
				Members: []identityv1alpha1.ZitadelProjectMember{{Email: "samyak.rout@gmail.com", Roles: []string{"admin"}}},
			},
		},
	}
	remote := &fakeProjects{
		byName:  true,
		project: zitadelproject.Project{ID: "p1", Name: "HomeChef", ProjectRoleCheck: true},
		roles:   []string{"member", "admin"},
		users:   map[string]string{"samyak.rout@gmail.com": "u1"},
		grants: []zitadelproject.UserGrant{
			{ID: "g1", UserID: "u1", RoleKeys: []string{"member"}},
			{ID: "g9", UserID: "u9", RoleKeys: []string{"member"}},
		},
	}
	if _, err := reconcile(t, claim, remote); err != nil {
		t.Fatal(err)
	}

	if len(remote.updateProjectCalls) != 0 {
		t.Fatalf("update project calls = %#v, want none (no drift)", remote.updateProjectCalls)
	}
	if got := remote.updatedGrants["g1"]; len(got) != 1 || got[0] != "admin" {
		t.Fatalf("updated grant g1 = %#v, want [admin]", got)
	}
	if len(remote.removedGrants) != 1 || remote.removedGrants[0] != "g9" {
		t.Fatalf("removed grants = %#v, want [g9]", remote.removedGrants)
	}
	if len(remote.addedGrants) != 0 {
		t.Fatalf("added grants = %#v, want none", remote.addedGrants)
	}
}

func TestReconcile_public_disables_role_check_and_leaves_grants_alone(t *testing.T) {
	t.Parallel()

	claim := &identityv1alpha1.ZitadelProject{
		ObjectMeta: metav1.ObjectMeta{Name: "homechef", Namespace: "identity"},
		Spec: identityv1alpha1.ZitadelProjectSpec{
			DisplayName:  "HomeChef",
			Organization: "TESSERIX",
			Access:       &identityv1alpha1.ZitadelProjectAccess{Mode: "public"},
		},
	}
	remote := &fakeProjects{
		byName:  true,
		project: zitadelproject.Project{ID: "p1", Name: "HomeChef", ProjectRoleCheck: true},
		grants:  []zitadelproject.UserGrant{{ID: "g1", UserID: "u1", RoleKeys: []string{"member"}}},
	}
	if _, err := reconcile(t, claim, remote); err != nil {
		t.Fatal(err)
	}

	if len(remote.updateProjectCalls) != 1 || remote.updateProjectCalls[0] != false {
		t.Fatalf("update project calls = %#v, want one disabling role check", remote.updateProjectCalls)
	}
	if remote.listGrantCalls != 0 || len(remote.removedGrants) != 0 {
		t.Fatalf("public mode touched grants: listed %d removed %#v", remote.listGrantCalls, remote.removedGrants)
	}
}

func TestReconcile_restricted_fails_when_member_has_no_account(t *testing.T) {
	t.Parallel()

	claim := &identityv1alpha1.ZitadelProject{
		ObjectMeta: metav1.ObjectMeta{Name: "homechef", Namespace: "identity"},
		Spec: identityv1alpha1.ZitadelProjectSpec{
			DisplayName:  "HomeChef",
			Organization: "TESSERIX",
			Access: &identityv1alpha1.ZitadelProjectAccess{
				Mode:    "restricted",
				Members: []identityv1alpha1.ZitadelProjectMember{{Email: "nobody@example.com"}},
			},
		},
	}
	remote := &fakeProjects{byName: true, project: zitadelproject.Project{ID: "p1", Name: "HomeChef", ProjectRoleCheck: true}, roles: []string{"member"}}
	reconciler, err := reconcile(t, claim, remote)
	if err == nil || !strings.Contains(err.Error(), "nobody@example.com") {
		t.Fatalf("Reconcile() error = %v, want missing-user error naming the email", err)
	}

	stored := storedClaim(t, reconciler, claim)
	if stored.Status.Conditions[0].Status != metav1.ConditionFalse {
		t.Fatalf("condition = %#v, want ReconcileFailed", stored.Status.Conditions)
	}
}

func TestReconcile_rejects_invalid_access_spec(t *testing.T) {
	t.Parallel()

	cases := map[string]*identityv1alpha1.ZitadelProjectAccess{
		"unknown mode":                {Mode: "invite-only"},
		"restricted without members":  {Mode: "restricted"},
		"duplicate member emails":     {Mode: "restricted", Members: []identityv1alpha1.ZitadelProjectMember{{Email: "a@b.com"}, {Email: "A@b.com"}}},
		"public with members":         {Mode: "public", Members: []identityv1alpha1.ZitadelProjectMember{{Email: "a@b.com"}}},
		"member with malformed email": {Mode: "restricted", Members: []identityv1alpha1.ZitadelProjectMember{{Email: "not-an-email"}}},
	}
	for name, access := range cases {
		t.Run(name, func(t *testing.T) {
			claim := &identityv1alpha1.ZitadelProject{
				ObjectMeta: metav1.ObjectMeta{Name: "homechef", Namespace: "identity"},
				Spec: identityv1alpha1.ZitadelProjectSpec{
					DisplayName:  "HomeChef",
					Organization: "TESSERIX",
					Access:       access,
				},
			}
			remote := &fakeProjects{}
			if _, err := reconcile(t, claim, remote); err == nil {
				t.Fatal("Reconcile() error = nil, want validation error")
			}
			if remote.createCalls != 0 || len(remote.updateProjectCalls) != 0 {
				t.Fatal("invalid spec must not reach the remote API")
			}
		})
	}
}

type fakeProjects struct {
	created zitadelproject.Project
	byName  bool
	project zitadelproject.Project
	roles   []string
	users   map[string]string
	grants  []zitadelproject.UserGrant

	createCalls        int
	listGrantCalls     int
	updateProjectCalls []bool
	addedRoles         []string
	addedGrants        map[string][]string
	updatedGrants      map[string][]string
	removedGrants      []string
}

func (f *fakeProjects) FindByID(context.Context, string, string) (zitadelproject.Project, bool, error) {
	return zitadelproject.Project{}, false, nil
}

func (f *fakeProjects) FindByName(context.Context, string, string) (zitadelproject.Project, bool, error) {
	if f.byName {
		return f.project, true, nil
	}
	return zitadelproject.Project{}, false, nil
}

func (f *fakeProjects) Create(context.Context, string, zitadelproject.ProjectInput) (zitadelproject.Project, error) {
	f.createCalls++
	return f.created, nil
}

func (f *fakeProjects) UpdateProject(_ context.Context, _, _, _ string, roleCheck bool) error {
	f.updateProjectCalls = append(f.updateProjectCalls, roleCheck)
	return nil
}

func (f *fakeProjects) ListRoles(context.Context, string, string) ([]string, error) {
	return f.roles, nil
}

func (f *fakeProjects) AddRole(_ context.Context, _, _, key string) error {
	f.addedRoles = append(f.addedRoles, key)
	f.roles = append(f.roles, key)
	return nil
}

func (f *fakeProjects) FindUserByEmail(_ context.Context, _, email string) (string, bool, error) {
	id, ok := f.users[email]
	return id, ok, nil
}

func (f *fakeProjects) ListGrants(context.Context, string, string) ([]zitadelproject.UserGrant, error) {
	f.listGrantCalls++
	return f.grants, nil
}

func (f *fakeProjects) AddGrant(_ context.Context, _, userID, _ string, roles []string) error {
	if f.addedGrants == nil {
		f.addedGrants = map[string][]string{}
	}
	f.addedGrants[userID] = roles
	return nil
}

func (f *fakeProjects) UpdateGrant(_ context.Context, _, _, grantID string, roles []string) error {
	if f.updatedGrants == nil {
		f.updatedGrants = map[string][]string{}
	}
	f.updatedGrants[grantID] = roles
	return nil
}

func (f *fakeProjects) RemoveGrant(_ context.Context, _, _, grantID string) error {
	f.removedGrants = append(f.removedGrants, grantID)
	return nil
}

type failingProjects struct{}

func (f *failingProjects) FindByID(context.Context, string, string) (zitadelproject.Project, bool, error) {
	return zitadelproject.Project{}, false, nil
}

func (f *failingProjects) FindByName(context.Context, string, string) (zitadelproject.Project, bool, error) {
	return zitadelproject.Project{}, false, errors.New("Zitadel unavailable")
}

func (f *failingProjects) Create(context.Context, string, zitadelproject.ProjectInput) (zitadelproject.Project, error) {
	return zitadelproject.Project{}, errors.New("unexpected create")
}

func (f *failingProjects) UpdateProject(context.Context, string, string, string, bool) error {
	return errors.New("unexpected update")
}

func (f *failingProjects) ListRoles(context.Context, string, string) ([]string, error) {
	return nil, errors.New("unexpected list roles")
}

func (f *failingProjects) AddRole(context.Context, string, string, string) error {
	return errors.New("unexpected add role")
}

func (f *failingProjects) FindUserByEmail(context.Context, string, string) (string, bool, error) {
	return "", false, errors.New("unexpected user lookup")
}

func (f *failingProjects) ListGrants(context.Context, string, string) ([]zitadelproject.UserGrant, error) {
	return nil, errors.New("unexpected list grants")
}

func (f *failingProjects) AddGrant(context.Context, string, string, string, []string) error {
	return errors.New("unexpected add grant")
}

func (f *failingProjects) UpdateGrant(context.Context, string, string, string, []string) error {
	return errors.New("unexpected update grant")
}

func (f *failingProjects) RemoveGrant(context.Context, string, string, string) error {
	return errors.New("unexpected remove grant")
}
