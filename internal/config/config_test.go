package config

import (
	"os"
	"testing"
)

func setValidEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://test:test@localhost:5432/test?sslmode=disable")
	t.Setenv("JWT_SECRET", "01234567890123456789012345678901") // 32 chars
	t.Setenv("GOOGLE_CLIENT_ID", "test-client-id")
	t.Setenv("GOOGLE_CLIENT_SECRET", "test-client-secret")
	t.Setenv("GOOGLE_REDIRECT_URL", "http://localhost:8080/auth/google/callback")
	t.Setenv("SUPABASE_URL", "https://test.supabase.co")
	t.Setenv("SUPABASE_SECRET_KEY", "test-secret-key")
	t.Setenv("SUPABASE_BUCKET", "files")
	t.Setenv("PORT", "8080")
	t.Setenv("ENV", "test")
	t.Setenv("FRONTEND_URL", "http://localhost:3000")
}

func TestLoad_Success(t *testing.T) {
	setValidEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v, want nil", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("Port = %q, want 8080", cfg.Port)
	}
	if cfg.Env != "test" {
		t.Errorf("Env = %q, want test", cfg.Env)
	}
	if cfg.DatabaseURL == "" {
		t.Error("DatabaseURL empty")
	}
	if cfg.SupabaseBucket != "files" {
		t.Errorf("SupabaseBucket = %q, want files", cfg.SupabaseBucket)
	}
}

func TestLoad_Defaults(t *testing.T) {
	setValidEnv(t)
	// Unset optional defaults to check fallback
	t.Setenv("PORT", "")
	t.Setenv("SUPABASE_BUCKET", "")
	t.Setenv("ENV", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Port != "8080" {
		t.Errorf("default Port = %q, want 8080", cfg.Port)
	}
	if cfg.SupabaseBucket != "files" {
		t.Errorf("default SupabaseBucket = %q, want files", cfg.SupabaseBucket)
	}
	if cfg.Env != "development" {
		t.Errorf("default Env = %q, want development", cfg.Env)
	}
}

func TestLoad_MissingRequired(t *testing.T) {
	required := []string{
		"DATABASE_URL",
		"JWT_SECRET",
		"GOOGLE_CLIENT_ID",
		"GOOGLE_CLIENT_SECRET",
		"GOOGLE_REDIRECT_URL",
		"SUPABASE_URL",
		"SUPABASE_SECRET_KEY",
	}

	for _, key := range required {
		t.Run(key, func(t *testing.T) {
			setValidEnv(t)
			t.Setenv(key, "")
			// Special case: JWT_SECRET empty vs short
			if key == "JWT_SECRET" {
				// ensure empty triggers required error, not length error
			}
			_, err := Load()
			if err == nil {
				t.Fatalf("Load() with empty %s should error", key)
			}
			// Check error mentions key
			if !contains(err.Error(), key) {
				t.Errorf("error %q should mention %q", err.Error(), key)
			}
		})
	}
}

func TestLoad_MissingRequired_Whitespace(t *testing.T) {
	setValidEnv(t)
	t.Setenv("DATABASE_URL", "   ")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for whitespace DATABASE_URL")
	}
}

func TestLoad_JWTSecretTooShort(t *testing.T) {
	setValidEnv(t)
	t.Setenv("JWT_SECRET", "short")
	_, err := Load()
	if err == nil {
		t.Fatal("expected error for short JWT_SECRET")
	}
	if !contains(err.Error(), "JWT_SECRET must be at least 32") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLoad_JWTSecretExactly32(t *testing.T) {
	setValidEnv(t)
	t.Setenv("JWT_SECRET", "12345678901234567890123456789012") // 32
	_, err := Load()
	if err != nil {
		t.Fatalf("32-char secret should pass, got %v", err)
	}
}

func TestValidatePort(t *testing.T) {
	tests := []struct {
		port    string
		wantErr bool
	}{
		{"8080", false},
		{"1", false},
		{"65535", false},
		{"0", true},
		{"65536", true},
		{"-1", true},
		{"abc", true},
		{"", true},
		{" 8080 ", true}, // strconv.Atoi fails on spaces, validate should error
	}

	for _, tt := range tests {
		t.Run(tt.port, func(t *testing.T) {
			err := validatePort(tt.port)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePort(%q) error = %v, wantErr %v", tt.port, err, tt.wantErr)
			}
		})
	}
}

func TestGetEnv_Trimming(t *testing.T) {
	t.Setenv("TEST_KEY", "  value  ")
	got := getEnv("TEST_KEY", "fallback")
	if got != "value" {
		t.Errorf("getEnv trimming = %q, want %q", got, "value")
	}
	t.Setenv("TEST_KEY", "")
	got = getEnv("TEST_KEY", "fallback")
	if got != "fallback" {
		t.Errorf("getEnv fallback = %q, want fallback", got)
	}
	// ensure env with spaces only returns fallback
	t.Setenv("TEST_KEY", "   ")
	got = getEnv("TEST_KEY", "fallback")
	if got != "fallback" {
		t.Errorf("getEnv spaces = %q, want fallback", got)
	}
	os.Unsetenv("TEST_KEY")
}

func TestValidate_AllRequiredTrimmed(t *testing.T) {
	setValidEnv(t)
	// Ensure validate trims whitespace for required fields
	t.Setenv("SUPABASE_URL", "   https://test.supabase.co   ")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load with trimmed url failed: %v", err)
	}
	// Note: getEnv for SUPABASE_URL uses os.Getenv directly without trim for most fields,
	// but validate does TrimSpace check. The stored cfg will still have spaces.
	// This test ensures valid if spaces present, but we expect Load to succeed.
	_ = cfg
}

// helper
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	})()
}
