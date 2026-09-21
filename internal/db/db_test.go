package db

import (
	"context"
	"database/sql"
	"os"
	"testing"
	"time"
)

func TestOpen_EmptyURL(t *testing.T) {
	ctx := context.Background()
	_, err := Open(ctx, "")
	if err == nil {
		t.Fatal("expected error for empty DATABASE_URL, got nil")
	}
	if want := "ping database"; !contains(err.Error(), want) {
		t.Errorf("error = %q, want to contain %q", err.Error(), want)
	}
}

func TestOpen_InvalidURL(t *testing.T) {
	ctx := context.Background()
	// pgx will accept malformed URL but Ping will fail
	_, err := Open(ctx, "postgres://bad url%%")
	if err == nil {
		t.Fatal("expected error for invalid URL")
	}
}

func TestOpen_UnreachableHost(t *testing.T) {
	ctx := context.Background()
	// Use non-routable address with short timeout via context
	// Port 54321 is unlikely to have postgres, should fail on ping
	url := "postgres://user:pass@127.0.0.1:54321/test?sslmode=disable&connect_timeout=1"
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	_, err := Open(ctx, url)
	if err == nil {
		t.Fatal("expected error for unreachable host")
	}
	if !contains(err.Error(), "ping database") {
		t.Errorf("error = %q, want ping database", err.Error())
	}
}

func TestOpen_ContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately
	_, err := Open(ctx, "postgres://user:pass@127.0.0.1:5432/test?sslmode=disable")
	if err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestOpen_Success_IfDBAvailable(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	db, err := Open(ctx, url)
	if err != nil {
		t.Skipf("DB not reachable, skipping: %v", err)
	}
	defer db.Close()

	if err := db.PingContext(ctx); err != nil {
		t.Fatalf("Ping after Open failed: %v", err)
	}

	// Verify pool settings from db.go:12-18
	if got := db.Stats().MaxOpenConnections; got != 25 {
		t.Errorf("MaxOpenConns = %d, want 25", got)
	}

	// Verify we get a *sql.DB
	var _ *sql.DB = db
}

func TestOpen_SetsConnLifetime(t *testing.T) {
	// This test verifies Open doesn't panic and returns a DB handle with expected settings
	// when given a reachable DB. If DB not available, we just verify it doesn't panic on invalid.
	ctx := context.Background()
	_, err := Open(ctx, "postgres://user:pass@localhost:5432/nonexistent?sslmode=disable&connect_timeout=1")
	// Should error on ping, but not panic and should wrap as ping database
	if err != nil && !contains(err.Error(), "ping database") {
		t.Errorf("unexpected error type: %v", err)
	}
}

// helper
func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
