package zitadelapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/zitadelapi"
)

func machineUserServer(t *testing.T, handle func(w http.ResponseWriter, r *http.Request)) *zitadelapi.Client {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v2/organizations/_search" {
			_, _ = w.Write([]byte(`{"result":[{"id":"org-123","name":"TESSERIX"}]}`))
			return
		}
		if got := r.Header.Get("X-Zitadel-Orgid"); got != "org-123" {
			t.Errorf("%s %s org header = %q", r.Method, r.URL.Path, got)
		}
		handle(w, r)
	}))
	t.Cleanup(server.Close)
	c, err := zitadelapi.NewClient(server.URL, "auth.tesserix.app", func() (string, error) { return "test-token", nil })
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestClient_creates_machine_user_with_jwt_access_tokens(t *testing.T) {
	c := machineUserServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/management/v1/users/machine" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		want := map[string]string{"userName": "kora-eval", "name": "Kora eval", "accessTokenType": "ACCESS_TOKEN_TYPE_JWT"}
		if !reflect.DeepEqual(body, want) {
			t.Errorf("body = %v", body)
		}
		_, _ = w.Write([]byte(`{"userId":"user-1"}`))
	})

	user, err := c.CreateMachineUser(t.Context(), "TESSERIX", "kora-eval", "Kora eval")
	if err != nil || user.ID != "user-1" {
		t.Fatalf("user=%+v err=%v", user, err)
	}
}

func TestClient_refuses_to_adopt_a_human_user_with_the_declared_name(t *testing.T) {
	c := machineUserServer(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":[{"id":"person-1","userName":"kora-eval","human":{"profile":{}}}]}`))
	})

	if _, _, err := c.FindMachineUserByName(t.Context(), "TESSERIX", "kora-eval"); err == nil {
		t.Fatal("adopted a human user")
	}
}

func TestClient_finds_machine_user_by_name(t *testing.T) {
	c := machineUserServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/management/v1/users/_search" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"result":[{"id":"user-1","userName":"kora-eval","machine":{"name":"Kora eval","accessTokenType":"ACCESS_TOKEN_TYPE_BEARER"}}]}`))
	})

	user, found, err := c.FindMachineUserByName(t.Context(), "TESSERIX", "kora-eval")
	if err != nil || !found {
		t.Fatalf("found=%v err=%v", found, err)
	}
	if user.AccessTokenType != "ACCESS_TOKEN_TYPE_BEARER" || user.Name != "Kora eval" {
		t.Fatalf("user = %+v", user)
	}
}

func TestClient_replaces_every_pat_before_issuing_one(t *testing.T) {
	var calls []string
	c := machineUserServer(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/management/v1/users/user-1/pats/_search":
			_, _ = w.Write([]byte(`{"result":[{"id":"old-1"},{"id":"old-2"}]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/management/v1/users/user-1/pats":
			_, _ = w.Write([]byte(`{"tokenId":"new","token":"pat-value"}`))
		default:
			_, _ = w.Write([]byte(`{}`))
		}
	})

	token, err := c.ReplacePersonalAccessTokens(t.Context(), "TESSERIX", "user-1")
	if err != nil || token != "pat-value" {
		t.Fatalf("token=%q err=%v", token, err)
	}
	want := []string{
		"POST /management/v1/users/user-1/pats/_search",
		"DELETE /management/v1/users/user-1/pats/old-1",
		"DELETE /management/v1/users/user-1/pats/old-2",
		"POST /management/v1/users/user-1/pats",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %v", calls)
	}
}

func TestClient_replaces_client_secret(t *testing.T) {
	c := machineUserServer(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut || r.URL.Path != "/management/v1/users/user-1/secret" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"clientId":"kora-eval","clientSecret":"s3cret"}`))
	})

	id, secret, err := c.ReplaceClientSecret(t.Context(), "TESSERIX", "user-1")
	if err != nil || id != "kora-eval" || secret != "s3cret" {
		t.Fatalf("id=%q secret=%q err=%v", id, secret, err)
	}
}
