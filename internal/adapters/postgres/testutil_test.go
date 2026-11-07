package postgres

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// getTestDatabaseURL returns the test PostgreSQL connection URL from environment
// variables or defaults to orbit infra's provisioned shop database on 127.0.0.1:10000.
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
		t.Skip("TEST_DATABASE_URL or DB_PASSWORD not configured; skipping postgres test")
	}

	return fmt.Sprintf("postgres://%s:%s@%s:%s/%s?sslmode=disable",
		user, pass, host, port, dbName)
}

// openTestPool initializes a connected *pgxpool.Pool and registers its cleanup.
func openTestPool(t testing.TB) *pgxpool.Pool {
	t.Helper()
	dsn := getTestDatabaseURL(t)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := NewPool(ctx, dsn)
	if err != nil {
		t.Fatalf("failed to connect to test postgres pool: %v", err)
	}

	t.Cleanup(func() {
		pool.Close()
	})

	return pool
}

// openTestStore initializes a *Store backed by a verified test pool.
func openTestStore(t testing.TB) *Store {
	t.Helper()
	pool := openTestPool(t)
	return NewStore(pool)
}
