package zitadelapplication_test

import (
	"context"
	"encoding/json"
	"fmt"
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
		Status:     readyProjectStatus("project-123"),
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

func TestReconcile_manages_explicit_refresh_and_redirect_drift(t *testing.T) {
	for _, changed := range []bool{false, true} {
		t.Run(fmt.Sprint(changed), func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := identityv1alpha1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			yes := true
			project := &identityv1alpha1.ZitadelProject{ObjectMeta: metav1.ObjectMeta{Name: "roamie", Namespace: "identity"}, Status: readyProjectStatus("p")}
			claim := &identityv1alpha1.ZitadelApplication{ObjectMeta: metav1.ObjectMeta{Name: "roamie-ios", Namespace: "identity"}, Spec: identityv1alpha1.ZitadelApplicationSpec{ProjectRef: identityv1alpha1.ZitadelProjectReference{Name: "roamie"}, DisplayName: "Roamie iOS", ApplicationType: "native", RefreshToken: &yes, RedirectURIs: []string{"roamie:/auth/callback"}}}
			config := zitadelapi.OIDCConfig{LoginVersion: json.RawMessage(`{"loginV2":{}}`), ClientID: "c", IDTokenUserinfoAssertion: true, AppType: "OIDC_APP_TYPE_NATIVE", AuthMethodType: "OIDC_AUTH_METHOD_TYPE_NONE", AccessTokenType: "OIDC_TOKEN_TYPE_JWT", RedirectURIs: []string{"roamie:/auth/callback"}, GrantTypes: []string{"OIDC_GRANT_TYPE_AUTHORIZATION_CODE", "OIDC_GRANT_TYPE_REFRESH_TOKEN"}, ResponseTypes: []string{"OIDC_RESPONSE_TYPE_CODE"}}
			if changed {
				config.RedirectURIs = []string{"roamie:/old"}
				config.GrantTypes = []string{"OIDC_GRANT_TYPE_AUTHORIZATION_CODE"}
			}
			remote := &fakeApplications{existing: zitadelapi.Application{ID: "a", ClientID: "c", OIDCConfig: config}, existingFound: true}
			client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project, claim).WithObjects(project, claim).Build()
			if err := zitadelapplication.NewReconciler(client, remote).Reconcile(t.Context(), types.NamespacedName{Namespace: "identity", Name: "roamie-ios"}); err != nil {
				t.Fatal(err)
			}
			want := 0
			if changed {
				want = 1
			}
			if remote.updateCalls != want {
				t.Fatalf("updates=%d want=%d", remote.updateCalls, want)
			}
			if changed && (remote.updateInput.RefreshToken == nil || !*remote.updateInput.RefreshToken) {
				t.Fatal("refresh grant was not forwarded")
			}
		})
	}
}

func TestReconcile_heals_redirect_drift_without_removing_legacy_refresh_grant(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := identityv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	project := &identityv1alpha1.ZitadelProject{ObjectMeta: metav1.ObjectMeta{Name: "product", Namespace: "identity"}, Status: readyProjectStatus("p")}
	claim := &identityv1alpha1.ZitadelApplication{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "identity"}, Spec: identityv1alpha1.ZitadelApplicationSpec{ProjectRef: identityv1alpha1.ZitadelProjectReference{Name: "product"}, DisplayName: "Web", ApplicationType: "user_agent", RedirectURIs: []string{"https://product.example/callback"}}}
	remote := &fakeApplications{existingFound: true, existing: zitadelapi.Application{ID: "a", ClientID: "c", OIDCConfig: zitadelapi.OIDCConfig{IDTokenUserinfoAssertion: true, AppType: "OIDC_APP_TYPE_USER_AGENT", AuthMethodType: "OIDC_AUTH_METHOD_TYPE_NONE", AccessTokenType: "OIDC_TOKEN_TYPE_JWT", RedirectURIs: []string{"https://old.example/callback"}, GrantTypes: []string{"OIDC_GRANT_TYPE_AUTHORIZATION_CODE", "OIDC_GRANT_TYPE_REFRESH_TOKEN"}, ResponseTypes: []string{"OIDC_RESPONSE_TYPE_CODE"}}}}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project, claim).WithObjects(project, claim).Build()
	if err := zitadelapplication.NewReconciler(client, remote).Reconcile(t.Context(), types.NamespacedName{Namespace: "identity", Name: "web"}); err != nil {
		t.Fatal(err)
	}
	if remote.updateCalls != 1 || len(remote.updateInput.RedirectURIs) != 1 || remote.updateInput.RedirectURIs[0] != "https://product.example/callback" {
		t.Fatalf("redirect drift was not corrected: updates=%d input=%+v", remote.updateCalls, remote.updateInput)
	}
	if remote.updateInput.RefreshToken == nil || !*remote.updateInput.RefreshToken {
		t.Fatal("omitting refreshToken must preserve an existing refresh grant")
	}
}

