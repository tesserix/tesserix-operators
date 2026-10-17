package zitadelapi_test

import (
	"context"
	"encoding/json"
	"fmt"
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
		case "/management/v1/projects/project-123/apps/app-123":
			_, _ = w.Write([]byte(`{"app":{"id":"app-123","oidcConfig":{"clientId":"client-123"}}}`))
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

func TestClient_oidc_update_preserves_custom_login_and_unmanaged_settings(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/organizations/_search":
			_, _ = w.Write([]byte(`{"result":[{"id":"o","name":"TESSERIX"}]}`))
		case "/management/v1/projects/p/apps/a":
			_, _ = w.Write([]byte(`{"app":{"id":"a","oidcConfig":{"clientId":"c","loginVersion":{"loginV2":{"baseUri":"https://helivanta.app"}},"backChannelLogoutUri":"https://helivanta.app/logout/backchannel","additionalOrigins":["https://helivanta.app"],"clockSkew":"2s","idTokenRoleAssertion":true,"accessTokenRoleAssertion":true,"skipNativeAppSuccessPage":true}}}`))
		case "/management/v1/projects/p/apps/a/oidc_config":
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Error(err)
			}
			_, _ = w.Write([]byte(`{}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)
	client, err := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "test-token", nil })
	if err != nil {
		t.Fatal(err)
	}
	if err := client.UpdateOIDCConfig(t.Context(), "TESSERIX", "p", "a", zitadelapi.ApplicationInput{AppType: "native", RedirectURIs: []string{"product:/callback"}}); err != nil {
		t.Fatal(err)
	}
	if login, ok := got["loginVersion"].(map[string]any); !ok || login["loginV2"].(map[string]any)["baseUri"] != "https://helivanta.app" {
		t.Fatalf("custom login lost: %v", got)
	}
	for key, want := range map[string]any{"backChannelLogoutUri": "https://helivanta.app/logout/backchannel", "clockSkew": "2s", "idTokenRoleAssertion": true, "accessTokenRoleAssertion": true, "skipNativeAppSuccessPage": true} {
		if got[key] != want {
			t.Errorf("%s=%v want %v", key, got[key], want)
		}
	}
	if origins, ok := got["additionalOrigins"].([]any); !ok || len(origins) != 1 || origins[0] != "https://helivanta.app" {
		t.Errorf("origins lost: %v", got)
	}
}

func TestClient_creates_web_pkce_client_with_product_login_v2(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/organizations/_search" {
			_, _ = w.Write([]byte(`{"result":[{"id":"o"}]}`))
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["appType"] != "OIDC_APP_TYPE_WEB" {
			t.Errorf("web client type=%v", body["appType"])
		}
		login, ok := body["loginVersion"].(map[string]any)
		if !ok {
			t.Errorf("Login V2 not configured: %v", body)
		} else if v2, ok := login["loginV2"].(map[string]any); !ok || v2["baseUri"] != "https://helivanta.app" {
			t.Errorf("custom login=%v", login)
		}
		_, _ = w.Write([]byte(`{"appId":"a","clientId":"c"}`))
	}))
	t.Cleanup(server.Close)
	c, err := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "test-token", nil })
	if err != nil {
		t.Fatal(err)
	}
	var input zitadelapi.ApplicationInput
	if err := json.Unmarshal([]byte(`{"AppType":"web","LoginBaseURI":"https://helivanta.app"}`), &input); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateApplication(t.Context(), "TESSERIX", "p", input); err != nil {
		t.Fatal(err)
	}
}

func TestClient_refuses_email_grants_for_unverified_or_foreign_users(t *testing.T) {
	for _, result := range []string{
		`{"id":"u","details":{"resourceOwner":"org-123"},"human":{"email":{"email":"admin@example.com","isEmailVerified":false}}}`,
		`{"id":"u","details":{"resourceOwner":"other-org"},"human":{"email":{"email":"admin@example.com","isEmailVerified":true}}}`,
		`{"id":"u","details":{"resourceOwner":"org-123"},"machine":{"name":"admin@example.com"}}`,
	} {
		t.Run(result, func(t *testing.T) {
			c := machineUserServer(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte(`{"result":[` + result + `]}`)) })
			if id, found, err := c.FindUserByEmail(t.Context(), "TESSERIX", "admin@example.com"); err == nil || found || id != "" {
				t.Fatalf("unsafe grant target accepted: id=%q found=%v err=%v", id, found, err)
			}
		})
	}
}

func TestClient_creates_project_with_access_gate_before_apps_can_use_it(t *testing.T) {
	c := machineUserServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["projectRoleCheck"] != true || body["hasProjectCheck"] != true {
			t.Errorf("project created without requested gates: %v", body)
		}
		_, _ = w.Write([]byte(`{"id":"p"}`))
	})
	var input zitadelapi.ProjectInput
	if err := json.Unmarshal([]byte(`{"DisplayName":"Hospital","RoleCheck":true,"OrganizationCheck":true}`), &input); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Create(t.Context(), "TESSERIX", input); err != nil {
		t.Fatal(err)
	}
}

func TestClient_lists_grants_beyond_first_page_before_reconciliation(t *testing.T) {
	calls := 0
	c := machineUserServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body struct {
			Query struct {
				Offset json.Number `json:"offset"`
			} `json:"query"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if calls == 1 {
			results := make([]map[string]any, 200)
			for i := range results {
				results[i] = map[string]any{"id": fmt.Sprint(i), "userId": fmt.Sprint(i), "roleKeys": []string{"member"}}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"result": results})
		} else {
			if body.Query.Offset.String() != "200" {
				t.Errorf("offset=%s want 200", body.Query.Offset)
			}
			_, _ = w.Write([]byte(`{"result":[{"id":"last","userId":"last","roleKeys":["member"]}]}`))
		}
	})
	grants, err := c.ListGrants(t.Context(), "TESSERIX", "p")
	if err != nil {
		t.Fatal(err)
	}
	if len(grants) != 201 || grants[len(grants)-1].ID != "last" || calls != 2 {
		t.Fatalf("incomplete grants: count=%d calls=%d", len(grants), calls)
	}
}

