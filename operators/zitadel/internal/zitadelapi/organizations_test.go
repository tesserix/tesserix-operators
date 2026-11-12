package zitadelapi_test

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	identity "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapi"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestFirstOwnerCreationWaitsForNativeEmailVerification(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost_create_response=%v", lost), func(t *testing.T) { testFirstOwnerCreation(t, lost, false, false) })
	}
}

func TestFirstOwnerCannotActivateAfterNativeOwnershipWasRemoved(t *testing.T) {
	testFirstOwnerCreation(t, false, true, false)
}

func testFirstOwnerCreation(t *testing.T, lost, revoked, external bool, displayNames ...string) {
	displayName := "TEST Hospital"
	if len(displayNames) > 0 {
		displayName = displayNames[0]
	}
	t.Helper()
	tenant := "11111111-1111-4111-8111-111111111111"
	orgID, userID := "helivanta-"+tenant, "owner-"+tenant
	var spec identity.ZitadelOrganizationSpec
	if err := json.Unmarshal([]byte(fmt.Sprintf(`{"product":"helivanta","tenantId":%q,"displayName":"TEST Hospital","initialAdminUserId":%q,"initialAdmin":{"givenName":"Test","familyName":"Owner","email":"owner@example.test"}}`, tenant, userID)), &spec); err != nil {
		t.Fatal(err)
	}
	spec.DisplayName = displayName
	if external {
		raw, err := json.Marshal(spec)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]any
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		fields["externalInitialAdminEmail"] = true
		raw, err = json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &spec); err != nil {
			t.Fatal(err)
		}
	}
	created, verified := false, false
	creates, grants := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin/v1/orgs/" + orgID:
			if !created {
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"org": map[string]any{"id": orgID, "name": displayName, "state": "ORG_STATE_ACTIVE"}})
		case "/v2/organizations":
			creates++
			var body struct {
				OrganizationID string `json:"organizationId"`
				Name           string `json:"name"`
				Admins         []struct {
					UserID string         `json:"userId"`
					Human  map[string]any `json:"human"`
					Roles  []string       `json:"roles"`
				} `json:"admins"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				w.WriteHeader(500)
				return
			}
			if body.OrganizationID != orgID || len(body.Admins) != 1 {
				t.Error("missing deterministic organization/owner")
				w.WriteHeader(500)
				return
			}
			if !strings.HasPrefix(body.Name, "Helivanta-") || strings.Contains(body.Name, tenant) || len(strings.ToLower(body.Name)) > 55 {
				t.Error("fresh native organization name must be readable with room for a unique domain suffix")
			}
			admin := body.Admins[0]
			if admin.UserID != "" || admin.Human["userId"] != userID || len(admin.Roles) != 1 || admin.Roles[0] != "ORG_OWNER" {
				t.Error("first owner must be an inline human")
			}
			if admin.Human["username"] != userID {
				t.Error("first owner login must be tenant-unique, independent of email")
			}
			email, ok := admin.Human["email"].(map[string]any)
			if !ok || email["email"] != "owner@example.test" || email["isVerified"] != nil || admin.Human["password"] != nil {
				t.Error("unsafe email verification/password provisioning")
			}
			if external {
				if email["returnCode"] == nil || email["sendCode"] != nil {
					t.Error("external delivery triggered shared SMTP")
				}
			} else {
				sendCode, _ := email["sendCode"].(map[string]any)
				if sendCode["urlTemplate"] != "https://helivanta.app/signup/verify#phase=email&userId={{.UserID}}&code={{.Code}}" || email["returnCode"] != nil {
					t.Error("native delivery lost Helivanta verification link")
				}
			}
			created = true
			if lost {
				w.WriteHeader(500)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"organizationId": orgID})
		case "/v2/users/" + userID:
			if !created {
				t.Error("looked up a first owner before creating their organization")
				w.WriteHeader(404)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{"userId": userID, "details": map[string]string{"resourceOwner": orgID}, "state": "USER_STATE_ACTIVE", "human": map[string]any{"email": map[string]any{"email": "owner@example.test", "isVerified": verified}}}})
		case "/management/v1/orgs/me/members/_search":
			if r.Header.Get("X-Zitadel-Orgid") != orgID {
				t.Error("owner membership query was not organization-scoped")
			}
			if revoked {
				_, _ = fmt.Fprint(w, `{}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"result": []map[string]any{{"userId": userID, "roles": []string{"ORG_OWNER"}}}})
		case "/management/v1/metadata/tenant_id":
			_ = json.NewEncoder(w).Encode(map[string]any{"metadata": map[string]string{"key": "tenant_id", "value": base64.StdEncoding.EncodeToString([]byte(tenant))}})
		case "/v2/organizations/_search":
			_, _ = fmt.Fprint(w, `{"result":[{"id":"platform","name":"TESSERIX"}]}`)
		case "/management/v1/projects/project":
			_, _ = fmt.Fprint(w, `{"project":{"id":"project","details":{"resourceOwner":"platform"}}}`)
		case "/admin/v1/orgs/" + orgID + "/policies/domain":
			_, _ = fmt.Fprint(w, `{"policy":{"validateOrgDomains":true}}`)
		case "/management/v1/policies/login":
			_, _ = fmt.Fprint(w, `{"policy":{"forceMfa":true,"ignoreUnknownUsernames":true}}`)
		case "/management/v1/projectgrants/_search":
			_, _ = fmt.Fprint(w, `{}`)
		case "/management/v1/projects/project/grants":
			grants++
			_, _ = fmt.Fprint(w, `{}`)
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	c, _ := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "token", nil })
	for attempt := range 2 {
		_, err := c.EnsureOrganization(t.Context(), spec, "TESSERIX", "project")
		if (!lost || attempt > 0) && !errors.Is(err, zitadelapi.ErrOwnerVerificationPending) {
			t.Fatalf("expected verification pending: %v", err)
		}
		if err == nil || !created || grants != 0 {
			t.Fatalf("must create exactly one unverified owner without product access: created=%v grants=%d err=%v", created, grants, err)
		}
	}
	verified = true
	got, err := c.EnsureOrganization(t.Context(), spec, "TESSERIX", "project")
	if revoked {
		if err == nil || creates != 1 || grants != 0 {
			t.Fatalf("revoked native owner activated: err=%v creates=%d grants=%d", err, creates, grants)
		}
		return
	}
	if err != nil || got != orgID || creates != 1 || grants != 1 {
		t.Fatalf("verified replay: id=%s err=%v creates=%d grants=%d", got, err, creates, grants)
	}
}

