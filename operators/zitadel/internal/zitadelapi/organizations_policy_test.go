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
		_, _ = fmt.Fprint(w, `{"policy":{"forceMfa":true,"ignoreUnknownUsernames":true}}`)
	}))
	defer server.Close()
	c, _ := NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "token", nil })
	if e := c.ensureHospitalLogin(t.Context(), "org"); e != nil {
		t.Fatal(e)
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
