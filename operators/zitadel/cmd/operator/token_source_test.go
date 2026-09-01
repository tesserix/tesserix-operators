package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestMachineKeyTokenSource_exchanges_a_machine_key_without_exposing_it(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/.well-known/openid-configuration":
			_, _ = w.Write([]byte(`{"issuer":"` + serverURL(r) + `","token_endpoint":"` + serverURL(r) + `/oauth/v2/token"}`))
		case "/oauth/v2/token":
			body, err := io.ReadAll(r.Body)
			if err != nil {
				t.Fatal(err)
			}
			values, err := url.ParseQuery(string(body))
			if err != nil {
				t.Fatal(err)
			}
			if values.Get("grant_type") != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
				t.Fatalf("grant type = %q", values.Get("grant_type"))
			}
			_, _ = w.Write([]byte(`{"access_token":"test-access-token","token_type":"Bearer","expires_in":300}`))
		default:
			t.Fatalf("path = %s", r.URL.Path)
		}
	}))
	t.Cleanup(server.Close)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	keyFile, err := json.Marshal(map[string]string{"type": "serviceaccount", "keyId": "key-123", "key": string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})), "userId": "user-123"})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "machine-key.json")
	if err := os.WriteFile(path, keyFile, 0o600); err != nil {
		t.Fatal(err)
	}

	source, err := machineKeyTokenSource(server.URL, path)
	if err != nil {
		t.Fatal(err)
	}
	token, err := source()
	if err != nil {
		t.Fatal(err)
	}
	if token != "test-access-token" {
		t.Fatalf("token = %q", token)
	}
}

func serverURL(r *http.Request) string { return "http://" + r.Host }
