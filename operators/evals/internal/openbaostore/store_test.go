package openbaostore

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fallbackStore struct{ calls int }

func (f *fallbackStore) Latest(context.Context, string) (string, bool, error) {
	f.calls++
	return "other-product", true, nil
}
func (f *fallbackStore) Ensure(context.Context, string, string) error { f.calls++; return nil }

func TestMappedSecretsNeverFallBackAndWritesUseCAS(t *testing.T) {
	for _, code := range []int{200, 403, 404, 500} {
		t.Run(fmt.Sprint(code), func(t *testing.T) {
			fallback := &fallbackStore{}
			writes, revoked := 0, 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/v1/auth/kubernetes/login":
					json.NewEncoder(w).Encode(map[string]any{"auth": map[string]string{"client_token": "test-token"}})
				case "/v1/auth/token/revoke-self":
					revoked++
					w.WriteHeader(204)
				case "/v1/kv/data/kora/app/kora-langfuse-public-key":
					if r.Header.Get("X-Vault-Token") != "test-token" {
						t.Error("missing token")
					}
					if r.Method == "GET" {
						w.WriteHeader(code)
						if code == 200 {
							fmt.Fprint(w, `{"data":{"data":{"value":"old"},"metadata":{"version":3}}}`)
						} else {
							fmt.Fprint(w, "sensitive-upstream-error")
						}
						return
					}
					writes++
					var body struct {
						Options struct {
							CAS int `json:"cas"`
						}
						Data map[string]string
					}
					json.NewDecoder(r.Body).Decode(&body)
					want := 3
					if code == 404 {
						want = 0
					}
					if body.Options.CAS != want || body.Data["value"] != "new" {
						t.Error("wrong CAS or value")
					}
					w.WriteHeader(200)
				default:
					t.Error("unexpected endpoint")
					w.WriteHeader(500)
				}
			}))
			defer server.Close()
			store, err := New(server.URL, "evals-kora", func() (string, error) { return "test-jwt", nil }, map[string]string{"prod-kora-langfuse-public-key": "kora/app/kora-langfuse-public-key"}, fallback, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			err = store.Ensure(context.Background(), "prod-kora-langfuse-public-key", "new")
			if code == 200 || code == 404 {
				if err != nil || writes != 1 {
					t.Fatal("expected a CAS write", err, writes)
				}
			} else if err == nil || writes != 0 || strings.Contains(err.Error(), "sensitive") {
				t.Fatal("expected sanitized failure without write")
			}
			if fallback.calls != 0 || revoked != 1 {
				t.Fatal("mapped operation fell back or leaked token")
			}
			value, ok, err := store.Latest(context.Background(), "prod-devai-langfuse-public-key")
			if err != nil || !ok || value != "other-product" || fallback.calls != 1 {
				t.Fatal("other product routing failed")
			}
		})
	}
}

func TestRejectsUnsafePaths(t *testing.T) {
	for _, path := range []string{"../platform/key", "kora/app/../key", "kora/app/key?x=1", "/kora/app/key"} {
		if _, err := New("https://bao.example", "role", func() (string, error) { return "jwt", nil }, map[string]string{"source": path}, &fallbackStore{}, http.DefaultClient); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
}
