package openpanelapi_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tesserix/devai-sandbox-operator/operators/openpanel/internal/openpanelapi"
)

func TestEnsureProjectCreatesProjectAndReturnsWriteClient(t *testing.T) {
	t.Parallel()

	var created openpanelapi.ProjectInput
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("openpanel-client-id") != "root-id" || r.Header.Get("openpanel-client-secret") != "root-secret" {
			t.Fatal("management credentials were not sent")
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/manage/projects":
			_, _ = w.Write([]byte(`{"data":[]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/manage/projects":
			if err := json.NewDecoder(r.Body).Decode(&created); err != nil {
				t.Fatal(err)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"data":{"id":"project-langfuse","name":"Langfuse","domain":"https://langfuse.tesserix.app","cors":["https://langfuse.tesserix.app"],"types":["website"],"client":{"id":"client-123"}}}`))
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	client, err := openpanelapi.NewClient(server.URL, server.Client(), func() (string, string, error) {
		return "root-id", "root-secret", nil
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := client.EnsureProject(context.Background(), "", openpanelapi.ProjectInput{
		Name: "Langfuse", Domain: "https://langfuse.tesserix.app", CORS: []string{"https://langfuse.tesserix.app"}, Types: []string{"website"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.ProjectID != "project-langfuse" || result.ClientID != "client-123" {
		t.Fatalf("result = %#v", result)
	}
	if created.Name != "Langfuse" || created.Domain != "https://langfuse.tesserix.app" || len(created.CORS) != 1 {
		t.Fatalf("created input = %#v", created)
	}
}

func TestEnsureProjectUpdatesAnExistingProjectAndAdoptsItsWriteClient(t *testing.T) {
	t.Parallel()

	patchCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/manage/projects/project-devai":
			_, _ = w.Write([]byte(`{"data":{"id":"project-devai","name":"Old DevAI","domain":"https://old.example.com","cors":[],"types":["website"]}}`))
		case r.Method == http.MethodPatch && r.URL.Path == "/manage/projects/project-devai":
			patchCalls++
			_, _ = w.Write([]byte(`{"data":{"id":"project-devai","name":"DevAI","domain":"https://devai.tesserix.app","cors":["https://devai.tesserix.app"],"types":["website"]}}`))
		case r.Method == http.MethodGet && r.URL.Path == "/manage/clients" && r.URL.Query().Get("projectId") == "project-devai":
			_, _ = w.Write([]byte(`{"data":[{"id":"client-devai","name":"devai-write","type":"write","projectId":"project-devai"}]}`))
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(server.Close)

	client, err := openpanelapi.NewClient(server.URL, server.Client(), func() (string, string, error) {
		return "root-id", "root-secret", nil
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.EnsureProject(context.Background(), "project-devai", openpanelapi.ProjectInput{
		Name: "DevAI", Domain: "https://devai.tesserix.app", CORS: []string{"https://devai.tesserix.app"}, Types: []string{"website"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if patchCalls != 1 || result.ClientID != "client-devai" {
		t.Fatalf("patch calls = %d, result = %#v", patchCalls, result)
	}
}
