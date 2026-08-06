package cmd

import (
	"net/url"
	"os"
	"path/filepath"
	"testing"
)

func TestParseMigrationEndpoint(t *testing.T) {
	tests := []struct {
		name        string
		raw         string
		wantDialect string
		wantErr     bool
	}{
		{name: "sqlite path", raw: "/tmp/agent-vault.db", wantDialect: "sqlite"},
		{name: "relative sqlite path", raw: "agent-vault.db", wantDialect: "sqlite"},
		{name: "postgres URL", raw: "postgres://user:pass@localhost/db", wantDialect: "postgres"},
		{name: "postgresql URL", raw: "postgresql://user:pass@localhost/db", wantDialect: "postgres"},
		{name: "unsupported URL", raw: "mysql://localhost/db", wantErr: true},
		{name: "empty", raw: "", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMigrationEndpoint(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.dialect != tt.wantDialect || got.value != tt.raw {
				t.Fatalf("got %+v, want dialect=%s value=%s", got, tt.wantDialect, tt.raw)
			}
		})
	}
}

func TestMigrationEndpointDisplayRedactsPostgresPassword(t *testing.T) {
	endpoint, err := parseMigrationEndpoint("postgres://user:secret@localhost/db")
	if err != nil {
		t.Fatal(err)
	}
	display := endpoint.display()
	u, err := url.Parse(display)
	if err != nil {
		t.Fatal(err)
	}
	password, ok := u.User.Password()
	if !ok || password != "***" {
		t.Fatalf("password was not redacted: %s", display)
	}
}

func TestPrepareSQLiteDestination(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "agent-vault.db")
	if err := prepareSQLiteDestination(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Dir(path)); err != nil {
		t.Fatalf("destination directory was not created: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("destination database should not be created during preparation: %v", err)
	}

	if err := os.WriteFile(path, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := prepareSQLiteDestination(path); err == nil {
		t.Fatal("expected existing destination to be rejected")
	}
}
