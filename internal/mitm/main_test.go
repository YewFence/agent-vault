package mitm

import (
	"testing"

	"github.com/Infisical/agent-vault/internal/testenv"
)

// TestMain scrubs ambient AGENT_VAULT_* variables so a developer shell
// (mise, direnv, `infisical run`) cannot leak runtime config into tests.
// Tests that need a variable set it explicitly with t.Setenv.
func TestMain(m *testing.M) { testenv.Main(m) }