func TestFirstOwnerRejectsForeignOrUnavailableIdentityWithoutWrites(t *testing.T) {
	for _, tc := range []struct {
		name, state, org, email string
		status                  int
		human                   bool
	}{
		{name: "foreign_organization", state: "USER_STATE_ACTIVE", org: "foreign", email: "owner@example.test", human: true},
		{name: "changed_email", state: "USER_STATE_ACTIVE", org: "helivanta-11111111-1111-4111-8111-111111111111", email: "other@example.test", human: true},
		{name: "disabled_owner", state: "USER_STATE_INACTIVE", org: "helivanta-11111111-1111-4111-8111-111111111111", email: "owner@example.test", human: true},
		{name: "machine_owner", state: "USER_STATE_ACTIVE", org: "helivanta-11111111-1111-4111-8111-111111111111", email: "owner@example.test"},
		{name: "identity_unavailable", status: 503},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tenant := "11111111-1111-4111-8111-111111111111"
			org, user := "helivanta-"+tenant, "owner-"+tenant
			spec := identity.ZitadelOrganizationSpec{Product: "helivanta", TenantID: tenant, DisplayName: "Hospital", InitialAdminUserID: user, InitialAdmin: &identity.OrganizationInitialAdmin{GivenName: "Test", FamilyName: "Owner", Email: "owner@example.test"}}
			writes := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					writes++
					w.WriteHeader(500)
					return
				}
				switch r.URL.Path {
				case "/admin/v1/orgs/" + org:
					_ = json.NewEncoder(w).Encode(map[string]any{"org": map[string]string{"id": org, "name": "Hospital", "state": "ORG_STATE_ACTIVE"}})
				case "/v2/users/" + user:
					if tc.status != 0 {
						w.WriteHeader(tc.status)
						return
					}
					result := map[string]any{"userId": user, "state": tc.state, "details": map[string]string{"resourceOwner": tc.org}}
					if tc.human {
						result["human"] = map[string]any{"email": map[string]any{"email": tc.email, "isVerified": true}}
					}
					_ = json.NewEncoder(w).Encode(map[string]any{"user": result})
				default:
					t.Errorf("unexpected owner request %s", r.URL.Path)
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			c, _ := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "token", nil })
			_, err := c.EnsureOrganization(t.Context(), spec, "TESSERIX", "project")
			if err == nil || writes != 0 || errors.Is(err, zitadelapi.ErrOwnerVerificationPending) {
				t.Fatalf("unsafe owner accepted: err=%v writes=%d", err, writes)
			}
		})
	}
}

func TestExternalHelivantaEmailSuppressesSharedSMTPOnCreationAndReplay(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost=%v", lost), func(t *testing.T) { testFirstOwnerCreation(t, lost, false, true) })
	}
}

func TestFreshOwnerOrgNameLeavesRoomForUniqueNativeDomainSuffix(t *testing.T) {
	testFirstOwnerCreation(t, false, false, false, "Hospital "+strings.Repeat("x", 191))
}
