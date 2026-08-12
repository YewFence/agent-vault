// Package skills exposes the skills distributed with Agent Vault.
package skills

import (
	"embed"
	"io/fs"
)

const AgentVaultCLI = "agent-vault-cli"

//go:embed all:agent-vault-cli
var embedded embed.FS

// Content returns the complete agent-vault-cli skill, rooted at its SKILL.md.
func Content() fs.FS {
	content, err := fs.Sub(embedded, AgentVaultCLI)
	if err != nil {
		panic(err)
	}
	return content
}

// ReadFile reads a file relative to the agent-vault-cli skill root.
func ReadFile(name string) ([]byte, error) {
	return fs.ReadFile(Content(), name)
}
