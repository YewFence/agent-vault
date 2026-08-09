package cmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/Infisical/agent-vault/internal/broker"
	"github.com/spf13/cobra"
)

func newPlaceholdersCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "placeholders",
		Short: "Print shell exports for configured credential placeholders",
		Long: `Print shell code that exports the placeholders declared by enabled services.

These values are not secrets. They are credential-shaped markers that Agent
Vault replaces with real credentials at the proxy boundary. Evaluate the output
in a shell whose Agent Vault token, address, vault, proxy, and CA trust are
already configured:

  eval "$(agent-vault placeholders)"

Standard output contains only shell code. Warnings and errors go to standard
error, so command substitution is safe to evaluate on success.`,
		Args: cobra.NoArgs,
		RunE: placeholdersCmdRunE,
	}

	c.Flags().String("address", "", "Agent Vault server address (defaults to session address)")
	c.Flags().String("vault", "", "target vault (overrides active context)")
	return c
}

var placeholdersCmd = newPlaceholdersCmd()

func placeholdersCmdRunE(cmd *cobra.Command, _ []string) error {
	sess, tokenSource, err := resolveSession()
	if err != nil {
		return err
	}
	addr, _ := cmd.Flags().GetString("address")
	if addr == "" {
		addr = sess.Address
	}
	vault, err := resolveVaultForCommand(cmd, tokenSource)
	if err != nil {
		return err
	}

	placeholders, err := fetchServicePlaceholders(addr, sess.Token, vault)
	if err != nil {
		return err
	}
	script, err := renderPlaceholderScript(placeholders)
	if err != nil {
		return err
	}
	_, err = fmt.Fprint(cmd.OutOrStdout(), script)
	return err
}

func renderPlaceholderScript(placeholders map[string]string) (string, error) {
	keys := make([]string, 0, len(placeholders))
	for key := range placeholders {
		if !broker.EnvVarPattern.MatchString(key) {
			return "", fmt.Errorf("cannot export invalid environment variable %q", key)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var script strings.Builder
	for _, key := range keys {
		fmt.Fprintf(&script, "export %s=%s\n", key, quoteShellValue(placeholders[key]))
	}
	return script.String(), nil
}

// fetchServicePlaceholders returns env var name → placeholder for substitutions
// with an explicit env setting on the vault's enabled services. A single env
// var cannot represent different placeholders, so conflicting services fail
// instead of depending on their storage order.
func fetchServicePlaceholders(addr, token, vault string) (map[string]string, error) {
	url := fmt.Sprintf("%s/v1/vaults/%s/services", addr, vault)
	respBody, err := doVaultScopedRequestWithBody("GET", url, token, vault, nil)
	if err != nil {
		return nil, fmt.Errorf("fetching services for placeholder env: %w", err)
	}
	var resp struct {
		Services []broker.Service `json:"services"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("parsing services for placeholder env: %w", err)
	}

	placeholders := make(map[string]string)
	serviceNames := make(map[string]string)
	for _, svc := range resp.Services {
		if !svc.IsEnabled() {
			continue
		}
		for _, sub := range svc.Substitutions {
			if sub.Env == "" {
				continue
			}
			if previous, exists := placeholders[sub.Env]; exists && previous != sub.Placeholder {
				return nil, fmt.Errorf("environment variable %q has different placeholders in services %q and %q", sub.Env, serviceNames[sub.Env], svc.Name)
			}
			placeholders[sub.Env] = sub.Placeholder
			serviceNames[sub.Env] = svc.Name
		}
	}
	return placeholders, nil
}

func quoteShellValue(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

func init() {
	rootCmd.AddCommand(placeholdersCmd)
}
