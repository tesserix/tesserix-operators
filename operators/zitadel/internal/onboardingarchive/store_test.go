package onboardingarchive_test

import (
	"context"
	"github.com/tesserix/devai-sandbox-operator/operators/zitadel/internal/onboardingarchive"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestArchiveNeverOverwritesAndVerifiesExistingHeldRevision(t *testing.T) {
	var created bool
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "POST":
			if r.URL.Query().Get("ifGenerationMatch") != "0" || r.URL.Query().Get("uploadType") != "multipart" {
				t.Error("unconditional upload")
			}
			raw, _ := io.ReadAll(r.Body)
			if !strings.Contains(string(raw), `"eventBasedHold":true`) {
				t.Error("revision is deletable")
			}
			if created {
				w.WriteHeader(412)
				return
			}
			created = true
			_, _ = io.WriteString(w, `{"generation":"12","eventBasedHold":true}`)
		case r.Method == "GET" && r.URL.Query().Get("alt") == "media":
			_, _ = io.WriteString(w, "spec: safe\n")
		case r.Method == "GET":
			_, _ = io.WriteString(w, `{"generation":"12","eventBasedHold":true}`)
		default:
			t.Errorf("unexpected destructive method %s", r.Method)
			w.WriteHeader(400)
		}
	}))
	defer s.Close()
	store, e := onboardingarchive.New("bucket", s.URL, s.Client())
	if e != nil {
		t.Fatal(e)
	}
	for range 2 {
		gen, e := store.Put(context.Background(), "helivanta/prod/tenants/tenant/revisions/hash/intent.yaml", []byte("spec: safe\n"))
		if e != nil || gen != "12" {
			t.Fatalf("generation=%s error=%v", gen, e)
		}
	}
	_, e = store.Put(context.Background(), "helivanta/prod/tenants/tenant/revisions/hash/intent.yaml", []byte("different"))
	if e == nil {
		t.Fatal("accepted conflicting immutable revision")
	}
}
