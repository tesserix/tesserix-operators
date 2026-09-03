package main

import (
	"os"
	"strings"
	"testing"
)

func TestOpenPanelProjectInitImageIsBuiltAndPublished(t *testing.T) {
	dockerfile := readFile(t, "Dockerfile")
	for _, want := range []string{
		"FROM postgres:14-alpine@sha256:727876d274666da0b92a445390ba093c84b8e9f8343e1c53cd4e9a7ab2d85310 AS openpanel-project-init",
		"apk add --no-cache argon2 nodejs kubectl",
		"rm -f /usr/local/bin/gosu",
		"USER 65532:65532",
	} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile does not contain %q", want)
		}
	}

	for _, path := range []string{".github/workflows/ci.yml", ".github/workflows/release.yml"} {
		workflow := readFile(t, path)
		if !strings.Contains(workflow, `"name":"openpanel-project-init"`) {
			t.Fatalf("%s does not build openpanel-project-init", path)
		}
		if !strings.Contains(workflow, `"target":"openpanel-project-init"`) {
			t.Fatalf("%s does not select the openpanel-project-init target", path)
		}
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(contents)
}

func TestEvalsOnboardingOperatorImageIsBuiltAndPublished(t *testing.T) {
	dockerfile := string(readFile(t, "Dockerfile"))
	for _, want := range []string{"-o out/evals-onboarding-operator ./operators/evals/cmd/operator", "AS evals-onboarding-operator"} {
		if !strings.Contains(dockerfile, want) {
			t.Fatalf("Dockerfile is missing %q", want)
		}
	}
	for _, path := range []string{".github/workflows/ci.yml", ".github/workflows/release.yml"} {
		workflow := string(readFile(t, path))
		if !strings.Contains(workflow, `"name":"evals-onboarding-operator"`) || !strings.Contains(workflow, `"source_root":"operators/evals"`) {
			t.Fatalf("%s does not build evals-onboarding-operator", path)
		}
	}
}