func TestReconcile_rejects_duplicate_redirect_uris_before_remote_call(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := identityv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	project := &identityv1alpha1.ZitadelProject{ObjectMeta: metav1.ObjectMeta{Name: "homechef", Namespace: "identity"}, Status: readyProjectStatus("project-123")}
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
	project := &identityv1alpha1.ZitadelProject{ObjectMeta: metav1.ObjectMeta{Name: "homechef", Namespace: "identity"}, Status: readyProjectStatus("project-123")}
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

func TestReconcile_heals_adopted_application_missing_userinfo_assertion(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := identityv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	project := &identityv1alpha1.ZitadelProject{ObjectMeta: metav1.ObjectMeta{Name: "homechef", Namespace: "identity"}, Status: readyProjectStatus("project-123")}
	claim := &identityv1alpha1.ZitadelApplication{
		ObjectMeta: metav1.ObjectMeta{Name: "homechef-ios", Namespace: "identity"},
		Spec: identityv1alpha1.ZitadelApplicationSpec{
			ProjectRef:      identityv1alpha1.ZitadelProjectReference{Name: "homechef"},
			DisplayName:     "HomeChef iOS",
			ApplicationType: "native",
			RedirectURIs:    []string{"com.homechef.app:/oauth/callback"},
		},
	}
	remote := &fakeApplications{existing: zitadelapplication.Application{ID: "app-123", ClientID: "client-123", Name: "HomeChef iOS", OIDCConfig: zitadelapi.OIDCConfig{AuthMethodType: "OIDC_AUTH_METHOD_TYPE_NONE"}}, existingFound: true}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project, claim).WithObjects(project, claim).Build()
	reconciler := zitadelapplication.NewReconciler(client, remote)

	if err := reconciler.Reconcile(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}); err != nil {
		t.Fatal(err)
	}
	if remote.createCalls != 0 || remote.updateCalls != 1 {
		t.Fatalf("create calls = %d, update calls = %d, want 0 and 1", remote.createCalls, remote.updateCalls)
	}
	if got := remote.updateInput.RedirectURIs; len(got) != 1 || got[0] != "com.homechef.app:/oauth/callback" {
		t.Fatalf("update input redirect uris = %#v", got)
	}
}

func TestReconcile_leaves_adopted_application_with_userinfo_assertion_untouched(t *testing.T) {
	t.Parallel()

	scheme := runtime.NewScheme()
	if err := identityv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	project := &identityv1alpha1.ZitadelProject{ObjectMeta: metav1.ObjectMeta{Name: "homechef", Namespace: "identity"}, Status: readyProjectStatus("project-123")}
	claim := &identityv1alpha1.ZitadelApplication{
		ObjectMeta: metav1.ObjectMeta{Name: "homechef-ios", Namespace: "identity"},
		Spec: identityv1alpha1.ZitadelApplicationSpec{
			ProjectRef:      identityv1alpha1.ZitadelProjectReference{Name: "homechef"},
			DisplayName:     "HomeChef iOS",
			ApplicationType: "native",
			RedirectURIs:    []string{"com.homechef.app:/oauth/callback"},
		},
	}
	remote := &fakeApplications{existing: zitadelapplication.Application{ID: "app-123", ClientID: "client-123", Name: "HomeChef iOS", OIDCConfig: zitadelapplication.OIDCConfig{
		LoginVersion:             json.RawMessage(`{"loginV2":{}}`),
		IDTokenUserinfoAssertion: true, AppType: "OIDC_APP_TYPE_NATIVE", AuthMethodType: "OIDC_AUTH_METHOD_TYPE_NONE", AccessTokenType: "OIDC_TOKEN_TYPE_JWT",
		RedirectURIs: []string{"com.homechef.app:/oauth/callback"}, GrantTypes: []string{"OIDC_GRANT_TYPE_AUTHORIZATION_CODE"}, ResponseTypes: []string{"OIDC_RESPONSE_TYPE_CODE"},
	}}, existingFound: true}
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project, claim).WithObjects(project, claim).Build()
	reconciler := zitadelapplication.NewReconciler(client, remote)

	if err := reconciler.Reconcile(context.Background(), types.NamespacedName{Namespace: claim.Namespace, Name: claim.Name}); err != nil {
		t.Fatal(err)
	}
	if remote.createCalls != 0 || remote.updateCalls != 0 {
		t.Fatalf("create calls = %d, update calls = %d, want 0 and 0", remote.createCalls, remote.updateCalls)
	}
}

