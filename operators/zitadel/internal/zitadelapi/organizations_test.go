package zitadelapi_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	identity "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapi"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOrganizationAdoptionChecksTenantBeforeAnyMutation(t *testing.T) {
	tenant := "11111111-1111-4111-8111-111111111111"
	reauthLifetime := fmt.Sprintf("%ds", 3600)
	for _, valid := range []bool{false, true} {
		t.Run(fmt.Sprint(valid), func(t *testing.T) {
			writes := 0
			domainChecked := false
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v2/users/admin":
					_, _ = fmt.Fprint(w, `{"user":{"userId":"admin","state":"USER_STATE_ACTIVE","human":{"email":{"isVerified":true}}}}`)
				case "/admin/v1/orgs/hospital":
					_, _ = fmt.Fprint(w, `{"org":{"id":"hospital","name":"TEST Hospital","state":"ORG_STATE_ACTIVE"}}`)
				case "/management/v1/metadata/tenant_id":
					value := "foreign"
					if valid {
						value = tenant
					}
					_, _ = fmt.Fprintf(w, `{"metadata":{"key":"tenant_id","value":%q}}`, base64.StdEncoding.EncodeToString([]byte(value)))
				case "/v2/organizations/_search":
					_, _ = fmt.Fprint(w, `{"result":[{"id":"owner","name":"TESSERIX"}]}`)
				case "/management/v1/projects/project":
					_, _ = fmt.Fprint(w, `{"project":{"id":"project","details":{"resourceOwner":"owner"}}}`)
				case "/management/v1/projectgrants/_search":
					_, _ = fmt.Fprint(w, `{"result":[{"grantId":"grant","grantedOrgId":"hospital","projectId":"project","state":"PROJECT_GRANT_STATE_ACTIVE"}]}`)
				case "/admin/v1/orgs/hospital/policies/domain":
					domainChecked = true
					_, _ = fmt.Fprint(w, `{"policy":{"validateOrgDomains":true}}`)
				case "/management/v1/policies/login":
					if r.Method == "GET" {
						_ = json.NewEncoder(w).Encode(map[string]any{"isDefault": true, "policy": map[string]any{"allowUsernamePassword": true, "allowRegister": true, "allowExternalIdp": true, "secondFactors": []string{"SECOND_FACTOR_TYPE_OTP"}, "passwordCheckLifetime": reauthLifetime}})
						return
					}
					writes++
					var b map[string]any
					if e := json.NewDecoder(r.Body).Decode(&b); e != nil {
						t.Fatal(e)
					}
					if b["forceMfa"] != true || b["allowRegister"] != false || b["allowDomainDiscovery"] != false || b["forceMfaLocalOnly"] != false || b["passwordCheckLifetime"] != reauthLifetime {
						t.Errorf("unsafe policy: %v", b)
					}
					_, _ = fmt.Fprint(w, `{}`)
				default:
					if r.Method != "GET" {
						writes++
					}
					t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			c, _ := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "token", nil })
			id, err := c.EnsureOrganization(t.Context(), identity.ZitadelOrganizationSpec{InitialAdminUserID: "admin", Product: "helivanta", TenantID: tenant, DisplayName: "TEST Hospital", OrganizationID: "hospital"}, "TESSERIX", "project")
			if !valid {
				if err == nil || writes != 0 {
					t.Fatalf("foreign tenant mutated: %v writes=%d", err, writes)
				}
			} else if err != nil || id != "hospital" || writes != 1 || !domainChecked {
				t.Fatalf("adoption: %s %v writes=%d", id, err, writes)
			}
		})
	}
}

func TestNewOrganizationRecoversLostCreateResponseWithoutDuplicatingOwner(t *testing.T) {
	tenant := "11111111-1111-4111-8111-111111111111"
	id := "helivanta-" + tenant
	created := false
	metadata := false
	grant := false
	creates := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/users/admin":
			_, _ = fmt.Fprint(w, `{"user":{"userId":"admin","state":"USER_STATE_ACTIVE","human":{"email":{"isVerified":true}}}}`)
		case "/admin/v1/orgs/" + id:
			if !created {
				w.WriteHeader(404)
				return
			}
			_, _ = fmt.Fprintf(w, `{"org":{"id":%q,"name":"TEST Hospital","state":"ORG_STATE_ACTIVE"}}`, id)
		case "/v2/organizations":
			creates++
			var body struct {
				OrganizationID string `json:"organizationId"`
				Admins         []struct {
					UserID string   `json:"userId"`
					Roles  []string `json:"roles"`
				} `json:"admins"`
			}
			if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
				t.Fatal(e)
			}
			if body.OrganizationID != id || len(body.Admins) != 1 || body.Admins[0].UserID != "admin" || len(body.Admins[0].Roles) != 1 || body.Admins[0].Roles[0] != "ORG_OWNER" {
				t.Error("missing deterministic identity/explicit initial owner")
			}
			created = true
			w.WriteHeader(http.StatusInternalServerError)
			return
		case "/management/v1/metadata/tenant_id":
			if r.Method == "POST" {
				metadata = true
				_, _ = fmt.Fprint(w, `{}`)
				return
			}
			if !metadata {
				w.WriteHeader(404)
				return
			}
			_, _ = fmt.Fprintf(w, `{"metadata":{"key":"tenant_id","value":%q}}`, base64.StdEncoding.EncodeToString([]byte(tenant)))
		case "/v2/organizations/_search":
			_, _ = fmt.Fprint(w, `{"result":[{"id":"owner","name":"TESSERIX"}]}`)
		case "/management/v1/projects/project":
			_, _ = fmt.Fprint(w, `{"project":{"id":"project","details":{"resourceOwner":"owner"}}}`)
		case "/admin/v1/orgs/" + id + "/policies/domain":
			_, _ = fmt.Fprint(w, `{"policy":{"validateOrgDomains":true}}`)
		case "/management/v1/policies/login":
			_, _ = fmt.Fprint(w, `{"policy":{"forceMfa":true,"ignoreUnknownUsernames":true}}`)
		case "/management/v1/projectgrants/_search":
			if grant {
				_, _ = fmt.Fprintf(w, `{"result":[{"grantId":"grant","grantedOrgId":%q,"projectId":"project","state":"PROJECT_GRANT_STATE_ACTIVE"}]}`, id)
			} else {
				_, _ = fmt.Fprint(w, `{}`)
			}
		case "/management/v1/projects/project/grants":
			grant = true
			_, _ = fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	c, _ := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "token", nil })
	spec := identity.ZitadelOrganizationSpec{Product: "helivanta", TenantID: tenant, DisplayName: "TEST Hospital", InitialAdminUserID: "admin"}
	if _, e := c.EnsureOrganization(t.Context(), spec, "TESSERIX", "project"); e == nil {
		t.Fatal("lost create response should leave onboarding pending")
	}
	for range 2 {
		got, e := c.EnsureOrganization(t.Context(), spec, "TESSERIX", "project")
		if e != nil || got != id {
			t.Fatalf("creation/replay: %s %v", got, e)
		}
	}
	if creates != 1 {
		t.Fatalf("created %d organizations", creates)
	}
}
