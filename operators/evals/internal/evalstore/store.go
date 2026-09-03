package evalstore

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5"
)

type Dataset struct {
	Name        string
	Modality    string
	Description string
}

// PasswordSource is reread on every connection so rotation needs no restart.
type PasswordSource func() (string, error)

type Store struct {
	databaseURL *url.URL
	password    PasswordSource
	connect     func(ctx context.Context, dsn string) (execer, error)
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconnCommandTag, error)
	Close(ctx context.Context) error
}

type pgconnCommandTag interface{ RowsAffected() int64 }

func NewStore(databaseURL string, password PasswordSource) (*Store, error) {
	parsed, err := url.Parse(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse evals database URL: %w", err)
	}
	if parsed.Scheme != "postgres" && parsed.Scheme != "postgresql" {
		return nil, errors.New("evals database URL must use the postgres scheme")
	}
	if parsed.Host == "" || parsed.User == nil || parsed.User.Username() == "" || strings.Trim(parsed.Path, "/") == "" {
		return nil, errors.New("evals database URL must include host, user and database")
	}
	if _, hasPassword := parsed.User.Password(); hasPassword {
		return nil, errors.New("evals database URL must not embed a password")
	}
	if password == nil {
		return nil, errors.New("evals database password source is required")
	}
	return &Store{databaseURL: parsed, password: password, connect: pgxConnect}, nil
}

const upsertSQL = `INSERT INTO eval.datasets (product, name, modality, description)
VALUES ($1, $2, $3, $4)
ON CONFLICT (product, name) DO UPDATE
SET modality = EXCLUDED.modality, description = EXCLUDED.description, updated_at = NOW()
WHERE eval.datasets.modality IS DISTINCT FROM EXCLUDED.modality
   OR eval.datasets.description IS DISTINCT FROM EXCLUDED.description`

// Upsert registers every dataset under the product, one short-lived connection per call.
func (s *Store) Upsert(ctx context.Context, product string, datasets []Dataset) error {
	if len(datasets) == 0 {
		return nil
	}
	password, err := s.password()
	if err != nil {
		return fmt.Errorf("read evals database password: %w", err)
	}
	dsn := *s.databaseURL
	dsn.User = url.UserPassword(s.databaseURL.User.Username(), password)
	conn, err := s.connect(ctx, dsn.String())
	if err != nil {
		return fmt.Errorf("connect to evals database: %w", err)
	}
	defer conn.Close(ctx)
	for _, dataset := range datasets {
		if _, err := conn.Exec(ctx, upsertSQL, product, dataset.Name, dataset.Modality, dataset.Description); err != nil {
			return fmt.Errorf("upsert eval dataset %q: %w", dataset.Name, err)
		}
	}
	return nil
}

func pgxConnect(ctx context.Context, dsn string) (execer, error) {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return nil, err
	}
	return pgxConn{conn}, nil
}

type pgxConn struct{ *pgx.Conn }

func (c pgxConn) Exec(ctx context.Context, sql string, args ...any) (pgconnCommandTag, error) {
	return c.Conn.Exec(ctx, sql, args...)
}
