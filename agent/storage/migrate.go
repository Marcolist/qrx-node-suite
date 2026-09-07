// Package storage owns the Agent's SQLite database: migrations and typed
// repositories over the tables in docs/architecture.md's "Local database"
// section. It never modifies the schema ad hoc -- every change is a new
// numbered file under migrations/, applied in order and recorded in
// schema_migrations (docs/updates.md#database-migrations).
package storage

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"time"

	_ "qrx-node-suite/agent/storage/sqlite" // registers the "sqlite3" driver
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Open opens (creating if necessary) the SQLite database at path and applies
// any migrations that haven't run yet. path may be ":memory:" for tests.
func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	// A single-writer file database; avoid the pool handing out a second
	// connection that would see a different (pre-migration) schema mid-open.
	db.SetMaxOpenConns(1)
	if err := Migrate(db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Migrate applies every embedded migration not yet recorded in
// schema_migrations, each inside its own transaction. Migration files are
// applied in filename order (0001_..., 0002_..., ...); never reorder or edit
// a released one -- add a new file instead.
func Migrate(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations (
		version TEXT PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	applied := map[string]bool{}
	rows, err := db.Query(`SELECT version FROM schema_migrations`)
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	for rows.Next() {
		var v string
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[v] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	rows.Close()

	entries, err := fs.ReadDir(migrationFiles, "migrations")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)

	for _, name := range names {
		if applied[name] {
			continue
		}
		script, err := migrationFiles.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		if err := applyMigration(db, name, string(script)); err != nil {
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
	}
	return nil
}

func applyMigration(db *sql.DB, name, script string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() // no-op if Commit succeeds

	// No bound parameters -- routes through the sqlite driver's Execer path
	// (sqlite3_exec), which runs every statement in the script. See
	// agent/storage/sqlite/README.md, "The multi-statement footgun".
	if _, err := tx.Exec(script); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO schema_migrations (version, applied_at) VALUES (?, ?)`,
		name, time.Now().UTC().Format(time.RFC3339Nano)); err != nil {
		return err
	}
	return tx.Commit()
}
