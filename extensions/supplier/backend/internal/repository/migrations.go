package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"sort"

	"github.com/moxaitoken/sub2api-supplier/internal/ports"
	"github.com/moxaitoken/sub2api-supplier/migrations"
)

var sourcePattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

const migrationLock int64 = 0x535550504c494552

type migration struct{ name, checksum, text string }

func migrationFiles() ([]migration, error) {
	names, err := fs.Glob(migrations.Files, "*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	items := make([]migration, 0, len(names))
	for _, name := range names {
		b, err := migrations.Files.ReadFile(name)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		items = append(items, migration{name, hex.EncodeToString(sum[:]), string(b)})
	}
	return items, nil
}

func (s *Store) databaseGuard(ctx context.Context, tx *sql.Tx) error {
	var name string
	if err := tx.QueryRowContext(ctx, `SELECT current_database()`).Scan(&name); err != nil {
		return err
	}
	if name == "sub2api" || name == "postgres" || name == "template0" || name == "template1" {
		return fmt.Errorf("%w: refusing a primary or maintenance database", ports.ErrForbidden)
	}
	if !sourcePattern.MatchString(s.sourceSHA) || s.instanceID == "" {
		return fmt.Errorf("%w: invalid upstream binding", ports.ErrConflict)
	}
	return nil
}

func checkApplied(ctx context.Context, tx *sql.Tx, files []migration, allowMissing bool) error {
	want := map[string]string{}
	for _, f := range files {
		want[f.name] = f.checksum
	}
	rows, err := tx.QueryContext(ctx, `SELECT version,checksum FROM supplier.schema_migrations ORDER BY version`)
	if err != nil {
		return err
	}
	defer rows.Close()
	seen := map[string]bool{}
	for rows.Next() {
		var name, checksum string
		if err = rows.Scan(&name, &checksum); err != nil {
			return err
		}
		expected, ok := want[name]
		if !ok || expected != checksum {
			return fmt.Errorf("%w: migration version or checksum mismatch", ports.ErrConflict)
		}
		seen[name] = true
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if !allowMissing && len(seen) != len(files) {
		return fmt.Errorf("%w: explicit migration required", ports.ErrUnavailable)
	}
	return nil
}

// Migrate is an explicitly invoked command. Constructing the store and starting
// HTTP handlers never apply DDL. All forward migrations and metadata commit atomically.
func (s *Store) Migrate(ctx context.Context) error {
	files, err := migrationFiles()
	if err != nil {
		return err
	}
	_, err = transaction(ctx, s.db, func(tx *sql.Tx) (bool, error) {
		if err = s.databaseGuard(ctx, tx); err != nil {
			return false, err
		}
		if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1)`, migrationLock); err != nil {
			return false, err
		}
		if _, err = tx.ExecContext(ctx, `CREATE SCHEMA IF NOT EXISTS supplier; CREATE TABLE IF NOT EXISTS supplier.schema_migrations(version text PRIMARY KEY,checksum text NOT NULL CHECK(checksum ~ '^[0-9a-f]{64}$'),applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
			return false, err
		}
		if err = checkApplied(ctx, tx, files, true); err != nil {
			return false, err
		}
		for _, file := range files {
			var exists bool
			if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM supplier.schema_migrations WHERE version=$1)`, file.name).Scan(&exists); err != nil {
				return false, err
			}
			if exists {
				continue
			}
			if _, err = tx.ExecContext(ctx, file.text); err != nil {
				return false, fmt.Errorf("migration %s: %w", file.name, err)
			}
			if _, err = tx.ExecContext(ctx, `INSERT INTO supplier.schema_migrations(version,checksum) VALUES($1,$2)`, file.name, file.checksum); err != nil {
				return false, err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO supplier.upstream_instances(id,code,source_sha) VALUES($1::uuid,$1::text,$2) ON CONFLICT(id) DO UPDATE SET source_sha=EXCLUDED.source_sha,updated_at=now(),version=supplier.upstream_instances.version+1 WHERE supplier.upstream_instances.source_sha<>EXCLUDED.source_sha`, s.instanceID, s.sourceSHA)
		return true, err
	})
	return err
}

// CheckMigrations performs only reads; the runtime DB role need not own DDL.
func (s *Store) CheckMigrations(ctx context.Context) error {
	files, err := migrationFiles()
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err = s.databaseGuard(ctx, tx); err != nil {
		return err
	}
	if err = checkApplied(ctx, tx, files, false); err != nil {
		return err
	}
	var source string
	var enabled bool
	if err = tx.QueryRowContext(ctx, `SELECT source_sha,enabled FROM supplier.upstream_instances WHERE id=$1`, s.instanceID).Scan(&source, &enabled); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ports.ErrUnavailable
		}
		return err
	}
	if source != s.sourceSHA || !enabled {
		return fmt.Errorf("%w: upstream binding mismatch", ports.ErrConflict)
	}
	return tx.Commit()
}
