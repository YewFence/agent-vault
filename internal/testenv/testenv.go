// Package testenv keeps tests hermetic from the developer's shell
// environment. Agent Vault is configured through AGENT_VAULT_* variables,
// and developers routinely export them via mise, direnv, or
// `infisical run -- make test`. Left in place they leak runtime config —
// data directory, server address, tokens — into tests that assume
// defaults (e.g. ~/.agent-vault paths), causing failures that have
// nothing to do with the code under test.
//
// Usage — one TestMain per affected package:
//
//	func TestMain(m *testing.M) { testenv.Main(m) }
//
// Tests that exercise env-var behavior explicitly are unaffected: they
// set what they need with t.Setenv, which runs after TestMain.
package testenv

import (
	"os"
	"strings"
	"testing"
)

// EnvPrefix is the environment variable prefix scrubbed by Scrub.
const EnvPrefix = "AGENT_VAULT_"

// Main scrubs ambient Agent Vault variables from the test process and
// runs m. Deliberately narrow: DATABASE_URL and similar are left alone
// because CI may set them intentionally to select a test backend.
func Main(m *testing.M) {
	Scrub()
	os.Exit(m.Run())
}

// Scrub removes every AGENT_VAULT_* variable from the process environment.
func Scrub() {
	for _, env := range os.Environ() {
		name, _, _ := strings.Cut(env, "=")
		if strings.HasPrefix(name, EnvPrefix) {
			os.Unsetenv(name)
		}
	}
}
