package zitadelapi

import (
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