func TestClient_refuses_project_owned_by_another_organization(t *testing.T) {
	c := machineUserServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"project":{"id":"p","name":"Product","details":{"resourceOwner":"foreign-org"}}}`))
	})
	if project, found, err := c.FindByID(t.Context(), "TESSERIX", "p"); err == nil || found || project.ID != "" {
		t.Fatalf("foreign project accepted: found=%v err=%v", found, err)
	}
}

func TestClient_never_follows_management_redirects(t *testing.T) {
	followed := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/organizations/_search" {
			http.Redirect(w, r, "/unexpected", http.StatusTemporaryRedirect)
			return
		}
		followed = true
		_, _ = w.Write([]byte(`{"result":[]}`))
	}))
	t.Cleanup(server.Close)
	c, err := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "test-token", nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.FindByName(t.Context(), "TESSERIX", "Product"); err == nil {
		t.Fatal("management redirect was accepted")
	}
	if followed {
		t.Fatal("management authorization followed a redirect")
	}
}

func TestOIDCConfig_matches_web_when_zitadel_omits_zero_value_app_type(t *testing.T) {
	config := zitadelapi.OIDCConfig{AuthMethodType: "OIDC_AUTH_METHOD_TYPE_NONE", AccessTokenType: "OIDC_TOKEN_TYPE_JWT", IDTokenUserinfoAssertion: true, RedirectURIs: []string{"https://product.example/callback"}, ResponseTypes: []string{"OIDC_RESPONSE_TYPE_CODE"}, GrantTypes: []string{"OIDC_GRANT_TYPE_AUTHORIZATION_CODE"}}
	if !config.Matches(zitadelapi.ApplicationInput{AppType: "web", RedirectURIs: []string{"https://product.example/callback"}}) {
		t.Fatal("valid Web client would be updated repeatedly because protobuf JSON omitted its zero-valued enum")
	}
}

func TestClient_preserves_helivanta_token_preferences_when_declared(t *testing.T) {
	c := machineUserServer(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if body["accessTokenType"] != "OIDC_TOKEN_TYPE_BEARER" || body["idTokenUserinfoAssertion"] != false {
			t.Errorf("existing product token preferences changed: %v", body)
		}
		_, _ = w.Write([]byte(`{"appId":"a","clientId":"c"}`))
	})
	var input zitadelapi.ApplicationInput
	if err := json.Unmarshal([]byte(`{"AppType":"web","AccessTokenType":"bearer","IDTokenUserinfoAssertion":false}`), &input); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateApplication(t.Context(), "TESSERIX", "p", input); err != nil {
		t.Fatal(err)
	}
}
