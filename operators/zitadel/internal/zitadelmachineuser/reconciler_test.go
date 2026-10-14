package zitadelmachineuser_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	identityv1alpha1 "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapi"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelmachineuser"
)

type fakeUsers struct {
	existing     *zitadelmachineuser.MachineUser
	created      int
	updated      int
	patIssues    int
	secretIssues int
}

func (f *fakeUsers) FindMachineUserByID(_ context.Context, _, id string) (zitadelmachineuser.MachineUser, bool, error) {
	if f.existing != nil && f.existing.ID == id {
		return *f.existing, true, nil
	}
	return zitadelmachineuser.MachineUser{}, false, nil
}

func (f *fakeUsers) FindMachineUserByName(_ context.Context, _, name string) (zitadelmachineuser.MachineUser, bool, error) {
	if f.existing != nil && f.existing.UserName == name {
		return *f.existing, true, nil
	}
	return zitadelmachineuser.MachineUser{}, false, nil
}

func (f *fakeUsers) CreateMachineUser(_ context.Context, _, userName, name string) (zitadelmachineuser.MachineUser, error) {
	f.created++
	user := zitadelmachineuser.MachineUser{ID: "user-1", UserName: userName, Name: name, AccessTokenType: zitadelapi.AccessTokenTypeJWT}
	f.existing = &user
	return user, nil
}

func (f *fakeUsers) UpdateMachineUser(_ context.Context, _, _, name string) error {
	f.updated++
	f.existing.Name, f.existing.AccessTokenType = name, zitadelapi.AccessTokenTypeJWT
	return nil
}

func (f *fakeUsers) ReplacePersonalAccessTokens(context.Context, string, string) (string, error) {
	f.patIssues++
	return "pat-value", nil
}

func (f *fakeUsers) ReplaceClientSecret(context.Context, string, string) (string, string, error) {
	f.secretIssues++
	return "kora-eval", "secret-value", nil
}

type fakeSecrets struct {
	values    map[string]string
	readErr   error
	ensureErr error
}

func (f *fakeSecrets) Latest(_ context.Context, name string) (string, bool, error) {
	if f.readErr != nil {
		return "", false, f.readErr
	}
	value, ok := f.values[name]
	return value, ok, nil
}

func (f *fakeSecrets) Ensure(_ context.Context, name, value string) error {
	if f.ensureErr != nil {
		return f.ensureErr
	}
	f.values[name] = value
	return nil
}

func claim(credential string) *identityv1alpha1.ZitadelMachineUser {
	return &identityv1alpha1.ZitadelMachineUser{
		ObjectMeta: metav1.ObjectMeta{Name: "kora-account-deleter", Namespace: "identity-operator"},
		Spec: identityv1alpha1.ZitadelMachineUserSpec{
			Organization: "TESSERIX",
			UserName:     "kora-account-deleter",
			DisplayName:  "Kora account deleter",
			Credential:   credential,
		},
	}
}

func setup(t *testing.T, c *identityv1alpha1.ZitadelMachineUser) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := identityv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	return fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(c).WithObjects(c).Build()
}

var key = types.NamespacedName{Namespace: "identity-operator", Name: "kora-account-deleter"}

