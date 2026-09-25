package zitadelapi_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapi"
)

func TestClient_refresh_grant_requires_explicit_opt_in(t *testing.T) {
	yes, no := true, false
	for _, tc := range []struct {
		name    string
		enabled *bool
		want    []string
	}{
		{"legacy", nil, []string{"OIDC_GRANT_TYPE_AUTHORIZATION_CODE"}},
		{"disabled", &no, []string{"OIDC_GRANT_TYPE_AUTHORIZATION_CODE"}},
		{"enabled", &yes, []string{"OIDC_GRANT_TYPE_AUTHORIZATION_CODE", "OIDC_GRANT_TYPE_REFRESH_TOKEN"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/v2/organizations/_search" {
					_, _ = w.Write([]byte(`{"result":[{"id":"org-123","name":"TESSERIX"}]}`))
					return
				}
				var body struct {
					GrantTypes []string `json:"grantTypes"`
					AuthMethod string   `json:"authMethodType"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Error(err)
					return
				}
				if !reflect.DeepEqual(body.GrantTypes, tc.want) || body.AuthMethod != "OIDC_AUTH_METHOD_TYPE_NONE" {
					t.Errorf("grants=%v auth=%s; want %v and public PKCE", body.GrantTypes, body.AuthMethod, tc.want)
				}
				_, _ = w.Write([]byte(`{"appId":"app-123","clientId":"client-123"}`))
			}))
			t.Cleanup(server.Close)
			c, err := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "test-token", nil })
			if err != nil {
				t.Fatal(err)
			}
			_, err = c.CreateApplication(t.Context(), "TESSERIX", "project-123", zitadelapi.ApplicationInput{AppType: "native", DisplayName: "Roamie", RefreshToken: tc.enabled})
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestClient_adopts_client_id_from_remote_oidc_configuration(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/organizations/_search":
			_, _ = w.Write([]byte(`{"result":[{"id":"org-123","name":"TESSERIX"}]}`))
		case "/management/v1/projects/project-123/apps/_search":
			_, _ = w.Write([]byte(`{"result":[{"id":"app-123","name":"Roamie iOS","oidcConfig":{"clientId":"client-123","idTokenUserinfoAssertion":true}}]}`))
		case "/management/v1/projects/project-123/apps/app-123":
			_, _ = w.Write([]byte(`{"app":{"id":"app-123","name":"Roamie iOS","oidcConfig":{"clientId":"client-123","idTokenUserinfoAssertion":true}}}`))
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	c, err := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "test-token", nil })
	if err != nil {
		t.Fatal(err)
	}
	byName, found, err := c.FindApplicationByName(t.Context(), "TESSERIX", "project-123", "Roamie iOS")
	if err != nil || !found || byName.ClientID != "client-123" {
		t.Fatalf("adoption by name: client id=%q found=%v err=%v", byName.ClientID, found, err)
	}
	byID, found, err := c.FindApplicationByID(t.Context(), "TESSERIX", "project-123", "app-123")
	if err != nil || !found || byID.ClientID != "client-123" {
		t.Fatalf("adoption by id: client id=%q found=%v err=%v", byID.ClientID, found, err)
	}
}

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
		case "/v2/organizations/_search":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				`"limit":2`,
				`"nameQuery":{"method":"TEXT_QUERY_METHOD_EQUALS","name":"TESSERIX"}`,
			} {
				if !strings.Contains(string(body), want) {
					t.Fatalf("organization lookup body = %s, missing %s", body, want)
				}
			}
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
		case "/v2/organizations/_search":
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
				`"idTokenUserinfoAssertion":true`,
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

func TestClient_update_oidc_config_replaces_configuration_with_userinfo_assertion(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/organizations/_search":
			_, _ = w.Write([]byte(`{"result":[{"id":"org-123","name":"TESSERIX"}]}`))
		case "/management/v1/projects/project-123/apps/app-123/oidc_config":
			if r.Method != http.MethodPut {
				t.Fatalf("method = %s", r.Method)
			}
			if got := r.Header.Get("X-Zitadel-Orgid"); got != "org-123" {
				t.Fatalf("organization header = %q", got)
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			for _, want := range []string{
				`"idTokenUserinfoAssertion":true`,
				`"appType":"OIDC_APP_TYPE_NATIVE"`,
				`"authMethodType":"OIDC_AUTH_METHOD_TYPE_NONE"`,
				`"redirectUris":["com.homechef.app:/oauth/callback"]`,
			} {
				if !strings.Contains(string(body), want) {
					t.Fatalf("request body = %s, missing %s", body, want)
				}
			}
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Fatalf("request path = %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	client, err := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "test-token", nil })
	if err != nil {
		t.Fatal(err)
	}
	err = client.UpdateOIDCConfig(context.Background(), "TESSERIX", "project-123", "app-123", zitadelapi.ApplicationInput{AppType: "native", RedirectURIs: []string{"com.homechef.app:/oauth/callback"}})
	if err != nil {
		t.Fatal(err)
	}
}
