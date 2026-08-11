package session

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/Infisical/agent-vault/internal/datadir"
)

// ClientSession holds the session token and server address for an authenticated client.
type ClientSession struct {
	Token   string `json:"token"`
	Address string `json:"address"`
	// Email of the account that minted Token. Cached so re-auth on
	// expiry can skip the email prompt; empty for sessions saved by
	// older clients.
	Email string `json:"email,omitempty"`
	// DeviceLabel is the label this CLI sent when minting the session.
	// Cached so silent re-auth (withReauthRetry) preserves the operator's
	// `--device-label` choice instead of falling back to os.Hostname().
	// Empty for sessions saved by older clients.
	DeviceLabel string `json:"device_label,omitempty"`
}

func sessionPath() (string, error) {
	dir, err := datadir.Ensure()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "session.json"), nil
}

// Save persists the client session to session.json in the configured data directory.
func Save(sess *ClientSession) error {
	path, err := sessionPath()
	if err != nil {
		return err
	}
	data, err := json.Marshal(sess)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// Load reads the client session from the configured data directory.
// Returns nil, nil if the file does not exist.
func Load() (*ClientSession, error) {
	path, err := sessionPath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var sess ClientSession
	if err := json.Unmarshal(data, &sess); err != nil {
		return nil, err
	}
	return &sess, nil
}

// Clear removes the session file.
func Clear() error {
	path, err := sessionPath()
	if err != nil {
		return err
	}
	err = os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func contextPath() (string, error) {
	dir, err := datadir.Ensure()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "context"), nil
}

// SaveVaultContext persists the active vault name to context in the configured data directory.
func SaveVaultContext(vault string) error {
	path, err := contextPath()
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(vault), 0600)
}

// LoadVaultContext reads the active vault name from the configured data directory.
// Returns "" if the file does not exist.
func LoadVaultContext() string {
	path, err := contextPath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}
