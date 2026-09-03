package secretmanager_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tesserix/devai-sandbox-operator/internal/secretmanager"
)

func TestEnsureCreatesSecretAndAddsInitialVersion(t *testing.T) {
	t.Parallel()

	created := 0
	added := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet:
			http.Error(w, "missing", http.StatusNotFound)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/test-project/secrets":
			created++
			if r.URL.Query().Get("secretId") != "prod-openpanel-langfuse-client-id" {
				t.Fatalf("secret id = %q", r.URL.Query().Get("secretId"))
			}
			_, _ = w.Write([]byte(`{"name":"projects/test-project/secrets/prod-openpanel-langfuse-client-id"}`))
		case r.Method == http.MethodPost && r.URL.Path == "/v1/projects/test-project/secrets/prod-openpanel-langfuse-client-id:addVersion":
			added++
			var body struct {
				Payload struct {
					Data string `json:"data"`
				} `json:"payload"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			decoded, err := base64.StdEncoding.DecodeString(body.Payload.Data)
			if err != nil || string(decoded) != "client-123" {
				t.Fatalf("payload = %q, err = %v", decoded, err)
			}
			_, _ = w.Write([]byte(`{"name":"projects/test-project/secrets/prod-openpanel-langfuse-client-id/versions/1"}`))
		default:
			http.Error(w, "unexpected", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	store, err := secretmanager.NewStore(server.URL, "test-project", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Ensure(context.Background(), "prod-openpanel-langfuse-client-id", "client-123"); err != nil {
		t.Fatal(err)
	}
	if created != 1 || added != 1 {
		t.Fatalf("created = %d, added = %d", created, added)
	}
}

func TestEnsureDoesNotAddVersionWhenLatestValueMatches(t *testing.T) {
	t.Parallel()

	posts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posts++
		}
		_, _ = w.Write([]byte(`{"payload":{"data":"Y2xpZW50LTEyMw=="}}`))
	}))
	t.Cleanup(server.Close)

	store, err := secretmanager.NewStore(server.URL, "test-project", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Ensure(context.Background(), "prod-openpanel-devai-client-id", "client-123"); err != nil {
		t.Fatal(err)
	}
	if posts != 0 {
		t.Fatalf("posts = %d, want 0", posts)
	}
}
