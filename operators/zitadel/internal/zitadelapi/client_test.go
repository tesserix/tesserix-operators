package zitadelapi_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapi"
)

func TestClient_creates_project_with_organization_scoped_authorization(t *testing.T) {
	t.Parallel()

	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodPost {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("authorization = %q", got)
		}
		switch r.URL.Path {
		case "/management/v1/orgs/_search":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"result":[{"id":"org-123","name":"TESSERIX"}]}`))
			return
		case "/management/v1/projects":
			if got := r.Header.Get("X-Zitadel-Orgid"); got != "org-123" {
				t.Fatalf("organization header = %q", got)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(body), `"name":"HomeChef"`) {
				t.Fatalf("request body = %s", body)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"project-123"}`))
			return
		default:
			t.Fatalf("request path = %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client, err := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "test-token", nil })
	if err != nil {
		t.Fatal(err)
	}

	project, err := client.Create(context.Background(), "TESSERIX", zitadelapi.ProjectInput{DisplayName: "HomeChef"})
	if err != nil {
		t.Fatal(err)
	}
	if project.ID != "project-123" {
		t.Fatalf("project ID = %q", project.ID)
	}
	if requests != 2 {
		t.Fatalf("request count = %d, want 2", requests)
	}
}

func TestClient_creates_native_public_application_with_PKCE_safe_configuration(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/management/v1/orgs/_search":
			_, _ = w.Write([]byte(`{"result":[{"id":"org-123","name":"TESSERIX"}]}`))
		case "/management/v1/projects/project-123/apps/oidc":
			if got := r.Header.Get("X-Zitadel-Orgid"); got != "org-123" {
				t.Fatalf("organization header = %q", got)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				`"appType":"OIDC_APP_TYPE_NATIVE"`,
				`"authMethodType":"OIDC_AUTH_METHOD_TYPE_NONE"`,
				`"responseTypes":["OIDC_RESPONSE_TYPE_CODE"]`,
				`"grantTypes":["OIDC_GRANT_TYPE_AUTHORIZATION_CODE"]`,
			} {
				if !strings.Contains(string(body), want) {
					t.Fatalf("request body = %s, missing %s", body, want)
				}
			}
			_, _ = w.Write([]byte(`{"appId":"app-123","clientId":"client-123"}`))
		default:
			t.Fatalf("request path = %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client, err := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "test-token", nil })
	if err != nil {
		t.Fatal(err)
	}
	app, err := client.CreateApplication(context.Background(), "TESSERIX", "project-123", zitadelapi.ApplicationInput{DisplayName: "HomeChef iOS", AppType: "native", RedirectURIs: []string{"com.homechef.app:/oauth/callback"}})
	if err != nil {
		t.Fatal(err)
	}
	if app.ID != "app-123" || app.ClientID != "client-123" {
		t.Fatalf("application = %#v", app)
	}
}
