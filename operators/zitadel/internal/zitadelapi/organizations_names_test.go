package zitadelapi_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"unicode/utf8"

	identity "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapi"
)

func TestFreshHelivantaOrganizationHasReadableNameAndRecoversReplay(t *testing.T) {
	for _, lost := range []bool{false, true} {
		t.Run(fmt.Sprintf("lost_response=%t", lost), func(t *testing.T) {
			spec := readableOrganizationSpec("11111111-1111-4111-8111-111111111111", "Yes Hospital")
			var nativeName string
			creates := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/admin/v1/orgs/helivanta-" + spec.TenantID:
					if nativeName == "" {
						w.WriteHeader(http.StatusNotFound)
						return
					}
					writeOrganization(w, "helivanta-"+spec.TenantID, nativeName)
				case "/v2/organizations":
					creates++
					var body struct{ Name string }
					if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
						t.Error(err)
					}
					nativeName = body.Name
					if lost {
						w.WriteHeader(http.StatusInternalServerError)
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"organizationId": "helivanta-" + spec.TenantID})
				case "/v2/users/" + spec.InitialAdminUserID:
					writeUnverifiedOwner(w, spec)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			t.Cleanup(server.Close)
			client, err := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "token", nil })
			if err != nil {
				t.Fatal(err)
			}
			_, first := client.EnsureOrganization(t.Context(), spec, "TESSERIX", "project")
			if lost && first == nil {
				t.Fatal("lost response reported success")
			}
			id, err := client.EnsureOrganization(t.Context(), spec, "TESSERIX", "project")
			if !errors.Is(err, zitadelapi.ErrOwnerVerificationPending) || id != "helivanta-"+spec.TenantID {
				t.Fatalf("replay lost its tenant-owned first owner: id=%s err=%v", id, err)
			}
			if nativeName != "Helivanta-Yes-Hospital" || creates != 1 {
				t.Fatalf("name=%q creates=%d; want one readable organization", nativeName, creates)
			}
		})
	}
}

func readableOrganizationSpec(tenant, name string) identity.ZitadelOrganizationSpec {
	return identity.ZitadelOrganizationSpec{
		Product: "helivanta", TenantID: tenant, DisplayName: name,
		InitialAdminUserID: "owner-" + tenant,
		InitialAdmin:       &identity.OrganizationInitialAdmin{GivenName: "Test", FamilyName: "Owner", Email: "owner@example.test"},
	}
}

func writeOrganization(w http.ResponseWriter, id, name string) {
	_ = json.NewEncoder(w).Encode(map[string]any{"org": map[string]string{"id": id, "name": name, "state": "ORG_STATE_ACTIVE"}})
}

func writeUnverifiedOwner(w http.ResponseWriter, spec identity.ZitadelOrganizationSpec) {
	_ = json.NewEncoder(w).Encode(map[string]any{"user": map[string]any{
		"userId": spec.InitialAdminUserID, "state": "USER_STATE_ACTIVE",
		"details": map[string]string{"resourceOwner": "helivanta-" + spec.TenantID},
		"human":   map[string]any{"email": map[string]any{"email": spec.InitialAdmin.Email, "isVerified": false}},
	}})
}

// The provider owns its atomic organization-name and domain uniqueness constraints.
func TestConcurrentSameNameSignupsKeepSeparateOrganizationsAndOwners(t *testing.T) {
	specs := []identity.ZitadelOrganizationSpec{
		readableOrganizationSpec("11111111-1111-4111-8111-111111111111", "Yes Hospital"),
		readableOrganizationSpec("22222222-2222-4222-8222-222222222222", "Yes Hospital"),
	}
	var mu sync.Mutex
	byID, byDomain := map[string]string{}, map[string]string{}
	creates := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasPrefix(r.URL.Path, "/admin/v1/orgs/"):
			id := strings.TrimPrefix(r.URL.Path, "/admin/v1/orgs/")
			if name, found := byID[id]; found {
				writeOrganization(w, id, name)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		case r.URL.Path == "/v2/organizations":
			var body struct{ OrganizationID, Name string }
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			domain := strings.ToLower(body.Name) + ".auth.tesserix.app"
			if _, exists := byDomain[domain]; exists {
				w.WriteHeader(http.StatusConflict)
				return
			}
			if _, exists := byID[body.OrganizationID]; exists {
				w.WriteHeader(http.StatusConflict)
				return
			}
			byID[body.OrganizationID], byDomain[domain] = body.Name, body.OrganizationID
			creates++
			_ = json.NewEncoder(w).Encode(map[string]string{"organizationId": body.OrganizationID})
		case r.URL.Path == "/v2/organizations/_search":
			var body struct {
				Query   struct{ Limit uint64 }
				Queries []struct {
					DomainQuery struct{ Domain, Method string }
				}
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Error(err)
				return
			}
			if body.Query.Limit != 2 || len(body.Queries) != 1 || body.Queries[0].DomainQuery.Method != "TEXT_QUERY_METHOD_EQUALS" {
				t.Error("collision lookup must be exact and bounded")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			id := byDomain[body.Queries[0].DomainQuery.Domain]
			_ = json.NewEncoder(w).Encode(map[string]any{"result": []map[string]string{{"id": id, "name": byID[id]}}})
		case strings.HasPrefix(r.URL.Path, "/v2/users/"):
			for _, spec := range specs {
				if r.URL.Path == "/v2/users/"+spec.InitialAdminUserID {
					writeUnverifiedOwner(w, spec)
					return
				}
			}
			w.WriteHeader(http.StatusNotFound)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	client, err := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "token", nil })
	if err != nil {
		t.Fatal(err)
	}
	var group sync.WaitGroup
	for _, spec := range specs {
		group.Go(func() {
			for range 2 {
				id, err := client.EnsureOrganization(t.Context(), spec, "TESSERIX", "project")
				if !errors.Is(err, zitadelapi.ErrOwnerVerificationPending) || id != "helivanta-"+spec.TenantID {
					t.Errorf("tenant %s lost independent owner: id=%s err=%v", spec.TenantID, id, err)
				}
			}
		})
	}
	group.Wait()
	mu.Lock()
	defer mu.Unlock()
	if creates != 2 || len(byDomain) != 2 {
		t.Fatalf("creates=%d domains=%d; want two independently bound organizations", creates, len(byDomain))
	}
	for domain := range byDomain {
		if domain != "helivanta-yes-hospital.auth.tesserix.app" && domain != "helivanta-yes-hospital-2.auth.tesserix.app" {
			t.Errorf("unreadable native domain %q", domain)
		}
	}
}

