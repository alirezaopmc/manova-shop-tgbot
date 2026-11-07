package migrations

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func getTestDatabaseURL(t testing.TB) string {
	t.Helper()
	if dsn := os.Getenv("TEST_DATABASE_URL"); dsn != "" {
		return dsn
	}
	if dsn := os.Getenv("DATABASE_URL"); dsn != "" {
		return dsn
	}

	host := os.Getenv("DATABASE_HOST")
	if host == "" {
		host = "127.0.0.1"
	}
	port := os.Getenv("DATABASE_PORT")
	if port == "" {
		port = "10000"
	}
	user := os.Getenv("DATABASE_USER")
	if user == "" {
		user = "shop"
	}
	pass := os.Getenv("DB_PASSWORD")
	if pass == "" {
		passBytes, err := os.ReadFile("/opt/orbit-infra/postgres/secrets/shop.password")
		if err == nil {
			pass = strings.TrimSpace(string(passBytes))
		}
	}
	dbName := os.Getenv("DATABASE_NAME")
	if dbName == "" {
		dbName = "shop"
	}

	if pass == "" {
		t.Skip("TEST_DATABASE_URL or DB_PASSWORD not configured; skipping postgres migration test")
	}

	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		user, pass, host, port, dbName)
}

func TestFreshDatabaseAppliesEmbeddedMigrations(t *testing.T) {
	dsn := getTestDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	m, err := Open(dsn)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = m.Close() }()

	// Apply migrations
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up failed: %v", err)
	}

	current, target, err := m.Version(ctx)
	if err != nil {
		t.Fatalf("Version failed: %v", err)
	}
	if current < 1 || target < 1 {
		t.Fatalf("expected version >= 1, got current=%d, target=%d", current, target)
	}

	// Verify all 15 expected tables exist in shop schema
	expectedTables := []string{
		"users",
		"invites",
		"invite_claims",
		"wallets",
		"wallet_ledger",
		"catalog_items",
		"os_catalog_entries",
		"orders",
		"order_events",
		"receipts",
		"provisioned_resources",
		"minute_postings",
		"refund_requests",
		"conversation_states",
		"admin_actions",
	}

	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer func() { _ = db.Close() }()

	for _, table := range expectedTables {
		var exists bool
		err := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM information_schema.tables 
				WHERE table_schema = 'shop' AND table_name = $1
			)
		`, table).Scan(&exists)
		if err != nil {
			t.Fatalf("query table %s existence: %v", table, err)
		}
		if !exists {
			t.Errorf("expected table shop.%s to exist, but it was not found", table)
		}
	}

	// Verify seeded catalog items
	var catalogCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM shop.catalog_items`).Scan(&catalogCount); err != nil {
		t.Fatalf("query catalog_items count: %v", err)
	}
	if catalogCount < 3 {
		t.Errorf("expected at least 3 catalog items seeded, got %d", catalogCount)
	}

	for _, id := range []string{"cx23", "cx33", "cx43"} {
		var active bool
		var name string
		err := db.QueryRowContext(ctx, `SELECT name, active FROM shop.catalog_items WHERE id = $1`, id).Scan(&name, &active)
		if err != nil {
			t.Errorf("seed item %s not found: %v", id, err)
		}
		if !active {
			t.Errorf("expected seed item %s to be active", id)
		}
	}

	// Verify seeded OS catalog entries
	var osCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM shop.os_catalog_entries`).Scan(&osCount); err != nil {
		t.Fatalf("query os_catalog_entries count: %v", err)
	}
	if osCount < 3 {
		t.Errorf("expected at least 3 OS entries seeded, got %d", osCount)
	}

	for _, img := range []string{"ubuntu-24.04", "ubuntu-22.04", "debian-12"} {
		var id string
		err := db.QueryRowContext(ctx, `SELECT id FROM shop.os_catalog_entries WHERE image_name = $1`, img).Scan(&id)
		if err != nil {
			t.Errorf("seed OS image %s not found: %v", img, err)
		}
	}

	// Verify wallet_ledger append-only immutability trigger
	testLedgerID := fmt.Sprintf("test_led_%d", time.Now().UnixNano())
	_, err = db.ExecContext(ctx, `
		INSERT INTO shop.wallet_ledger (id, wallet_id, currency, amount, kind, balance_after)
		VALUES ($1, 99999999, 'IRT', 100000, 'topup_receipt', 100000)
	`, testLedgerID)
	if err != nil {
		t.Fatalf("inserting test ledger entry failed: %v", err)
	}

	// Attempt UPDATE on wallet_ledger - must fail
	_, err = db.ExecContext(ctx, `
		UPDATE shop.wallet_ledger SET amount = 200000 WHERE id = $1
	`, testLedgerID)
	if err == nil {
		t.Errorf("expected UPDATE on shop.wallet_ledger to fail due to immutable trigger, but it succeeded")
	} else if !strings.Contains(err.Error(), "immutable") && !strings.Contains(err.Error(), "prohibited") {
		t.Errorf("unexpected error message on UPDATE: %v", err)
	}

	// Attempt DELETE on wallet_ledger - must fail
	_, err = db.ExecContext(ctx, `
		DELETE FROM shop.wallet_ledger WHERE id = $1
	`, testLedgerID)
	if err == nil {
		t.Errorf("expected DELETE on shop.wallet_ledger to fail due to immutable trigger, but it succeeded")
	} else if !strings.Contains(err.Error(), "immutable") && !strings.Contains(err.Error(), "prohibited") {
		t.Errorf("unexpected error message on DELETE: %v", err)
	}
}

func TestDownAndReapplyMigrations(t *testing.T) {
	dsn := getTestDatabaseURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Create isolated test schema
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatalf("connect to database: %v", err)
	}
	defer func() { _ = db.Close() }()

	testSchema := fmt.Sprintf("test_mig_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, fmt.Sprintf("CREATE SCHEMA %s", testSchema)); err != nil {
		t.Fatalf("failed to create isolated test schema %s: %v", testSchema, err)
	}
	t.Cleanup(func() {
		cleanCtx, cleanCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanCancel()
		_, _ = db.ExecContext(cleanCtx, fmt.Sprintf("DROP SCHEMA IF EXISTS %s CASCADE", testSchema))
	})

	// 2. Open migrator targeting the isolated test schema via search_path
	sep := "?"
	if strings.Contains(dsn, "?") {
		sep = "&"
	}
	isolatedDSN := fmt.Sprintf("%s%ssearch_path=%s", dsn, sep, testSchema)

	m, err := Open(isolatedDSN)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer func() { _ = m.Close() }()

	// Ensure Up is applied first in isolated test schema
	if err := m.Up(ctx); err != nil {
		t.Fatalf("Up before Down failed: %v", err)
	}

	var initialTableCount int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = $1
	`, testSchema).Scan(&initialTableCount)
	if err != nil {
		t.Fatalf("query table count in test schema: %v", err)
	}
	if initialTableCount < 15 {
		t.Fatalf("expected at least 15 tables in isolated schema %s, got %d", testSchema, initialTableCount)
	}

	// Test Down - drops tables in isolated test schema
	if err := m.Down(ctx); err != nil {
		t.Fatalf("Down failed: %v", err)
	}

	// Verify isolated schema tables (excluding goose version tracking table) are dropped
	var tableCount int
	err = db.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = $1 AND table_name != 'goose_db_version'
	`, testSchema).Scan(&tableCount)
	if err != nil {
		t.Fatalf("query table count after Down: %v", err)
	}
	if tableCount != 0 {
		t.Errorf("expected 0 tables in %s schema after Down, got %d", testSchema, tableCount)
	}

	// Reapply Up in isolated test schema
	if err := m.Up(ctx); err != nil {
		t.Fatalf("reapply Up after Down failed: %v", err)
	}

	current, target, err := m.Version(ctx)
	if err != nil {
		t.Fatalf("Version after reapply failed: %v", err)
	}
	if current != target || current < 1 {
		t.Errorf("expected version >= 1 after reapply, got current=%d, target=%d", current, target)
	}
}
