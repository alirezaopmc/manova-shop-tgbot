// Package migrations owns embedded goose migration files and applies them
// through goose's Provider API.
package migrations

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"

	_ "github.com/jackc/pgx/v5/stdlib" // registers the "pgx" database/sql driver
	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var files embed.FS

// Migrator applies embedded migrations to a target PostgreSQL database.
type Migrator struct {
	db       *sql.DB
	provider *goose.Provider
}

// Open connects to databaseURL using pgx and prepares the embedded migrations.
// The caller must call Close() when finished.
func Open(databaseURL string) (*Migrator, error) {
	if !strings.Contains(databaseURL, "search_path=") {
		sep := "?"
		if strings.Contains(databaseURL, "?") {
			sep = "&"
		}
		databaseURL = databaseURL + sep + "search_path=shop,public"
	}
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("migrations: open database: %w", err)
	}
	m, err := New(db)
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	return m, nil
}

// New wraps an existing *sql.DB handle and prepares the embedded migrations.
func New(db *sql.DB) (*Migrator, error) {
	if db == nil {
		return nil, fmt.Errorf("migrations: nil database")
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, files)
	if err != nil {
		return nil, fmt.Errorf("migrations: prepare embedded migrations: %w", err)
	}
	return &Migrator{db: db, provider: provider}, nil
}

// Up applies all pending embedded migrations.
func (m *Migrator) Up(ctx context.Context) error {
	if _, err := m.provider.Up(ctx); err != nil {
		return fmt.Errorf("migrations: up: %w", err)
	}
	return nil
}

// UpResults applies all pending embedded migrations and returns individual results.
func (m *Migrator) UpResults(ctx context.Context) ([]*goose.MigrationResult, error) {
	results, err := m.provider.Up(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrations: up: %w", err)
	}
	return results, nil
}

// Down rolls back the most recently applied migration.
func (m *Migrator) Down(ctx context.Context) error {
	if _, err := m.provider.Down(ctx); err != nil {
		return fmt.Errorf("migrations: down: %w", err)
	}
	return nil
}

// DownResult rolls back the most recently applied migration and returns the result.
func (m *Migrator) DownResult(ctx context.Context) (*goose.MigrationResult, error) {
	result, err := m.provider.Down(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrations: down: %w", err)
	}
	return result, nil
}

// Status reports the state of every embedded migration.
func (m *Migrator) Status(ctx context.Context) ([]*goose.MigrationStatus, error) {
	status, err := m.provider.Status(ctx)
	if err != nil {
		return nil, fmt.Errorf("migrations: status: %w", err)
	}
	return status, nil
}

// Version returns the current and latest migration versions.
func (m *Migrator) Version(ctx context.Context) (current, target int64, err error) {
	current, target, err = m.provider.GetVersions(ctx)
	if err != nil {
		return 0, 0, fmt.Errorf("migrations: version: %w", err)
	}
	return current, target, nil
}

// Close closes the underlying migration database handle.
func (m *Migrator) Close() error {
	if m.db != nil {
		return m.db.Close()
	}
	return nil
}
