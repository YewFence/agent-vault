package datadir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPathDefaultsToAgentVaultDirectoryInHome(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(EnvVar, "")

	got, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := filepath.Join(home, ".agent-vault")
	if got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestPathUsesConfiguredDirectory(t *testing.T) {
	want := filepath.Join(t.TempDir(), "vault-data")
	t.Setenv(EnvVar, want)

	got, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	if got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestPathResolvesRelativeConfiguredDirectory(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	t.Setenv(EnvVar, filepath.Join("testdata", "vault-data"))

	got, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := filepath.Join(cwd, "testdata", "vault-data")
	if got != want {
		t.Fatalf("Path = %q, want %q", got, want)
	}
}

func TestPathRejectsFilesystemRoot(t *testing.T) {
	t.Setenv(EnvVar, string(os.PathSeparator))

	if _, err := Path(); err == nil {
		t.Fatal("Path accepted the filesystem root")
	}
}

func TestEnsureCreatesOwnerOnlyDirectory(t *testing.T) {
	want := filepath.Join(t.TempDir(), "nested", "vault-data")
	t.Setenv(EnvVar, want)

	got, err := Ensure()
	if err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	if got != want {
		t.Fatalf("Ensure = %q, want %q", got, want)
	}
	info, err := os.Stat(want)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o700 {
		t.Fatalf("permissions = %o, want 700", perm)
	}
}