func TestLongUnicodeNameKeepsReadablePrefixAndDomainSuffixRoom(t *testing.T) {
	spec := readableOrganizationSpec("11111111-1111-4111-8111-111111111111", strings.Repeat("हॉस्पिटल ", 6))
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		var body struct{ Name string }
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if !strings.HasPrefix(body.Name, "Helivanta-हॉस्पिटल") || strings.Contains(body.Name, spec.TenantID) || !utf8.ValidString(body.Name) || len(body.Name) > 55 {
			t.Errorf("name %q does not leave room for a collision suffix in the native 63-byte domain", body.Name)
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(server.Close)
	client, err := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "token", nil })
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.EnsureOrganization(t.Context(), spec, "TESSERIX", "project"); err == nil {
		t.Fatal("provider outage reported success")
	}
}

func TestReadableNameDoesNotRetryUnrelatedConflictsOrUnavailableCollisionLookups(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusServiceUnavailable} {
		t.Run(fmt.Sprintf("collision_lookup_status=%d", status), func(t *testing.T) {
			creates, lookups := 0, 0
			spec := readableOrganizationSpec("11111111-1111-4111-8111-111111111111", "Yes Hospital")
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/admin/v1/orgs/helivanta-" + spec.TenantID:
					w.WriteHeader(http.StatusNotFound)
				case "/v2/organizations":
					creates++
					w.WriteHeader(http.StatusConflict)
				case "/v2/organizations/_search":
					lookups++
					w.WriteHeader(status)
					_, _ = w.Write([]byte(`{"result":[]}`))
				default:
					t.Errorf("unexpected write or identity adoption: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			t.Cleanup(server.Close)
			client, err := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "token", nil })
			if err != nil {
				t.Fatal(err)
			}
			if id, err := client.EnsureOrganization(t.Context(), spec, "TESSERIX", "project"); err == nil || id != "" || creates != 1 || lookups != 1 {
				t.Fatalf("unconfirmed name collision retried or activated: id=%s creates=%d lookups=%d err=%v", id, creates, lookups, err)
			}
		})
	}
}

func TestExistingTenantNamesArePreservedWithoutRecreationOrRename(t *testing.T) {
	spec := readableOrganizationSpec("11111111-1111-4111-8111-111111111111", "Yes Hospital")
	for _, name := range []string{spec.DisplayName, "[" + spec.TenantID + "] " + spec.DisplayName, "Helivanta-Yes-Hospital-2"} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/admin/v1/orgs/helivanta-" + spec.TenantID:
					writeOrganization(w, "helivanta-"+spec.TenantID, name)
				case "/v2/users/" + spec.InitialAdminUserID:
					writeUnverifiedOwner(w, spec)
				default:
					t.Errorf("existing organization was mutated: %s %s", r.Method, r.URL.Path)
					w.WriteHeader(http.StatusInternalServerError)
				}
			}))
			t.Cleanup(server.Close)
			client, err := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "token", nil })
			if err != nil {
				t.Fatal(err)
			}
			id, err := client.EnsureOrganization(t.Context(), spec, "TESSERIX", "project")
			if id != "helivanta-"+spec.TenantID || !errors.Is(err, zitadelapi.ErrOwnerVerificationPending) {
				t.Fatalf("existing binding was not preserved: id=%s err=%v", id, err)
			}
		})
	}
}

func TestNameCollisionRetriesAreBoundedWithoutGrantingAccess(t *testing.T) {
	spec := readableOrganizationSpec("11111111-1111-4111-8111-111111111111", "Yes Hospital")
	creates, searches := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/admin/v1/orgs/helivanta-" + spec.TenantID:
			w.WriteHeader(http.StatusNotFound)
		case "/v2/organizations":
			creates++
			w.WriteHeader(http.StatusConflict)
		case "/v2/organizations/_search":
			searches++
			_, _ = w.Write([]byte(`{"result":[{"id":"another-organization"}]}`))
		default:
			t.Errorf("exhaustion must not inspect another owner or grant access: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	t.Cleanup(server.Close)
	client, err := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "token", nil })
	if err != nil {
		t.Fatal(err)
	}
	id, err := client.EnsureOrganization(t.Context(), spec, "TESSERIX", "project")
	if err == nil || id != "" || creates != 25 || searches != 25 {
		t.Fatalf("unbounded or successful collision exhaustion: id=%s creates=%d searches=%d err=%v", id, creates, searches, err)
	}
}
