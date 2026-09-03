package langfuseapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tesserix/devai-sandbox-operator/operators/evals/internal/langfuseapi"
)

func creds() (string, string, error) { return "pk-lf-org", "sk-lf-org", nil }

func TestEnsureProjectAdoptsByNameBeforeCreating(t *testing.T) {
	t.Parallel()
	created := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, _ := r.BasicAuth()
		if user != "pk-lf-org" || pass != "sk-lf-org" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.Method + " " + r.URL.Path {
		case "GET /api/public/projects":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "p1", "name": "Kora"}}})
		case "POST /api/public/projects":
			created++
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "p2", "name": "New"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := langfuseapi.NewClient(server.URL, server.Client(), creds)
	if err != nil {
		t.Fatal(err)
	}
	project, err := client.EnsureProject(context.Background(), "", "Kora", nil)
	if err != nil || project.ID != "p1" || created != 0 {
		t.Fatalf("project = %#v, created = %d, err = %v", project, created, err)
	}
	project, err = client.EnsureProject(context.Background(), "", "New", map[string]string{"claim": "new"})
	if err != nil || project.ID != "p2" || created != 1 {
		t.Fatalf("project = %#v, created = %d, err = %v", project, created, err)
	}
}

func TestCreateAPIKeyRequiresBothKeysAndMarksClientErrorsPermanent(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/public/projects/p1/apiKeys":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "k1", "publicKey": "pk-lf-1", "secretKey": "sk-lf-1"})
		case "/api/public/projects/p2/apiKeys":
			_ = json.NewEncoder(w).Encode(map[string]string{"id": "k2", "publicKey": "pk-lf-2"})
		default:
			w.WriteHeader(http.StatusForbidden)
		}
	}))
	defer server.Close()
	client, err := langfuseapi.NewClient(server.URL, server.Client(), creds)
	if err != nil {
		t.Fatal(err)
	}
	key, err := client.CreateAPIKey(context.Background(), "p1", "evals")
	if err != nil || key.PublicKey != "pk-lf-1" || key.SecretKey != "sk-lf-1" {
		t.Fatalf("key = %#v, err = %v", key, err)
	}
	if _, err := client.CreateAPIKey(context.Background(), "p2", "evals"); err == nil {
		t.Fatal("expected an error for a response without a secret key")
	}
	_, err = client.ListAPIKeys(context.Background(), "p3")
	var status *langfuseapi.StatusError
	if !errorsAs(err, &status) || !status.Permanent() {
		t.Fatalf("expected a permanent status error, got %v", err)
	}
}

func errorsAs(err error, target **langfuseapi.StatusError) bool {
	for err != nil {
		if s, ok := err.(*langfuseapi.StatusError); ok {
			*target = s
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
