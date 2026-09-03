package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Connection URIs with a password and vendor-shaped keys must never be committed, not even in tests.
var credentialPatterns = []*regexp.Regexp{
	regexp.MustCompile(`[a-z]+://[^/@\s"']+:[^/@\s"']+@`),
	regexp.MustCompile(`\b[sp]k-lf-[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`),
}

func TestNoCredentialsAreCommitted(t *testing.T) {
	err := filepath.WalkDir(".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" || entry.Name() == "graft" {
				return filepath.SkipDir
			}
			return nil
		}
		switch filepath.Ext(path) {
		case ".go", ".yaml", ".yml", ".md", ".env", ".json", ".toml":
		default:
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for lineNumber, line := range strings.Split(string(content), "\n") {
			for _, pattern := range credentialPatterns {
				if pattern.MatchString(line) {
					t.Errorf("%s:%d looks like a committed credential", path, lineNumber+1)
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