type fakeApplications struct {
	created       zitadelapplication.Application
	input         zitadelapplication.ApplicationInput
	createCalls   int
	existing      zitadelapplication.Application
	existingFound bool
	updateCalls   int
	updateInput   zitadelapplication.ApplicationInput
}

func (f *fakeApplications) FindApplicationByID(context.Context, string, string, string) (zitadelapplication.Application, bool, error) {
	return zitadelapplication.Application{}, false, nil
}

func (f *fakeApplications) FindApplicationByName(context.Context, string, string, string) (zitadelapplication.Application, bool, error) {
	return f.existing, f.existingFound, nil
}

func (f *fakeApplications) UpdateOIDCConfig(_ context.Context, _ string, _ string, _ string, input zitadelapplication.ApplicationInput) error {
	f.updateCalls++
	f.updateInput = input
	return nil
}

func (f *fakeApplications) CreateApplication(_ context.Context, _ string, _ string, input zitadelapplication.ApplicationInput) (zitadelapplication.Application, error) {
	f.createCalls++
	f.input = input
	return f.created, nil
}

var _ zitadelapplication.Applications = (*fakeApplications)(nil)
var _ = zitadelapi.Application{}

func TestReconcile_rejects_unsafe_callback_uris(t *testing.T) {
	for _, tc := range []struct{ kind, uri string }{
		{"native", "javascript:alert(1)"}, {"native", "data:text/html,test"}, {"native", "file:///tmp/callback"}, {"native", "http://public.example/callback"},
		{"user_agent", "https://" + "user" + ":" + "example" + "@product.example/callback"}, {"user_agent", "https://*.example/callback"}, {"user_agent", "https://product.example/callback#token"},
		{"native", "product:/callback#token"}, {"web", "https://product.example/callback#fragment"},
	} {
		t.Run(tc.kind+"/"+tc.uri, func(t *testing.T) {
			scheme := runtime.NewScheme()
			if err := identityv1alpha1.AddToScheme(scheme); err != nil {
				t.Fatal(err)
			}
			claim := &identityv1alpha1.ZitadelApplication{ObjectMeta: metav1.ObjectMeta{Name: "bad", Namespace: "identity"}, Spec: identityv1alpha1.ZitadelApplicationSpec{ProjectRef: identityv1alpha1.ZitadelProjectReference{Name: "p"}, DisplayName: "Bad", ApplicationType: tc.kind, RedirectURIs: []string{tc.uri}}}
			project := &identityv1alpha1.ZitadelProject{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "identity"}, Status: readyProjectStatus("p")}
			remote := &fakeApplications{created: zitadelapi.Application{ID: "a", ClientID: "c"}}
			c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(claim, project).WithObjects(claim, project).Build()
			if err := zitadelapplication.NewReconciler(c, remote).Reconcile(t.Context(), types.NamespacedName{Namespace: "identity", Name: "bad"}); err == nil {
				t.Fatal("unsafe callback accepted")
			}
			if remote.createCalls != 0 {
				t.Fatal("unsafe callback reached Zitadel")
			}
		})
	}
}

func TestReconcile_does_not_provision_against_a_failed_project_generation(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := identityv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	project := &identityv1alpha1.ZitadelProject{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "identity", Generation: 2}, Status: identityv1alpha1.ZitadelProjectStatus{ProjectID: "p", ObservedGeneration: 1, Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionFalse, Reason: "ReconcileFailed"}}}}
	claim := &identityv1alpha1.ZitadelApplication{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "identity"}, Spec: identityv1alpha1.ZitadelApplicationSpec{ProjectRef: identityv1alpha1.ZitadelProjectReference{Name: "p"}, DisplayName: "Web", ApplicationType: "user_agent", RedirectURIs: []string{"https://product.example/callback"}}}
	remote := &fakeApplications{created: zitadelapi.Application{ID: "a", ClientID: "c"}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project, claim).WithObjects(project, claim).Build()
	if err := zitadelapplication.NewReconciler(c, remote).Reconcile(t.Context(), types.NamespacedName{Namespace: "identity", Name: "web"}); err == nil {
		t.Fatal("application provisioned against a failed project")
	}
	if remote.createCalls != 0 {
		t.Fatal("failed project reached Zitadel")
	}
}

