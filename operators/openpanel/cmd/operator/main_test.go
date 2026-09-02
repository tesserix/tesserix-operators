package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFileCredentialsReadsTrimmedValues(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	idPath := filepath.Join(dir, "client-id")
	secretPath := filepath.Join(dir, "client-secret")
	if err := os.WriteFile(idPath, []byte("root-id\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(secretPath, []byte("root-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	id, secret, err := fileCredentials(idPath, secretPath)()
	if err != nil {
		t.Fatal(err)
	}
	if id != "root-id" || secret != "root-secret" {
		t.Fatalf("credentials were not trimmed")
	}
}
