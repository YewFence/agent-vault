// Package datadir resolves the directory used for Agent Vault's local state.
package datadir

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	// EnvVar overrides the default ~/.agent-vault data directory.
	EnvVar         = "AGENT_VAULT_HOME"
	defaultDirName = ".agent-vault"
)

// Path returns the absolute path to Agent Vault's local data directory.
func Path() (string, error) {
	if configured := os.Getenv(EnvVar); configured != "" {
		path, err := filepath.Abs(configured)
		if err != nil {
			return "", fmt.Errorf("resolving %s: %w", EnvVar, err)
		}
		path = filepath.Clean(path)
		if filepath.Dir(path) == path {
			return "", fmt.Errorf("%s must not be a filesystem root", EnvVar)
		}
		return path, nil
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolving home directory: %w", err)
	}
	return filepath.Join(home, defaultDirName), nil
}

// Ensure creates the data directory with owner-only permissions and returns it.
func Ensure() (string, error) {
	path, err := Path()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return "", fmt.Errorf("creating data directory: %w", err)
	}
	return path, nil
}