func readyProjectStatus(id string) identityv1alpha1.ZitadelProjectStatus {
	return identityv1alpha1.ZitadelProjectStatus{ProjectID: id, Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Reconciled"}}}
}

func TestReconcile_marks_previously_ready_application_not_ready_after_validation_failure(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := identityv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	claim := &identityv1alpha1.ZitadelApplication{ObjectMeta: metav1.ObjectMeta{Name: "bad", Namespace: "identity", Generation: 2}, Spec: identityv1alpha1.ZitadelApplicationSpec{}, Status: identityv1alpha1.ZitadelApplicationStatus{AppID: "a", ObservedGeneration: 1, Conditions: []metav1.Condition{{Type: "Ready", Status: metav1.ConditionTrue, Reason: "Reconciled"}}}}
	c := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(claim).WithObjects(claim).Build()
	if err := zitadelapplication.NewReconciler(c, &fakeApplications{}).Reconcile(t.Context(), types.NamespacedName{Namespace: "identity", Name: "bad"}); err == nil {
		t.Fatal("invalid claim accepted")
	}
	stored := &identityv1alpha1.ZitadelApplication{}
	if err := c.Get(t.Context(), types.NamespacedName{Namespace: "identity", Name: "bad"}, stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.Status.Conditions) != 1 || stored.Status.Conditions[0].Status != metav1.ConditionFalse || stored.Status.ObservedGeneration != 2 {
		t.Fatalf("stale ready status retained: %+v", stored.Status)
	}
}

func TestReconcile_never_converts_a_confidential_client_to_a_public_client(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := identityv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	project := &identityv1alpha1.ZitadelProject{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "identity"}, Status: readyProjectStatus("p")}
	claim := &identityv1alpha1.ZitadelApplication{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "identity"}, Spec: identityv1alpha1.ZitadelApplicationSpec{ProjectRef: identityv1alpha1.ZitadelProjectReference{Name: "p"}, DisplayName: "Web", ApplicationType: "web", RedirectURIs: []string{"https://product.example/callback"}}}
	remote := &fakeApplications{existingFound: true, existing: zitadelapi.Application{ID: "a", ClientID: "c", OIDCConfig: zitadelapi.OIDCConfig{AuthMethodType: "OIDC_AUTH_METHOD_TYPE_BASIC"}}}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project, claim).WithObjects(project, claim).Build()
	if err := zitadelapplication.NewReconciler(kube, remote).Reconcile(t.Context(), types.NamespacedName{Namespace: "identity", Name: "web"}); err == nil {
		t.Fatal("confidential client adopted as public")
	}
	if remote.createCalls+remote.updateCalls != 0 {
		t.Fatal("confidential client configuration was modified")
	}
}

func TestReconcile_never_adopts_a_client_with_unknown_authentication_method(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := identityv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	project := &identityv1alpha1.ZitadelProject{ObjectMeta: metav1.ObjectMeta{Name: "p", Namespace: "identity"}, Status: readyProjectStatus("p")}
	claim := &identityv1alpha1.ZitadelApplication{ObjectMeta: metav1.ObjectMeta{Name: "web", Namespace: "identity"}, Spec: identityv1alpha1.ZitadelApplicationSpec{ProjectRef: identityv1alpha1.ZitadelProjectReference{Name: "p"}, DisplayName: "Web", ApplicationType: "web", RedirectURIs: []string{"https://product.example/callback"}}}
	remote := &fakeApplications{existingFound: true, existing: zitadelapi.Application{ID: "a", ClientID: "c", OIDCConfig: zitadelapi.OIDCConfig{AuthMethodType: ""}}}
	kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(project, claim).WithObjects(project, claim).Build()
	if err := zitadelapplication.NewReconciler(kube, remote).Reconcile(t.Context(), types.NamespacedName{Namespace: "identity", Name: "web"}); err == nil {
		t.Fatal("confidential client adopted as public")
	}
	if remote.createCalls+remote.updateCalls != 0 {
		t.Fatal("confidential client configuration was modified")
	}
}