func stored(t *testing.T, c client.Client) *identityv1alpha1.ZitadelMachineUser {
	t.Helper()
	out := &identityv1alpha1.ZitadelMachineUser{}
	if err := c.Get(context.Background(), key, out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestReconcile_creates_jwt_machine_user_and_publishes_pat_only_to_openbao(t *testing.T) {
	t.Parallel()
	c := setup(t, claim(zitadelmachineuser.CredentialPAT))
	users, secrets := &fakeUsers{}, &fakeSecrets{values: map[string]string{}}

	if err := zitadelmachineuser.NewReconciler(c, users, secrets).Reconcile(context.Background(), key); err != nil {
		t.Fatal(err)
	}

	if users.created != 1 || users.patIssues != 1 {
		t.Fatalf("created=%d patIssues=%d, want 1 and 1", users.created, users.patIssues)
	}
	if secrets.values["kora-account-deleter"] != "pat-value" {
		t.Fatalf("published %q", secrets.values["kora-account-deleter"])
	}
	got := stored(t, c)
	if got.Status.UserID != "user-1" || got.Status.ClientID != "" {
		t.Fatalf("status = %+v", got.Status)
	}
	raw, _ := json.Marshal(got)
	if strings.Contains(string(raw), "pat-value") {
		t.Fatal("credential leaked into the claim")
	}
}

func TestReconcile_never_reissues_while_a_credential_is_published(t *testing.T) {
	t.Parallel()
	c := setup(t, claim(zitadelmachineuser.CredentialPAT))
	users := &fakeUsers{existing: &zitadelmachineuser.MachineUser{ID: "user-1", UserName: "kora-account-deleter", Name: "Kora account deleter", AccessTokenType: zitadelapi.AccessTokenTypeJWT}}
	secrets := &fakeSecrets{values: map[string]string{"kora-account-deleter": "in-use"}}

	if err := zitadelmachineuser.NewReconciler(c, users, secrets).Reconcile(context.Background(), key); err != nil {
		t.Fatal(err)
	}

	if users.created+users.updated+users.patIssues != 0 {
		t.Fatalf("created=%d updated=%d patIssues=%d, want no writes", users.created, users.updated, users.patIssues)
	}
	if secrets.values["kora-account-deleter"] != "in-use" {
		t.Fatal("published credential changed")
	}
}

func TestReconcile_an_openbao_read_failure_never_rotates(t *testing.T) {
	t.Parallel()
	c := setup(t, claim(zitadelmachineuser.CredentialPAT))
	users := &fakeUsers{}
	secrets := &fakeSecrets{values: map[string]string{}, readErr: errors.New("OpenBao returned HTTP 503")}

	if err := zitadelmachineuser.NewReconciler(c, users, secrets).Reconcile(context.Background(), key); err == nil {
		t.Fatal("want an error")
	}
	if users.patIssues != 0 {
		t.Fatal("issued a credential while OpenBao was unreadable")
	}
}

func TestReconcile_a_failed_publish_is_reissued_on_the_next_pass(t *testing.T) {
	t.Parallel()
	c := setup(t, claim(zitadelmachineuser.CredentialPAT))
	users := &fakeUsers{}
	secrets := &fakeSecrets{values: map[string]string{}, ensureErr: errors.New("OpenBao returned HTTP 403")}
	reconciler := zitadelmachineuser.NewReconciler(c, users, secrets)

	if err := reconciler.Reconcile(context.Background(), key); err == nil {
		t.Fatal("want an error")
	}
	secrets.ensureErr = nil
	if err := reconciler.Reconcile(context.Background(), key); err != nil {
		t.Fatal(err)
	}

	if users.patIssues != 2 || users.created != 1 {
		t.Fatalf("patIssues=%d created=%d, want 2 and 1", users.patIssues, users.created)
	}
	if secrets.values["kora-account-deleter"] != "pat-value" {
		t.Fatal("credential not published")
	}
}

func TestReconcile_adopts_by_name_and_forces_jwt_access_tokens(t *testing.T) {
	t.Parallel()
	c := setup(t, claim(zitadelmachineuser.CredentialPAT))
	users := &fakeUsers{existing: &zitadelmachineuser.MachineUser{ID: "user-9", UserName: "kora-account-deleter", Name: "Kora account deleter", AccessTokenType: "ACCESS_TOKEN_TYPE_BEARER"}}
	secrets := &fakeSecrets{values: map[string]string{"kora-account-deleter": "in-use"}}

	if err := zitadelmachineuser.NewReconciler(c, users, secrets).Reconcile(context.Background(), key); err != nil {
		t.Fatal(err)
	}

	if users.created != 0 || users.updated != 1 {
		t.Fatalf("created=%d updated=%d, want 0 and 1", users.created, users.updated)
	}
	if stored(t, c).Status.UserID != "user-9" {
		t.Fatal("adopted user id not recorded")
	}
}

func TestReconcile_client_secret_records_the_client_id(t *testing.T) {
	t.Parallel()
	c := setup(t, claim(zitadelmachineuser.CredentialClientSecret))
	users, secrets := &fakeUsers{}, &fakeSecrets{values: map[string]string{}}

	if err := zitadelmachineuser.NewReconciler(c, users, secrets).Reconcile(context.Background(), key); err != nil {
		t.Fatal(err)
	}

	if users.secretIssues != 1 || secrets.values["kora-account-deleter"] != "secret-value" {
		t.Fatalf("secretIssues=%d published=%q", users.secretIssues, secrets.values["kora-account-deleter"])
	}
	if stored(t, c).Status.ClientID != "kora-eval" {
		t.Fatal("client id not recorded")
	}
}

func TestReconcile_rejects_invalid_claims(t *testing.T) {
	t.Parallel()
	for name, mutate := range map[string]func(*identityv1alpha1.ZitadelMachineUser){
		"unknown credential": func(c *identityv1alpha1.ZitadelMachineUser) { c.Spec.Credential = "key" },
		"bad user name":      func(c *identityv1alpha1.ZitadelMachineUser) { c.Spec.UserName = "Kora Deleter" },
		"no organization":    func(c *identityv1alpha1.ZitadelMachineUser) { c.Spec.Organization = "" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			bad := claim(zitadelmachineuser.CredentialPAT)
			mutate(bad)
			users := &fakeUsers{}
			err := zitadelmachineuser.NewReconciler(setup(t, bad), users, &fakeSecrets{values: map[string]string{}}).Reconcile(context.Background(), key)
			if err == nil || users.created != 0 {
				t.Fatalf("err=%v created=%d", err, users.created)
			}
		})
	}
}
