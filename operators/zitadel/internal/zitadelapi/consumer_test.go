package zitadelapi_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	identity "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapi"
)

type consumerSecrets map[string]string

func (s consumerSecrets) Latest(_ context.Context, name string) (string, bool, error) {
	v, ok := s[name]
	return v, ok, nil
}

func TestConsumerOrganization_scopes_social_only_policy_and_branding_and_reuses_ids(t *testing.T) {
	created, providers := false, map[string]string{}
	orgCreates, providerCreates := 0, 0
	loginCreates, loginUpdates := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if r.Body != nil && r.ContentLength != 0 {
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
			}
		}
		path := r.URL.Path
		if path != "/v2/users/admin" && path != "/admin/v1/orgs/consumer-civiclens" && path != "/v2/organizations" && r.Header.Get("x-zitadel-orgid") != "consumer-civiclens" {
			t.Errorf("wrong organization for %s", path)
		}
		switch path {
		case "/v2/users/admin":
			fmt.Fprint(w, `{"user":{"userId":"admin","state":"USER_STATE_ACTIVE","human":{"email":{"isVerified":true}}}}`)
		case "/admin/v1/orgs/consumer-civiclens":
			if !created {
				w.WriteHeader(404)
				return
			}
			fmt.Fprint(w, `{"org":{"id":"consumer-civiclens","name":"CivicLens","state":"ORG_STATE_ACTIVE"}}`)
		case "/v2/organizations":
			created = true
			orgCreates++
			if body["organizationId"] != "consumer-civiclens" {
				t.Error("non-deterministic id")
			}
			fmt.Fprint(w, `{"organizationId":"consumer-civiclens"}`)
		case "/management/v1/metadata/consumer_binding":
			if r.Method == "GET" {
				fmt.Fprintf(w, `{"metadata":{"key":"consumer_binding","value":%q}}`, base64.StdEncoding.EncodeToString([]byte("identity-operator/civiclens")))
			}
		case "/management/v1/idps/templates/_search":
			result := []map[string]any{}
			for name, id := range providers {
				configKind := name
				if name == "facebook" {
					configKind = "oauth"
				}
				result = append(result, map[string]any{"id": id, "name": strings.ToUpper(name[:1]) + name[1:], "state": "IDP_STATE_ACTIVE", "owner": "IDP_OWNER_TYPE_ORG", "config": map[string]any{configKind: map[string]any{}, "options": map[string]any{"isAutoCreation": true, "isCreationAllowed": true}}})
			}
			json.NewEncoder(w).Encode(map[string]any{"result": result})
		case "/management/v1/idps/google", "/management/v1/idps/apple", "/management/v1/idps/oauth":
			providerCreates++
			name := strings.ToLower(body["name"].(string))
			providers[name] = name + "-id"
			options := body["providerOptions"].(map[string]any)
			if options["isCreationAllowed"] != true || options["isAutoCreation"] != true || options["isLinkingAllowed"] != false || options["autoLinking"] != "AUTO_LINKING_OPTION_UNSPECIFIED" {
				t.Errorf("unsafe options: %v", options)
			}
			fmt.Fprintf(w, `{"id":%q}`, name+"-id")
		case "/management/v1/idps/google/google-id", "/management/v1/idps/apple/apple-id", "/management/v1/idps/oauth/facebook-id":
			if r.Method != http.MethodPut {
				t.Errorf("provider update method = %s, want PUT", r.Method)
			}
			fmt.Fprint(w, `{}`)
		case "/management/v1/policies/login":
			if r.Method == "GET" {
				if loginCreates == 0 {
					fmt.Fprint(w, `{"isDefault":true,"policy":{"allowUsernamePassword":true,"allowRegister":true,"externalLoginCheckLifetime":"864000s"}}`)
				} else {
					fmt.Fprint(w, `{"policy":{"allowExternalIdp":true,"hidePasswordReset":true,"externalLoginCheckLifetime":"43200s"}}`)
				}
				return
			}
			for _, key := range []string{"allowUsernamePassword", "allowRegister", "allowDomainDiscovery"} {
				if body[key] != false {
					t.Errorf("%s must be disabled", key)
				}
			}
			if body["allowExternalIdp"] != true || body["passwordlessType"] != "PASSWORDLESS_TYPE_NOT_ALLOWED" {
				t.Errorf("incorrect login policy: %v", body)
			}
			if r.Method == "PUT" {
				loginUpdates++
			}
			if r.Method == "POST" {
				loginCreates++
				idps, ok := body["idps"].([]any)
				if !ok || len(idps) != 3 {
					t.Error("must create policy with exact provider list")
				}
			}
		case "/management/v1/policies/login/idps/_search":
			fmt.Fprint(w, `{"result":[{"idpId":"google-id"},{"idpId":"apple-id"},{"idpId":"facebook-id"}]}`)
		case "/management/v1/policies/label":
			if r.Method == "GET" {
				fmt.Fprint(w, `{"isDefault":true,"policy":{}}`)
				return
			}
			if body["primaryColor"] != "#3858F5" || body["backgroundColor"] != "#F7F8FA" {
				t.Errorf("incorrect branding %v", body)
			}
		case "/management/v1/policies/label/_activate":
		default:
			t.Errorf("unexpected %s %s", r.Method, path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	c, err := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "test", nil })
	if err != nil {
		t.Fatal(err)
	}
	c.UseConsumerSecrets(consumerSecrets{"civiclens-facebook-idp-prod": `{"clientId":"meta-client","clientSecret":"fixture-only"}`,
		"civiclens-google-idp-prod": `{"clientId":"google-client","clientSecret":"fixture-only"}`, "civiclens-apple-idp-prod": `{"clientId":"apple-client","teamId":"TEAM123456","keyId":"KEY1234567","privateKey":"Zml4dHVyZQ=="}`})
	spec := identity.ConsumerOrganization{InitialAdminUserID: "admin", Providers: []identity.ConsumerProvider{{Kind: "facebook", SecretName: "civiclens-facebook-idp-prod"}, {Kind: "google", SecretName: "civiclens-google-idp-prod"}, {Kind: "apple", SecretName: "civiclens-apple-idp-prod"}}, PrimaryColor: "#3858F5", BackgroundColor: "#F7F8FA", FontColor: "#151923"}
	var previous *identity.ConsumerIdentity
	for range 2 {
		result, err := c.EnsureConsumerOrganization(t.Context(), "identity-operator/civiclens", "CivicLens", spec, previous)
		previous = result
		if err != nil {
			t.Fatal(err)
		}
		if result.OrganizationID != "consumer-civiclens" || result.ProviderIDs["apple"] != "apple-id" {
			t.Fatalf("wrong receipt: %+v", result)
		}
	}
	if loginCreates != 1 || loginUpdates != 0 {
		t.Fatalf("unchanged social policy rewritten: creates %d updates %d", loginCreates, loginUpdates)
	}
	if orgCreates != 1 || len(providers) != 3 || providerCreates != 3 {
		t.Fatalf("duplicate provisioning: orgs %d provider creates %d providers %v", orgCreates, providerCreates, providers)
	}
}

func TestConsumerOrganization_rejects_foreign_or_missing_secrets_before_remote_writes(t *testing.T) {
	for _, secret := range []string{"roamie-apple-idp-prod", "civiclens-missing"} {
		t.Run(secret, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Error("unreviewed credential must not reach Zitadel") }))
			t.Cleanup(server.Close)
			c, err := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "test", nil })
			if err != nil {
				t.Fatal(err)
			}
			c.UseConsumerSecrets(consumerSecrets{})
			_, err = c.EnsureConsumerOrganization(t.Context(), "identity-operator/civiclens", "CivicLens", identity.ConsumerOrganization{InitialAdminUserID: "admin", PrimaryColor: "#3858F5", BackgroundColor: "#F7F8FA", FontColor: "#151923", Providers: []identity.ConsumerProvider{{Kind: "apple", SecretName: secret}}}, nil)
			if err == nil {
				t.Fatal("unreviewed secret accepted")
			}
		})
	}
}
