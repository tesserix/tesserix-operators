package evalstore

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

type fakeConn struct {
	calls [][]any
}

type tag struct{}

func (tag) RowsAffected() int64 { return 1 }

func (f *fakeConn) Exec(_ context.Context, sql string, args ...any) (pgconnCommandTag, error) {
	if !strings.Contains(sql, "ON CONFLICT (product, name)") {
		panic("unexpected sql")
	}
	f.calls = append(f.calls, args)
	return tag{}, nil
}

func (f *fakeConn) Close(context.Context) error { return nil }

func TestUpsertInjectsPasswordAndWritesEveryDataset(t *testing.T) {
	t.Parallel()
	store, err := NewStore("postgres://grader@db.global.svc:5432/evals_db?sslmode=prefer", func() (string, error) { return "p@ss", nil })
	if err != nil {
		t.Fatal(err)
	}
	conn := &fakeConn{}
	var dsn string
	store.connect = func(_ context.Context, got string) (execer, error) {
		dsn = got
		return conn, nil
	}
	datasets := []Dataset{{Name: "coaching", Modality: "agent"}, {Name: "foods", Modality: "retrieval", Description: "d"}}
	if err := store.Upsert(context.Background(), "kora", datasets); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := parsed.User.Password()
	if parsed.User.Username() != "grader" || password != "p@ss" || parsed.Host != "db.global.svc:5432" || parsed.Path != "/evals_db" {
		t.Fatalf("dsn = %q", dsn)
	}
	if len(conn.calls) != 2 || conn.calls[1][0] != "kora" || conn.calls[1][1] != "foods" || conn.calls[1][3] != "d" {
		t.Fatalf("calls = %#v", conn.calls)
	}
}

func TestNewStoreRejectsEmbeddedPasswords(t *testing.T) {
	t.Parallel()
	if _, err := NewStore("postgres://grader:leak@db/evals_db", func() (string, error) { return "", nil }); err == nil {
		t.Fatal("expected an error for an embedded password")
	}
	if _, err := NewStore("mysql://grader@db/evals_db", func() (string, error) { return "", nil }); err == nil {
		t.Fatal("expected an error for a non-postgres scheme")
	}
}
