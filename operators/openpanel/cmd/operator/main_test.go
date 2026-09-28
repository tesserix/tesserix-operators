package main

import (
	"os"
	"path/filepath"
	"reflect"
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

func TestReviewedPathsUseProductPrefixedOpenBaoDestinations(t *testing.T) {
	paths, err := reviewedPaths("devai,langfuse")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"devai": "devai/app/devai-openpanel-client-id", "langfuse": "langfuse/app/langfuse-openpanel-client-id"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v", paths)
	}
	for _, input := range []string{"", "devai,devai", "../other", "devai/other", "UPPER", "devai,", "end-"} {
		if _, err := reviewedPaths(input); err == nil {
			t.Errorf("accepted invalid product list %q", input)
		}
	}
}
