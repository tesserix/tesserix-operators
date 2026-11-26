package zitadelapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestStrictHospitalPolicyWithOmittedProtobufDefaultsDoesNotWrite(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			t.Errorf("strict policy drifted on omitted defaults: %s", r.Method)
		}
		_, _ = fmt.Fprint(w, `{"policy":{"forceMfa":true,"ignoreUnknownUsernames":true,"hidePasswordReset":true}}`)
	}))
	defer server.Close()
	c, _ := NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "token", nil })
	if e := c.ensureHospitalLogin(t.Context(), "org"); e != nil {
		t.Fatal(e)
	}
}

// helivanta#1859: a Helivanta organisation hides Zitadel's hosted password
// reset, so its Tesserix-branded reset mail cannot be requested; the other
// strict settings and the existing policy fields are kept.
func TestHospitalLoginHidesHostedPasswordReset(t *testing.T) {
	var written map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = fmt.Fprint(w, `{"policy":{"forceMfa":true,"ignoreUnknownUsernames":true,"hidePasswordReset":false,"allowExternalIdp":true}}`)
			return
		}
		if r.Method != http.MethodPut || written != nil {
			t.Errorf("want one PUT, got %s", r.Method)
		}
		if e := json.NewDecoder(r.Body).Decode(&written); e != nil {
			t.Fatal(e)
		}
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	c, _ := NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "token", nil })
	if e := c.ensureHospitalLogin(t.Context(), "org"); e != nil {
		t.Fatal(e)
	}
	if written["hidePasswordReset"] != true || written["forceMfa"] != true || written["allowExternalIdp"] != true {
		t.Fatalf("hosted reset left visible or policy lost: %v", written)
	}
}

func TestHospitalDomainPolicyRequiresVerificationAndPreservesNaming(t *testing.T) {
	configured := false
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(map[string]any{"policy": map[string]any{"isDefault": !configured, "validateOrgDomains": configured, "userLoginMustBeDomain": true, "smtpSenderAddressMatchesInstanceDomain": true}})
			return
		}
		writes++
		if r.Method != http.MethodPost {
			t.Error("inherited domain policy requires creation")
		}
		var body map[string]bool
		if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
			t.Fatal(e)
		}
		if !body["validateOrgDomains"] || !body["userLoginMustBeDomain"] || !body["smtpSenderAddressMatchesInstanceDomain"] {
			t.Error("domain verification or existing naming/sender policy was lost")
		}
		configured = true
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	c, _ := NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "token", nil })
	for range 2 {
		if e := c.ensureHospitalDomainPolicy(t.Context(), "org"); e != nil {
			t.Fatal(e)
		}
	}
	if writes != 1 {
		t.Fatalf("domain policy wrote %d times", writes)
	}
}

// tesserix-operators#33: Zitadel's password-changed email is turned off per
// Helivanta organisation. Inherited default → one POST; a custom policy that
// still mails → one PUT; already off → no write at all.
func TestHospitalNotificationPolicyDisablesPasswordChangeMailOnce(t *testing.T) {
	for name, tc := range map[string]struct {
		isDefault, passwordChange bool
		wantMethod                string
	}{
		"inherited default":        {true, true, http.MethodPost},
		"custom but still mailing": {false, true, http.MethodPut},
		"already off":              {false, false, ""},
	} {
		t.Run(name, func(t *testing.T) {
			state := tc
			writes := []string{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/management/v1/policies/notification" || r.Header.Get("x-zitadel-orgid") != "org" {
					t.Errorf("unexpected request %s %s org=%q", r.Method, r.URL.Path, r.Header.Get("x-zitadel-orgid"))
				}
				if r.Method == http.MethodGet {
					_ = json.NewEncoder(w).Encode(map[string]any{"policy": map[string]any{"isDefault": state.isDefault, "passwordChange": state.passwordChange}})
					return
				}
				var body map[string]any
				if e := json.NewDecoder(r.Body).Decode(&body); e != nil {
					t.Fatal(e)
				}
				if body["passwordChange"] != false || len(body) != 1 {
					t.Errorf("wrote %v, want only passwordChange:false", body)
				}
				writes = append(writes, r.Method)
				state.isDefault, state.passwordChange = false, false
				_, _ = fmt.Fprint(w, `{}`)
			}))
			defer server.Close()
			c, _ := NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "token", nil })
			for range 2 {
				if e := c.ensureHospitalNotification(t.Context(), "org"); e != nil {
					t.Fatal(e)
				}
			}
			want := []string{}
			if tc.wantMethod != "" {
				want = []string{tc.wantMethod}
			}
			if fmt.Sprint(writes) != fmt.Sprint(want) {
				t.Fatalf("writes %v, want %v", writes, want)
			}
		})
	}
}

func TestHospitalNotificationPolicyFailsClosedOnAnUnreadableAnswer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("wrote after an unreadable read: %s", r.Method)
		}
		_, _ = fmt.Fprint(w, `{}`)
	}))
	defer server.Close()
	c, _ := NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "token", nil })
	if e := c.ensureHospitalNotification(t.Context(), "org"); e == nil {
		t.Fatal("missing policy object was accepted")
	}
}
