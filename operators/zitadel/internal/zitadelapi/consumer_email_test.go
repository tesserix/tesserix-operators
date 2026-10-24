package zitadelapi_test

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	identity "github.com/tesserix/devai-sandbox-operator/operators/zitadel/api/v1alpha1"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapi"
)

func TestConsumerEmailOTPReconcilesOptInAndRevocation(t *testing.T) {
	metadata := ""
	writes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/users/admin" && r.URL.Path != "/admin/v1/orgs/consumer-mediconnect" && r.Header.Get("x-zitadel-orgid") != "consumer-mediconnect" {
			t.Errorf("unscoped request: %s", r.URL.Path)
		}
		switch r.URL.Path {
		case "/v2/users/admin":
			fmt.Fprint(w, `{"user":{"userId":"admin","state":"USER_STATE_ACTIVE","human":{"email":{"isVerified":true}}}}`)
		case "/admin/v1/orgs/consumer-mediconnect":
			fmt.Fprint(w, `{"org":{"id":"consumer-mediconnect","name":"MediConnect","state":"ORG_STATE_ACTIVE"}}`)
		case "/management/v1/metadata/consumer_binding":
			fmt.Fprintf(w, `{"metadata":{"key":"consumer_binding","value":%q}}`, base64.StdEncoding.EncodeToString([]byte("identity-operator/mediconnect")))
		case "/management/v1/metadata/consumer_email_otp":
			if r.Method == http.MethodGet {
				if metadata == "" {
					w.WriteHeader(http.StatusNotFound)
					return
				}
				fmt.Fprintf(w, `{"metadata":{"key":"consumer_email_otp","value":%q}}`, metadata)
			} else if r.Method == http.MethodPost {
				var body struct {
					Value string `json:"value"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				metadata = body.Value
				writes++
			} else {
				t.Errorf("unexpected metadata method %s", r.Method)
			}
		case "/management/v1/policies/login":
			if r.Method == http.MethodGet {
				fmt.Fprint(w, `{"policy":{"allowExternalIdp":true,"hidePasswordReset":true,"externalLoginCheckLifetime":"43200s"}}`)
				return
			}
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["allowUsernamePassword"] != false || body["allowRegister"] != false {
				t.Errorf("unsafe login policy %v", body)
			}
		case "/management/v1/policies/login/idps/_search":
			fmt.Fprint(w, `{"result":[]}`)
		case "/management/v1/policies/label":
			if r.Method == http.MethodGet {
				fmt.Fprint(w, `{"policy":{}}`)
			}
		case "/management/v1/policies/label/_activate":
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	c, err := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "test", nil })
	if err != nil {
		t.Fatal(err)
	}
	var spec identity.ConsumerOrganization
	if err := json.Unmarshal([]byte(`{"initialAdminUserId":"admin","emailOtp":true,"providers":[],"primaryColor":"#3858F5","backgroundColor":"#F7F8FA","fontColor":"#151923"}`), &spec); err != nil {
		t.Fatal(err)
	}
	var previous *identity.ConsumerIdentity
	for range 2 {
		previous, err = c.EnsureConsumerOrganization(t.Context(), "identity-operator/mediconnect", "MediConnect", spec, previous)
		if err != nil {
			t.Fatal(err)
		}
	}
	decoded, err := base64.StdEncoding.DecodeString(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != `{"version":1,"enabled":true,"smsEnabled":false}` || writes != 1 {
		t.Fatalf("incorrect or non-idempotent policy: %s, writes %d", decoded, writes)
	}
	// Keep a valid email-only claim when revoking its email login. A disabled org must fail closed.
	if err := json.Unmarshal([]byte(`{"emailOtp":false}`), &spec); err != nil {
		t.Fatal(err)
	}
	_, err = c.EnsureConsumerOrganization(t.Context(), "identity-operator/mediconnect", "MediConnect", spec, previous)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = base64.StdEncoding.DecodeString(metadata)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != `{"version":1,"enabled":false,"smsEnabled":false}` || writes != 2 {
		t.Fatalf("policy not revoked: %s, writes %d", decoded, writes)
	}
}
