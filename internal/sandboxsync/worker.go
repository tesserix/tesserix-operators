package sandboxsync

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5"

	devai "github.com/tesserix/devai-sandbox-operator/api/v1alpha1"
)

type RunConfig struct{ PolicyPath, SourceURL, TargetURL, Salt string }

func Run(ctx context.Context, config RunConfig) error {
	if config.PolicyPath == "" || config.SourceURL == "" || config.TargetURL == "" || config.Salt == "" {
		return fmt.Errorf("policy path, source URL, target URL, and anonymization salt are required")
	}
	data, err := os.ReadFile(config.PolicyPath)
	if err != nil {
		return fmt.Errorf("read policy: %w", err)
	}
	var tables []devai.TableRule
	if err := json.Unmarshal(data, &tables); err != nil {
		return fmt.Errorf("decode policy: %w", err)
	}
	if err := validate(devai.SandboxDataSyncSpec{Schedule: "@daily", Source: devai.DatabaseReference{SecretRef: devai.SecretKeyReference{Name: "source"}}, Target: devai.DatabaseReference{SecretRef: devai.SecretKeyReference{Name: "target"}}, AnonymizationSaltSecretRef: devai.SecretKeyReference{Name: "salt"}, Tables: tables}); err != nil {
		return fmt.Errorf("validate policy: %w", err)
	}
	source, err := pgx.Connect(ctx, config.SourceURL)
	if err != nil {
		return fmt.Errorf("connect source: %w", err)
	}
	defer source.Close(ctx)
	target, err := pgx.Connect(ctx, config.TargetURL)
	if err != nil {
		return fmt.Errorf("connect target: %w", err)
	}
	defer target.Close(ctx)
	sourceTx, err := source.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return fmt.Errorf("begin source snapshot: %w", err)
	}
	defer sourceTx.Rollback(ctx)
	targetTx, err := target.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin target transaction: %w", err)
	}
	defer targetTx.Rollback(ctx)
	for _, table := range tables {
		if err := copyTable(ctx, sourceTx, targetTx, table, config.Salt); err != nil {
			return err
		}
	}
	if err := targetTx.Commit(ctx); err != nil {
		return fmt.Errorf("commit target transaction: %w", err)
	}
	return sourceTx.Commit(ctx)
}

func copyTable(ctx context.Context, source pgx.Tx, target pgx.Tx, rule devai.TableRule, salt string) error {
	columns := make([]string, len(rule.Columns))
	for i, column := range rule.Columns {
		columns[i] = quote(column.Name)
	}
	rows, err := source.Query(ctx, "SELECT "+strings.Join(columns, ", ")+" FROM "+quotePath(rule.Source))
	if err != nil {
		return fmt.Errorf("query source table %s: %w", rule.Source, err)
	}
	defer rows.Close()
	values := make([][]any, 0)
	for rows.Next() {
		row, err := rows.Values()
		if err != nil {
			return fmt.Errorf("read source row: %w", err)
		}
		for i, column := range rule.Columns {
			row[i], err = transform(row[i], column.Transform, salt)
			if err != nil {
				return fmt.Errorf("transform %s.%s: %w", rule.Source, column.Name, err)
			}
		}
		values = append(values, row)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate source table %s: %w", rule.Source, err)
	}
	if _, err := target.Exec(ctx, "TRUNCATE TABLE "+quotePath(rule.Target)+" RESTART IDENTITY CASCADE"); err != nil {
		return fmt.Errorf("truncate target table %s: %w", rule.Target, err)
	}
	if len(values) == 0 {
		return nil
	}
	if _, err := target.CopyFrom(ctx, pgx.Identifier(strings.Split(rule.Target, ".")), columnNames(rule.Columns), pgx.CopyFromRows(values)); err != nil {
		return fmt.Errorf("copy target table %s: %w", rule.Target, err)
	}
	return nil
}

func columnNames(columns []devai.ColumnRule) []string {
	names := make([]string, len(columns))
	for i := range columns {
		names[i] = columns[i].Name
	}
	return names
}
func quotePath(path string) string {
	parts := strings.Split(path, ".")
	for i := range parts {
		parts[i] = quote(parts[i])
	}
	return strings.Join(parts, ".")
}
func quote(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
func transform(value any, rule, salt string) (any, error) {
	if rule == "preserve" || value == nil {
		return value, nil
	}
	raw, ok := value.(string)
	if !ok {
		if bytes, ok := value.([]byte); ok {
			raw = string(bytes)
		} else {
			return nil, fmt.Errorf("transform requires text value")
		}
	}
	digest := digest(raw, salt)
	switch rule {
	case "email":
		return "user-" + digest[:20] + "@sandbox.invalid", nil
	case "name":
		return "Sandbox User " + digest[:12], nil
	case "hash":
		return digest, nil
	case "redact":
		return "[redacted]", nil
	}
	return nil, fmt.Errorf("unsupported transform %q", rule)
}
func digest(value, salt string) string {
	hash := hmac.New(sha256.New, []byte(salt))
	_, _ = hash.Write([]byte(value))
	return hex.EncodeToString(hash.Sum(nil))
}
