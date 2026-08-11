package testenv

import (
	"os"
	"testing"
)

func TestScrub(t *testing.T) {
	t.Setenv("AGENT_VAULT_TESTENV_DUMMY", "1")
	t.Setenv("AGENT_VAULT_HOME", "/tmp/should-not-survive")
	t.Setenv("UNRELATED_TESTENV_VAR", "keep")

	Scrub()

	for _, name := range []string{"AGENT_VAULT_TESTENV_DUMMY", "AGENT_VAULT_HOME"} {
		if _, ok := os.LookupEnv(name); ok {
			t.Fatalf("%s should have been scrubbed", name)
		}
	}
	if got := os.Getenv("UNRELATED_TESTENV_VAR"); got != "keep" {
		t.Fatalf("unrelated variable changed: %q", got)
	}
}
